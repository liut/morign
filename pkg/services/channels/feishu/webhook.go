package feishu

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkcontact "github.com/larksuite/oapi-sdk-go/v3/service/contact/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/liut/morign/pkg/models/channel"
	"github.com/liut/morign/pkg/services/channels"
	"github.com/liut/morign/pkg/services/stores"
)

// HTTPChannel implements channel.Channel for Feishu HTTP webhook callback.
type HTTPChannel struct {
	appID         string
	appSecret     string
	allowFrom     string
	encryptKey    string
	callbackPath  string
	handler       channel.MessageHandler
	server        *http.Server
	dedup         *channels.Dedup
	botOpenID     string
	userNameCache sync.Map
	apiClient     *http.Client
}

type webhookReplyContext struct {
	messageID string
	chatID    string
	userID    string
}

// feishuWebhookEvent is the structure for incoming webhook events
type feishuWebhookEvent struct {
	Schema string `json:"schema"`
	Header struct {
		EventID    string `json:"event_id"`
		EventType  string `json:"event_type"`
		Token      string `json:"token"`
		AppID      string `json:"app_id"`
		TenantKey  string `json:"tenant_key"`
		CreateTime string `json:"create_time"`
	} `json:"header"`
	Event struct {
		// Sender information
		Sender struct {
			SenderID struct {
				OpenID  string `json:"open_id"`
				UserID  string `json:"user_id"`
				UnionID string `json:"union_id"`
			} `json:"sender_id"`
			SenderType string `json:"sender_type"`
			TenantKey  string `json:"tenant_key"`
		} `json:"sender"`
		// Message information
		Message struct {
			MessageID   string `json:"message_id"`
			RootID      string `json:"root_id"`
			ParentID    string `json:"parent_id"`
			CreateTime  string `json:"create_time"`
			ChatID      string `json:"chat_id"`
			ChatType    string `json:"chat_type"`
			MessageType string `json:"message_type"`
			Content     string `json:"content"`
		} `json:"message"`
	} `json:"event"`
}

func newWebhook(opts map[string]any) (channel.Channel, error) {
	appID, _ := opts["app_id"].(string)
	appID = os.ExpandEnv(appID)
	appSecret, _ := opts["app_secret"].(string)
	appSecret = os.ExpandEnv(appSecret)
	if appID == "" || appSecret == "" {
		return nil, fmt.Errorf("feishu-webhook: app_id and app_secret are required")
	}
	allowFrom, _ := opts["allow_from"].(string)
	encryptKey, _ := opts["encrypt_key"].(string)
	encryptKey = os.ExpandEnv(encryptKey)
	callbackPath, _ := opts["callback_path"].(string)
	if callbackPath == "" {
		callbackPath = "/feishu/callback"
	}

	return &HTTPChannel{
		appID:        appID,
		appSecret:    appSecret,
		allowFrom:    allowFrom,
		encryptKey:   encryptKey,
		callbackPath: callbackPath,
		dedup:        channels.NewDedup(stores.SgtRC()),
		apiClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

func (p *HTTPChannel) Name() string { return "feishu" }

func (p *HTTPChannel) Start(handler channel.MessageHandler) error {
	p.handler = handler

	if err := p.fetchBotOpenID(); err != nil {
		slog.Warn("feishu-webhook: failed to get bot open_id", "error", err)
	}

	return nil
}

func (p *HTTPChannel) fetchBotOpenID() error {
	client := lark.NewClient(p.appID, p.appSecret)
	resp, err := client.Get(context.Background(),
		"/open-apis/bot/v3/info", nil, larkcore.AccessTokenTypeTenant)
	if err != nil {
		return fmt.Errorf("fetch bot info: %w", err)
	}
	var result struct {
		Code int `json:"code"`
		Bot  struct {
			OpenID string `json:"open_id"`
		} `json:"bot"`
	}
	if err := json.Unmarshal(resp.RawBody, &result); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if result.Code != 0 {
		return fmt.Errorf("api code=%d", result.Code)
	}
	p.botOpenID = result.Bot.OpenID
	slog.Info("feishu-webhook: bot identified", "open_id", p.botOpenID)
	return nil
}

func (p *HTTPChannel) RegisterHTTPRoutes(r chi.Router, callbackPath string, handler channel.MessageHandler) {
	p.callbackPath = callbackPath
	r.Method(http.MethodGet, callbackPath, http.HandlerFunc(p.handleVerify))
	r.Method(http.MethodPost, callbackPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.handleMessage(w, r, handler)
	}))
	slog.Info("feishu-webhook: routes registered", "path", callbackPath)
}

