package wecom

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liut/morign/pkg/models/channel"
	"github.com/liut/morign/pkg/services/channels"
)

// HTTPChannel implements channel.Channel for WeCom HTTP webhook.
type HTTPChannel struct {
	corpID         string
	corpSecret     string
	agentID        string
	allowFrom      string
	token          string
	aesKey         []byte
	callbackPath   string
	enableMarkdown bool
	handler        channel.MessageHandler
	apiClient      *http.Client
	tokenCache     tokenCache
	userNameCache  sync.Map
}

type tokenCache struct {
	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

type replyContext struct {
	userID string
}

func newHTTP(opts map[string]any) (channel.Channel, error) {
	corpID, _ := opts["corp_id"].(string)
	corpID = os.ExpandEnv(corpID)
	corpSecret, _ := opts["corp_secret"].(string)
	corpSecret = os.ExpandEnv(corpSecret)
	agentID, _ := opts["agent_id"].(string)
	agentID = os.ExpandEnv(agentID)
	callbackToken, _ := opts["callback_token"].(string)
	callbackToken = os.ExpandEnv(callbackToken)
	callbackAESKey, _ := opts["callback_aes_key"].(string)
	callbackAESKey = os.ExpandEnv(callbackAESKey)

	if corpID == "" || corpSecret == "" || agentID == "" {
		return nil, fmt.Errorf("wecom-http: corp_id, corp_secret, and agent_id are required")
	}
	if callbackToken == "" || callbackAESKey == "" {
		return nil, fmt.Errorf("wecom-http: callback_token and callback_aes_key are required")
	}

	aesKey, err := decodeAESKey(callbackAESKey)
	if err != nil {
		return nil, fmt.Errorf("wecom-http: invalid callback_aes_key: %w", err)
	}

	transport := &http.Transport{
		MaxIdleConns:        2,
		MaxIdleConnsPerHost: 1,
		IdleConnTimeout:     10 * time.Second,
	}
	if proxyURL, _ := opts["proxy"].(string); proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("wecom-http: invalid proxy URL %q: %w", proxyURL, err)
		}
		proxyUser, _ := opts["proxy_username"].(string)
		proxyPass, _ := opts["proxy_password"].(string)
		if proxyUser != "" {
			u.User = url.UserPassword(proxyUser, proxyPass)
		}
		transport.Proxy = http.ProxyURL(u)
		transport.DisableKeepAlives = true
	}
	apiClient := &http.Client{Timeout: 30 * time.Second, Transport: transport}

	enableMarkdown, _ := opts["enable_markdown"].(bool)
	allowFrom, _ := opts["allow_from"].(string)

	return &HTTPChannel{
		corpID:         corpID,
		corpSecret:     corpSecret,
		agentID:        agentID,
		allowFrom:      allowFrom,
		token:          callbackToken,
		aesKey:         aesKey,
		enableMarkdown: enableMarkdown,
		apiClient:      apiClient,
	}, nil
}

func (p *HTTPChannel) Name() string { return "wecom" }

func (p *HTTPChannel) Start(handler channel.MessageHandler) error {
	p.handler = handler
	return nil
}

func (p *HTTPChannel) RegisterHTTPRoutes(r chi.Router, callbackPath string, handler channel.MessageHandler) {
	p.callbackPath = callbackPath
	r.Method(http.MethodGet, callbackPath, http.HandlerFunc(p.handleVerify))
	r.Method(http.MethodPost, callbackPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.handleMessage(w, r, handler)
	}))
	slog.Info("wecom-http: routes registered", "path", callbackPath)
}

