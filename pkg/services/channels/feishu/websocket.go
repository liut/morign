package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkcontact "github.com/larksuite/oapi-sdk-go/v3/service/contact/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/liut/morign/pkg/models/channel"
	"github.com/liut/morign/pkg/services/channels"
	"github.com/liut/morign/pkg/services/stores"
)

// WSChannel implements channel.Channel using Feishu WebSocket long-connection mode.
type WSChannel struct {
	appID         string
	appSecret     string
	allowFrom     string
	wsClient      *larkws.Client
	handler       channel.MessageHandler
	eventHandler  *dispatcher.EventDispatcher
	ctx           context.Context
	cancel        context.CancelFunc
	dedup         *channels.Dedup
	botOpenID     string
	userNameCache sync.Map // open_id -> display name
}

type wsReplyContext struct {
	messageID string
	chatID    string
	userID    string
}

func newWebSocket(opts map[string]any) (channel.Channel, error) {
	appID, _ := opts["app_id"].(string)
	appID = os.ExpandEnv(appID)
	appSecret, _ := opts["app_secret"].(string)
	appSecret = os.ExpandEnv(appSecret)
	if appID == "" || appSecret == "" {
		return nil, fmt.Errorf("feishu-ws: app_id and app_secret are required")
	}
	allowFrom, _ := opts["allow_from"].(string)

	return &WSChannel{
		appID:     appID,
		appSecret: appSecret,
		allowFrom: allowFrom,
		dedup:     channels.NewDedup(stores.SgtRC()),
	}, nil
}

func (p *WSChannel) Name() string { return "feishu" }

func (p *WSChannel) Start(handler channel.MessageHandler) error {
	p.handler = handler
	p.ctx, p.cancel = context.WithCancel(context.Background())

	p.eventHandler = dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			return p.onMessage(event)
		})

	wsOpts := []larkws.ClientOption{
		larkws.WithEventHandler(p.eventHandler),
		larkws.WithLogger(newDefaultLog()),
	}

	p.wsClient = larkws.NewClient(p.appID, p.appSecret, wsOpts...)

	go func() {
		if err := p.wsClient.Start(p.ctx); err != nil {
			slog.Error("feishu-ws: websocket error", "error", err)
		}
	}()

	if err := p.fetchBotOpenID(); err != nil {
		slog.Warn("feishu-ws: failed to get bot open_id", "error", err)
	}

	return nil
}

func (p *WSChannel) fetchBotOpenID() error {
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
	slog.Info("feishu-ws: bot identified", "open_id", p.botOpenID)
	return nil
}

func (p *WSChannel) Stop() error {
	if p.cancel != nil {
		p.cancel()
	}
	return nil
}