func (p *HTTPChannel) handleVerify(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	signature := q.Get("signature")
	timestamp := q.Get("timestamp")
	nonce := q.Get("nonce")
	echostr := q.Get("echostr")

	if signature == "" {
		slog.Warn("feishu-webhook: missing signature in verification")
		w.WriteHeader(http.StatusForbidden)
		return
	}

	if !p.verifySignature(signature, timestamp, nonce, echostr) {
		slog.Warn("feishu-webhook: verify signature failed")
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// Decode echostr if encrypted
	var plain string
	if p.encryptKey != "" {
		var err error
		plain, err = p.decrypt(echostr)
		if err != nil {
			slog.Error("feishu-webhook: decrypt echostr failed", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	} else {
		plain = echostr
	}

	slog.Info("feishu-webhook: URL verification succeeded")
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, plain)
}

func (p *HTTPChannel) handleMessage(w http.ResponseWriter, r *http.Request, handler channel.MessageHandler) {
	q := r.URL.Query()
	signature := q.Get("signature")
	timestamp := q.Get("timestamp")
	nonce := q.Get("nonce")

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		slog.Error("feishu-webhook: read body failed", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Parse challenge if present (for URL verification)
	var checkReq struct {
		Challenge string `json:"challenge"`
	}
	if err := json.Unmarshal(body, &checkReq); err == nil && checkReq.Challenge != "" {
		// This is a URL verification request
		if signature == "" || !p.verifySignature(signature, timestamp, nonce, checkReq.Challenge) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"challenge": checkReq.Challenge})
		return
	}

	if !p.verifySignature(signature, timestamp, nonce, string(body)) {
		slog.Warn("feishu-webhook: message signature verification failed")
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// Return 200 immediately (Feishu requires response within 3 seconds)
	w.WriteHeader(http.StatusOK)

	// Parse the webhook event
	var event feishuWebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		slog.Error("feishu-webhook: parse event failed", "error", err)
		return
	}

	// Handle the message event
	if event.Header.EventType == "im.message.receive_v1" {
		p.onWebhookMessage(&event)
	}
}

func (p *HTTPChannel) onWebhookMessage(event *feishuWebhookEvent) {
	msg := &event.Event.Message
	sender := &event.Event.Sender

	msgType := msg.MessageType
	chatID := msg.ChatID
	chatType := msg.ChatType
	threadID := msg.RootID
	userID := sender.SenderID.OpenID
	messageID := msg.MessageID

	// Filter: skip messages without message ID
	if messageID == "" {
		slog.Debug("feishu-webhook: message without ID ignored")
		return
	}

	// Debug logging for received messages
	slog.Info("feishu-webhook: message received",
		"msg_id", messageID,
		"msg_type", msgType,
		"chat_id", chatID,
		"chat_type", chatType,
		"thread_id", threadID,
		"user_id", userID,
		"content_len", len(msg.Content),
	)

	// Deduplicate
	if p.dedup != nil {
		isDup, _ := p.dedup.IsDuplicate(context.Background(), "feishu", messageID)
		if isDup {
			slog.Info("feishu-webhook: skipping duplicate message", "msg_id", messageID)
			return
		}
	}

	// Check allow_from filter
	if !channels.AllowList(p.allowFrom, userID) {
		slog.Info("feishu-webhook: message from unauthorized user", "user", userID)
		return
	}

	sessionKey := fmt.Sprintf("feishu:%s:%s", chatID, userID)
	rctx := webhookReplyContext{
		messageID: messageID,
		chatID:    chatID,
		userID:    userID,
	}

	switch msgType {
	case "text":
		var textBody struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(msg.Content), &textBody); err != nil {
			slog.Error("feishu-webhook: failed to parse text content", "error", err)
			return
		}
		text := strings.TrimSpace(textBody.Text)
		if text == "" {
			slog.Debug("feishu-webhook: dropping empty text")
			return
		}
		go p.handler(p, &channel.Message{
			SessionKey: sessionKey, Channel: "feishu",
			MessageID: messageID,
			UserID:    userID, UserName: p.resolveUserName(userID),
			Content: text, ReplyCtx: rctx,
		})

	case "image":
		var imgBody struct {
			ImageKey string `json:"image_key"`
		}
		if err := json.Unmarshal([]byte(msg.Content), &imgBody); err != nil {
			slog.Error("feishu-webhook: failed to parse image content", "error", err)
			return
		}
		imgData, mimeType, err := p.downloadImage(messageID, imgBody.ImageKey)
		if err != nil {
			slog.Error("feishu-webhook: download image failed", "error", err)
			return
		}
		go p.handler(p, &channel.Message{
			SessionKey: sessionKey, Channel: "feishu",
			MessageID: messageID,
			UserID:    userID, UserName: p.resolveUserName(userID),
			Images:   []channel.ImageAttachment{{MimeType: mimeType, Data: imgData}},
			ReplyCtx: rctx,
		})

	case "audio":
		var audioBody struct {
			FileKey  string `json:"file_key"`
			Duration int    `json:"duration"`
		}
		if err := json.Unmarshal([]byte(msg.Content), &audioBody); err != nil {
			slog.Error("feishu-webhook: failed to parse audio content", "error", err)
			return
		}
		slog.Debug("feishu-webhook: audio received", "user", userID, "file_key", audioBody.FileKey)
		audioData, err := p.downloadResource(messageID, audioBody.FileKey, "file")
		if err != nil {
			slog.Error("feishu-webhook: download audio failed", "error", err)
			return
		}
		go p.handler(p, &channel.Message{
			SessionKey: sessionKey, Channel: "feishu",
			MessageID: messageID,
			UserID:    userID, UserName: p.resolveUserName(userID),
			Audio:    &channel.AudioAttachment{MimeType: "audio/opus", Data: audioData, Format: "ogg"},
			ReplyCtx: rctx,
		})

	default:
		slog.Debug("feishu-webhook: ignoring unsupported message type", "type", msgType)
	}
}