func (p *HTTPChannel) handleVerify(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	msgSignature := q.Get("msg_signature")
	timestamp := q.Get("timestamp")
	nonce := q.Get("nonce")
	echostr := q.Get("echostr")

	if !p.verifySignature(msgSignature, timestamp, nonce, echostr) {
		slog.Warn("wecom-http: verify signature failed")
		w.WriteHeader(http.StatusForbidden)
		return
	}

	plain, err := p.decrypt(echostr)
	if err != nil {
		slog.Error("wecom-http: decrypt echostr failed", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	slog.Info("wecom-http: URL verification succeeded")
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, plain)
}

func (p *HTTPChannel) handleMessage(w http.ResponseWriter, r *http.Request, handler channel.MessageHandler) {
	q := r.URL.Query()
	msgSignature := q.Get("msg_signature")
	timestamp := q.Get("timestamp")
	nonce := q.Get("nonce")

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var encMsg xmlEncryptedMsg
	if err := xml.Unmarshal(body, &encMsg); err != nil {
		slog.Error("wecom-http: parse xml failed", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if !p.verifySignature(msgSignature, timestamp, nonce, encMsg.Encrypt) {
		slog.Warn("wecom-http: message signature verification failed")
		w.WriteHeader(http.StatusForbidden)
		return
	}

	plainXML, err := p.decrypt(encMsg.Encrypt)
	if err != nil {
		slog.Error("wecom-http: decrypt message failed", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Return 200 immediately (WeChat Work requires response within 5 seconds)
	w.WriteHeader(http.StatusOK)

	var msg xmlMessage
	if err := xml.Unmarshal([]byte(plainXML), &msg); err != nil {
		slog.Error("wecom-http: parse decrypted xml failed", "error", err)
		return
	}

	// Check allow_from filter
	if !channels.AllowList(p.allowFrom, msg.FromUserName) {
		slog.Info("wecom-http: message from unauthorized user", "user", msg.FromUserName)
		return
	}

	sessionKey := fmt.Sprintf("wecom:%s", msg.FromUserName)
	rctx := replyContext{userID: msg.FromUserName}

	switch msg.MsgType {
	case "text":
		text := stripAtMentions(msg.Content, p.agentID)
		slog.Debug("wecom-http: message received", "user", msg.FromUserName, "text_len", len(text))
		go handler(p, &channel.Message{
			SessionKey: sessionKey, Channel: "wecom",
			MessageID: strconv.FormatInt(msg.MsgId, 10),
			UserID:    msg.FromUserName, UserName: p.resolveUserName(msg.FromUserName),
			Content: text, ReplyCtx: rctx,
		})

	case "image":
		slog.Debug("wecom-http: image received", "user", msg.FromUserName)
		go func() {
			imgData, err := p.downloadMedia(msg.MediaId)
			if err != nil {
				slog.Error("wecom-http: download image failed", "error", err)
				return
			}
			handler(p, &channel.Message{
				SessionKey: sessionKey, Channel: "wecom",
				MessageID: strconv.FormatInt(msg.MsgId, 10),
				UserID:    msg.FromUserName, UserName: p.resolveUserName(msg.FromUserName),
				Images:   []channel.ImageAttachment{{MimeType: "image/jpeg", Data: imgData}},
				ReplyCtx: rctx,
			})
		}()

	case "voice":
		slog.Debug("wecom-http: voice received", "user", msg.FromUserName, "format", msg.Format)
		go func() {
			audioData, err := p.downloadMedia(msg.MediaId)
			if err != nil {
				slog.Error("wecom-http: download voice failed", "error", err)
				return
			}
			format := strings.ToLower(msg.Format)
			if format == "" {
				format = "amr"
			}
			handler(p, &channel.Message{
				SessionKey: sessionKey, Channel: "wecom",
				MessageID: strconv.FormatInt(msg.MsgId, 10),
				UserID:    msg.FromUserName, UserName: p.resolveUserName(msg.FromUserName),
				Audio:    &channel.AudioAttachment{MimeType: "audio/" + format, Data: audioData, Format: format},
				ReplyCtx: rctx,
			})
		}()

	default:
		slog.Debug("wecom-http: ignoring unsupported message type", "type", msg.MsgType)
	}
}

func (p *HTTPChannel) Reply(ctx context.Context, rctx any, content string) error {
	rc, ok := rctx.(replyContext)
	if !ok {
		return fmt.Errorf("wecom-http: invalid reply context type %T", rctx)
	}
	if content == "" {
		return nil
	}

	accessToken, err := p.getAccessToken()
	if err != nil {
		return fmt.Errorf("wecom-http: get access_token: %w", err)
	}

	chunks := splitByBytes(content, 2000)
	for i, chunk := range chunks {
		var sendErr error
		if p.enableMarkdown {
			sendErr = p.sendMarkdown(accessToken, rc.userID, chunk)
		} else {
			sendErr = p.sendText(accessToken, rc.userID, chunk)
		}
		if sendErr != nil {
			slog.Error("wecom-http: send failed", "user", rc.userID, "chunk", i, "error", sendErr)
			return sendErr
		}
	}
	return nil
}

func (p *HTTPChannel) Send(ctx context.Context, rctx any, content string) error {
	return p.Reply(ctx, rctx, content)
}

func (p *HTTPChannel) Stop() error {
	return nil
}

func (p *HTTPChannel) ReconstructReplyCtx(sessionKey string) (any, error) {
	parts := strings.SplitN(sessionKey, ":", 2)
	if len(parts) < 2 || parts[0] != "wecom" {
		return nil, fmt.Errorf("wecom-http: invalid session key %q", sessionKey)
	}
	return replyContext{userID: parts[1]}, nil
}

func (p *HTTPChannel) sendMarkdown(accessToken, toUser, content string) error {
	payload := map[string]any{
		"touser":   toUser,
		"msgtype":  "markdown",
		"agentid":  p.agentID,
		"markdown": map[string]string{"content": content},
	}

	body, _ := json.Marshal(payload)
	apiURL := "https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=" + accessToken

	resp, err := p.apiClient.Post(apiURL, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("wecom-http: send markdown: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("wecom-http: decode send response: %w", err)
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("wecom-http: send markdown failed: %d %s", result.ErrCode, result.ErrMsg)
	}
	return nil
}

func (p *HTTPChannel) sendText(accessToken, toUser, text string) error {
	payload := map[string]any{
		"touser":  toUser,
		"msgtype": "text",
		"agentid": p.agentID,
		"text":    map[string]string{"content": text},
		"safe":    0,
	}

	body, _ := json.Marshal(payload)
	apiURL := "https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=" + accessToken

	resp, err := p.apiClient.Post(apiURL, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("wecom-http: send message: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("wecom-http: decode send response: %w", err)
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("wecom-http: send failed: %d %s", result.ErrCode, result.ErrMsg)
	}
	return nil
}

func (p *HTTPChannel) getAccessToken() (string, error) {
	p.tokenCache.mu.Lock()
	defer p.tokenCache.mu.Unlock()

	if p.tokenCache.token != "" && time.Now().Before(p.tokenCache.expiresAt) {
		return p.tokenCache.token, nil
	}

	apiURL := fmt.Sprintf(
		"https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s",
		p.corpID, p.corpSecret,
	)

	resp, err := p.apiClient.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("wecom-http: request access_token: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("wecom-http: decode token response: %w", err)
	}
	if result.ErrCode != 0 {
		return "", fmt.Errorf("wecom-http: get token failed: %d %s", result.ErrCode, result.ErrMsg)
	}

	p.tokenCache.token = result.AccessToken
	p.tokenCache.expiresAt = time.Now().Add(time.Duration(result.ExpiresIn-60) * time.Second)
	return result.AccessToken, nil
}

func (p *HTTPChannel) downloadMedia(mediaID string) ([]byte, error) {
	accessToken, err := p.getAccessToken()
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}
	u := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/media/get?access_token=%s&media_id=%s", accessToken, mediaID)
	resp, err := p.apiClient.Get(u)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (p *HTTPChannel) resolveUserName(userID string) string {
	if cached, ok := p.userNameCache.Load(userID); ok {
		return cached.(string)
	}
	accessToken, err := p.getAccessToken()
	if err != nil {
		return userID
	}
	apiURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/user/get?access_token=%s&userid=%s", accessToken, url.QueryEscape(userID))
	resp, err := p.apiClient.Get(apiURL)
	if err != nil {
		return userID
	}
	defer resp.Body.Close()
	var result struct {
		ErrCode int    `json:"errcode"`
		Name    string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || result.ErrCode != 0 {
		return userID
	}
	if result.Name != "" {
		p.userNameCache.Store(userID, result.Name)
		return result.Name
	}
	return userID
}

func (p *HTTPChannel) verifySignature(expected, timestamp, nonce, encrypt string) bool {
	parts := []string{p.token, timestamp, nonce, encrypt}
	sort.Strings(parts)
	h := sha1.New()
	h.Write([]byte(strings.Join(parts, "")))
	got := fmt.Sprintf("%x", h.Sum(nil))
	return got == expected
}

func decodeAESKey(encodingAESKey string) ([]byte, error) {
	if len(encodingAESKey) != 43 {
		return nil, fmt.Errorf("EncodingAESKey must be 43 characters, got %d", len(encodingAESKey))
	}
	return base64.StdEncoding.DecodeString(encodingAESKey + "=")
}

func (p *HTTPChannel) decrypt(cipherBase64 string) (string, error) {
	cipherData, err := base64.StdEncoding.DecodeString(cipherBase64)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}

	block, err := aes.NewCipher(p.aesKey)
	if err != nil {
		return "", fmt.Errorf("aes new cipher: %w", err)
	}

	if len(cipherData) < aes.BlockSize || len(cipherData)%aes.BlockSize != 0 {
		return "", fmt.Errorf("invalid ciphertext length %d", len(cipherData))
	}

	iv := p.aesKey[:16]
	mode := cipher.NewCBCDecrypter(block, iv)
	plain := make([]byte, len(cipherData))
	mode.CryptBlocks(plain, cipherData)

	plain = pkcs7Unpad(plain)

	if len(plain) < 20 {
		return "", fmt.Errorf("decrypted data too short")
	}

	msgLen := int(binary.BigEndian.Uint32(plain[16:20]))
	if 20+msgLen > len(plain) {
		return "", fmt.Errorf("invalid message length %d in decrypted data (total %d)", msgLen, len(plain))
	}

	msg := string(plain[20 : 20+msgLen])
	corpID := string(plain[20+msgLen:])

	if corpID != p.corpID {
		return "", fmt.Errorf("corp_id mismatch: expected %s, got %s", p.corpID, corpID)
	}

	return msg, nil
}

func pkcs7Unpad(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	pad := int(data[len(data)-1])
	if pad < 1 || pad > 32 || pad > len(data) {
		return data
	}
	return data[:len(data)-pad]
}

func stripAtMentions(content, agentID string) string {
	agentIDStr := "@" + agentID
	return strings.ReplaceAll(content, agentIDStr, "")
}

var _ channel.Channel = (*HTTPChannel)(nil)