func (p *WSChannel) Reply(ctx context.Context, rctx any, content string) error {
	rc, ok := rctx.(wsReplyContext)
	if !ok {
		return fmt.Errorf("feishu-ws: invalid reply context type %T", rctx)
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
		return fmt.Errorf("feishu-ws: reply api call: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("feishu-ws: reply failed code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func (p *WSChannel) Send(ctx context.Context, rctx any, content string) error {
	rc, ok := rctx.(wsReplyContext)
	if !ok {
		return fmt.Errorf("feishu-ws: invalid reply context type %T", rctx)
	}
	if content == "" {
		return nil
	}
	if rc.chatID == "" {
		return fmt.Errorf("feishu-ws: chatID is empty, cannot send proactive message")
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
			return fmt.Errorf("feishu-ws: send api call: %w", err)
		}
		if !resp.Success() {
			return fmt.Errorf("feishu-ws: send failed code=%d msg=%s", resp.Code, resp.Msg)
		}
	}
	return nil
}

func (p *WSChannel) ReconstructReplyCtx(sessionKey string) (any, error) {
	parts := strings.SplitN(sessionKey, ":", 3)
	if len(parts) < 3 || parts[0] != "feishu" {
		return nil, fmt.Errorf("feishu-ws: invalid session key %q", sessionKey)
	}
	return wsReplyContext{chatID: parts[1], userID: parts[2]}, nil
}

func (p *WSChannel) onMessage(event *larkim.P2MessageReceiveV1) error {
	slog.Debug("feishu-ws: onMessage called")

	msg := event.Event.Message
	sender := event.Event.Sender

	msgType := ""
	if msg.MessageType != nil {
		msgType = *msg.MessageType
	}

	chatID := ""
	if msg.ChatId != nil {
		chatID = *msg.ChatId
	}

	userID := ""
	if sender.SenderId != nil && sender.SenderId.OpenId != nil {
		userID = *sender.SenderId.OpenId
	}

	messageID := ""
	if msg.MessageId != nil {
		messageID = *msg.MessageId
	}

	// Filter: skip messages without message ID
	if messageID == "" {
		slog.Debug("feishu-ws: message without ID ignored")
		return nil
	}

	// Debug logging for received messages
	slog.Info("feishu-ws: message received",
		"msg_id", messageID,
		"msg_type", msgType,
		"chat_id", chatID,
		"chat_type", ptrStr(msg.ChatType),
		"thread_id", ptrStr(msg.ThreadId),
		"user_id", userID,
		"content_len", len(ptrStr(msg.Content)),
	)

	// Deduplicate
	if p.dedup != nil {
		isDup, _ := p.dedup.IsDuplicate(context.Background(), "feishu", messageID)
		if isDup {
			slog.Info("feishu-ws: skipping duplicate message", "msg_id", messageID)
			return nil
		}
	}

	// Check allow_from filter
	if !channels.AllowList(p.allowFrom, userID) {
		slog.Info("feishu-ws: message from unauthorized user", "user", userID)
		return nil
	}

	sessionKey := fmt.Sprintf("feishu:%s:%s", chatID, userID)
	rctx := wsReplyContext{
		messageID: messageID,
		chatID:    chatID,
		userID:    userID,
	}

	switch msgType {
	case "text":
		var textBody struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(ptrStr(msg.Content)), &textBody); err != nil {
			slog.Error("feishu-ws: failed to parse text content", "error", err)
			return nil
		}
		text := stripMentions(textBody.Text, msg.Mentions, p.botOpenID)
		if text == "" {
			slog.Debug("feishu-ws: dropping empty text after mention stripping")
			return nil
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
		if err := json.Unmarshal([]byte(ptrStr(msg.Content)), &imgBody); err != nil {
			slog.Error("feishu-ws: failed to parse image content", "error", err)
			return nil
		}
		imgData, mimeType, err := p.downloadImage(messageID, imgBody.ImageKey)
		if err != nil {
			slog.Error("feishu-ws: download image failed", "error", err)
			return nil
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
		if err := json.Unmarshal([]byte(ptrStr(msg.Content)), &audioBody); err != nil {
			slog.Error("feishu-ws: failed to parse audio content", "error", err)
			return nil
		}
		slog.Debug("feishu-ws: audio received", "user", userID, "file_key", audioBody.FileKey)
		audioData, err := p.downloadResource(messageID, audioBody.FileKey, "file")
		if err != nil {
			slog.Error("feishu-ws: download audio failed", "error", err)
			return nil
		}
		go p.handler(p, &channel.Message{
			SessionKey: sessionKey, Channel: "feishu",
			MessageID: messageID,
			UserID:    userID, UserName: p.resolveUserName(userID),
			Audio:    &channel.AudioAttachment{MimeType: "audio/opus", Data: audioData, Format: "ogg"},
			ReplyCtx: rctx,
		})

	default:
		slog.Debug("feishu-ws: ignoring unsupported message type", "type", msgType)
	}

	return nil
}

func (p *WSChannel) resolveUserName(openID string) string {
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
		slog.Debug("feishu-ws: resolve user name failed", "open_id", openID, "error", err)
		return openID
	}
	if !resp.Success() || resp.Data == nil || resp.Data.User == nil || resp.Data.User.Name == nil {
		return openID
	}
	name := *resp.Data.User.Name
	p.userNameCache.Store(openID, name)
	return name
}

func (p *WSChannel) downloadImage(messageID, imageKey string) ([]byte, string, error) {
	client := lark.NewClient(p.appID, p.appSecret)
	resp, err := client.Im.MessageResource.Get(context.Background(),
		larkim.NewGetMessageResourceReqBuilder().
			MessageId(messageID).
			FileKey(imageKey).
			Type("image").
			Build())
	if err != nil {
		return nil, "", fmt.Errorf("feishu-ws: image API: %w", err)
	}
	if !resp.Success() {
		return nil, "", fmt.Errorf("feishu-ws: image API code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.File == nil {
		return nil, "", fmt.Errorf("feishu-ws: image API returned nil file body")
	}
	data, err := ioReadAll(resp.File)
	if err != nil {
		return nil, "", fmt.Errorf("feishu-ws: read image: %w", err)
	}
	mimeType := detectMimeType(data)
	return data, mimeType, nil
}

func (p *WSChannel) downloadResource(messageID, fileKey, resType string) ([]byte, error) {
	client := lark.NewClient(p.appID, p.appSecret)
	resp, err := client.Im.MessageResource.Get(context.Background(),
		larkim.NewGetMessageResourceReqBuilder().
			MessageId(messageID).
			FileKey(fileKey).
			Type(resType).
			Build())
	if err != nil {
		return nil, fmt.Errorf("feishu-ws: resource API: %w", err)
	}
	if !resp.Success() {
		return nil, fmt.Errorf("feishu-ws: resource API code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.File == nil {
		return nil, fmt.Errorf("feishu-ws: resource API returned nil file body")
	}
	return ioReadAll(resp.File)
}

func ioReadAll(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	var buf []byte
	tmp := make([]byte, 4096)
	for {
		n, err := r.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return buf, nil
}

func detectMimeType(data []byte) string {
	if len(data) >= 8 {
		if data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' {
			return "image/png"
		}
		if data[0] == 0xFF && data[1] == 0xD8 {
			return "image/jpeg"
		}
		if string(data[:4]) == "GIF8" {
			return "image/gif"
		}
	}
	return "image/png"
}

func buildReplyContent(content string) (msgType string, body string) {
	b, _ := json.Marshal(map[string]string{"text": content})
	return "text", string(b)
}

func stripMentions(text string, mentions []*larkim.MentionEvent, botOpenID string) string {
	if len(mentions) == 0 {
		return text
	}
	for _, m := range mentions {
		if m.Key == nil {
			continue
		}
		if botOpenID != "" && m.Id != nil && m.Id.OpenId != nil && *m.Id.OpenId == botOpenID {
			text = strings.ReplaceAll(text, *m.Key, "")
		} else if m.Name != nil && *m.Name != "" {
			text = strings.ReplaceAll(text, *m.Key, "@"+*m.Name)
		} else {
			text = strings.ReplaceAll(text, *m.Key, "")
		}
	}
	return strings.TrimSpace(text)
}

func splitByBytes(s string, maxBytes int) []string {
	if len(s) <= maxBytes {
		return []string{s}
	}
	var parts []string
	for len(s) > 0 {
		end := maxBytes
		if end > len(s) {
			end = len(s)
		}
		for end > 0 && end < len(s) && s[end]>>6 == 0b10 {
			end--
		}
		if end == 0 {
			end = maxBytes
		}
		parts = append(parts, s[:end])
		s = s[end:]
	}
	return parts
}

func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

var _ channel.Channel = (*WSChannel)(nil)