func (p *HTTPChannel) Reply(ctx context.Context, rctx any, content string) error {
	rc, ok := rctx.(webhookReplyContext)
	if !ok {
		return fmt.Errorf("feishu-webhook: invalid reply context type %T", rctx)
	}
	if content == "" {
		return nil
	}

	client := lark.NewClient(p.appID, p.appSecret)
	msgType, msgBody := buildReplyContent(content)

	resp, err := client.Im.Message.Reply(ctx, larkim.NewReplyMessageReqBuilder().
		MessageId(rc.messageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType(msgType).
			Content(msgBody).
			ReplyInThread(true).
			Build()).
		Build())
	if err != nil {
		return fmt.Errorf("feishu-webhook: reply api call: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("feishu-webhook: reply failed code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func (p *HTTPChannel) Send(ctx context.Context, rctx any, content string) error {
	rc, ok := rctx.(webhookReplyContext)
	if !ok {
		return fmt.Errorf("feishu-webhook: invalid reply context type %T", rctx)
	}
	if content == "" {
		return nil
	}
	if rc.chatID == "" {
		return fmt.Errorf("feishu-webhook: chatID is empty, cannot send proactive message")
	}

	client := lark.NewClient(p.appID, p.appSecret)
	msgType, _ := buildReplyContent(content)

	chunks := splitByBytes(content, 4000)
	for _, chunk := range chunks {
		_, body := buildReplyContent(chunk)
		resp, err := client.Im.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().
			ReceiveIdType(larkim.ReceiveIdTypeChatId).
			Body(larkim.NewCreateMessageReqBodyBuilder().
				ReceiveId(rc.chatID).
				MsgType(msgType).
				Content(body).
				Build()).
			Build())
		if err != nil {
			return fmt.Errorf("feishu-webhook: send api call: %w", err)
		}
		if !resp.Success() {
			return fmt.Errorf("feishu-webhook: send failed code=%d msg=%s", resp.Code, resp.Msg)
		}
	}
	return nil
}

func (p *HTTPChannel) Stop() error {
	if p.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := p.server.Shutdown(ctx); err != nil {
			slog.Error("feishu-webhook: server shutdown error", "error", err)
		}
	}
	return nil
}

func (p *HTTPChannel) ReconstructReplyCtx(sessionKey string) (any, error) {
	parts := strings.SplitN(sessionKey, ":", 3)
	if len(parts) < 3 || parts[0] != "feishu" {
		return nil, fmt.Errorf("feishu-webhook: invalid session key %q", sessionKey)
	}
	return webhookReplyContext{chatID: parts[1], userID: parts[2]}, nil
}

func (p *HTTPChannel) resolveUserName(openID string) string {
	if cached, ok := p.userNameCache.Load(openID); ok {
		return cached.(string)
	}
	client := lark.NewClient(p.appID, p.appSecret)
	resp, err := client.Contact.User.Get(context.Background(),
		larkcontact.NewGetUserReqBuilder().
			UserId(openID).
			UserIdType("open_id").
			Build())
	if err != nil {
		slog.Debug("feishu-webhook: resolve user name failed", "open_id", openID, "error", err)
		return openID
	}
	if !resp.Success() || resp.Data == nil || resp.Data.User == nil || resp.Data.User.Name == nil {
		return openID
	}
	name := *resp.Data.User.Name
	p.userNameCache.Store(openID, name)
	return name
}

func (p *HTTPChannel) downloadImage(messageID, imageKey string) ([]byte, string, error) {
	client := lark.NewClient(p.appID, p.appSecret)
	resp, err := client.Im.MessageResource.Get(context.Background(),
		larkim.NewGetMessageResourceReqBuilder().
			MessageId(messageID).
			FileKey(imageKey).
			Type("image").
			Build())
	if err != nil {
		return nil, "", fmt.Errorf("feishu-webhook: image API: %w", err)
	}
	if !resp.Success() {
		return nil, "", fmt.Errorf("feishu-webhook: image API code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.File == nil {
		return nil, "", fmt.Errorf("feishu-webhook: image API returned nil file body")
	}
	data, err := io.ReadAll(resp.File)
	if err != nil {
		return nil, "", fmt.Errorf("feishu-webhook: read image: %w", err)
	}
	mimeType := detectMimeType(data)
	return data, mimeType, nil
}

func (p *HTTPChannel) downloadResource(messageID, fileKey, resType string) ([]byte, error) {
	client := lark.NewClient(p.appID, p.appSecret)
	resp, err := client.Im.MessageResource.Get(context.Background(),
		larkim.NewGetMessageResourceReqBuilder().
			MessageId(messageID).
			FileKey(fileKey).
			Type(resType).
			Build())
	if err != nil {
		return nil, fmt.Errorf("feishu-webhook: resource API: %w", err)
	}
	if !resp.Success() {
		return nil, fmt.Errorf("feishu-webhook: resource API code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.File == nil {
		return nil, fmt.Errorf("feishu-webhook: resource API returned nil file body")
	}
	return io.ReadAll(resp.File)
}

func (p *HTTPChannel) verifySignature(expected, timestamp, nonce, encrypt string) bool {
	parts := []string{timestamp, nonce, encrypt}
	sort.Strings(parts)
	h := sha256.New()
	h.Write([]byte(strings.Join(parts, "")))
	got := hex.EncodeToString(h.Sum(nil))
	return got == expected
}

func (p *HTTPChannel) decrypt(echostr string) (string, error) {
	// TODO: Implement AES decryption for encrypted messages
	// For now, return the echostr as-is
	return echostr, nil
}

var _ channel.Channel = (*HTTPChannel)(nil)
