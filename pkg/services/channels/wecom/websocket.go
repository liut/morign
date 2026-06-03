package wecom

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cupogo/andvari/models/oid"
	"github.com/gorilla/websocket"
	"github.com/liut/morign/pkg/models/channel"
	"github.com/liut/morign/pkg/services/channels"
	"github.com/liut/morign/pkg/services/stores"
)

const (
	wsEndpoint     = "wss://openws.work.weixin.qq.com"
	wsPingInterval = 30 * time.Second
	wsMaxBackoff   = 30 * time.Second
	wsMaxMissed    = 2
)

const wsAckTimeout = 5 * time.Second

// WSChannel implements channel.Channel using WeChat Work WebSocket long-connection mode.
type WSChannel struct {
	botID     string
	secret    string
	allowFrom string
	conn      *websocket.Conn
	handler   channel.MessageHandler
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	dedup     *channels.Dedup
	// reqSeq      atomic.Int64
	missedPong  atomic.Int32
	pendingAcks sync.Map
}

// wsReplyContext holds context needed to reply to a specific message.
type wsReplyContext struct {
	reqID    string
	chatID   string
	chatType string
	userID   string
}

type wsFrame struct {
	Cmd     string          `json:"cmd,omitempty"`
	Headers wsFrameHeaders  `json:"headers"`
	Body    json.RawMessage `json:"body,omitempty"`
	ErrCode *int            `json:"errcode,omitempty"`
	ErrMsg  string          `json:"errmsg,omitempty"`
}

type wsFrameHeaders struct {
	ReqID string `json:"req_id"`
}

type wsMsgCallbackBody struct {
	MsgID    string `json:"msgid"`
	AibotID  string `json:"aibotid"`
	ChatID   string `json:"chatid"`
	ChatType string `json:"chattype"`
	From     struct {
		UserID string `json:"userid"`
	} `json:"from"`
	MsgType string `json:"msgtype"`
	Text    struct {
		Content string `json:"content"`
	} `json:"text"`
	Voice struct {
		Text string `json:"text"`
	} `json:"voice"`
	CreateTime int64 `json:"create_time"`
}

// wsStreamFrame is the frame structure for streaming message responses.
type wsStreamFrame struct {
	Cmd     string         `json:"cmd"`
	Headers wsFrameHeaders `json:"headers"`
	Body    wsStreamBody   `json:"body"`
}

type wsStreamBody struct {
	MsgType string          `json:"msgtype"`
	Stream  wsStreamContent `json:"stream"`
}

type wsStreamContent struct {
	ID      string `json:"id"`
	Finish  bool   `json:"finish"`
	Content string `json:"content"`
}

func newWebSocket(opts map[string]any) (channel.Channel, error) {
	botID, _ := opts["bot_id"].(string)
	botID = os.ExpandEnv(botID)
	secret, _ := opts["bot_secret"].(string)
	secret = os.ExpandEnv(secret)
	if botID == "" || secret == "" {
		return nil, fmt.Errorf("wecom-ws: bot_id and bot_secret are required for websocket mode")
	}
	allowFrom, _ := opts["allow_from"].(string)

	return &WSChannel{
		botID:     botID,
		secret:    secret,
		allowFrom: allowFrom,
		dedup:     channels.NewDedup(stores.SgtRC()),
	}, nil
}

func (p *WSChannel) generateReqID(prefix string) string {
	id := oid.NewID(oid.OtEvent)
	return fmt.Sprintf("%s_%s", prefix, id)
}

func (p *WSChannel) replyCtx(rctx any) (wsReplyContext, error) {
	rc, ok := rctx.(wsReplyContext)
	if !ok {
		return wsReplyContext{}, fmt.Errorf("wecom-ws: invalid reply context type %T", rctx)
	}
	return rc, nil
}

func (p *WSChannel) Name() string { return "wecom" }

func (p *WSChannel) Start(handler channel.MessageHandler) error {
	p.handler = handler
	p.ctx, p.cancel = context.WithCancel(context.Background())
	go p.connectLoop()
	return nil
}

func (p *WSChannel) connectLoop() {
	backoff := time.Second
	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		start := time.Now()
		err := p.runConnection()
		if p.ctx.Err() != nil {
			return
		}

		if time.Since(start) > 2*wsPingInterval {
			backoff = time.Second
		}

		slog.Warn("wecom-ws: connection lost, reconnecting", "error", err, "backoff", backoff)
		select {
		case <-time.After(backoff):
		case <-p.ctx.Done():
			return
		}

		backoff *= 2
		if backoff > wsMaxBackoff {
			backoff = wsMaxBackoff
		}
	}
}

func (p *WSChannel) runConnection() error {
	slog.Info("wecom-ws: connecting", "endpoint", wsEndpoint)

	conn, _, err := websocket.DefaultDialer.DialContext(p.ctx, wsEndpoint, nil)
	if err != nil {
		slog.Info("wecom-ws: dial failed", "error", err)
		return fmt.Errorf("dial: %w", err)
	}

	p.mu.Lock()
	p.conn = conn
	p.mu.Unlock()

	defer func() {
		slog.Debug("wecom-ws: connection closed")
		p.mu.Lock()
		p.conn = nil
		p.mu.Unlock()
		conn.Close()

		var staleKeys []any
		p.pendingAcks.Range(func(key, value any) bool {
			if ch, ok := value.(chan error); ok {
				select {
				case ch <- fmt.Errorf("wecom-ws: connection closed"):
				default:
				}
			}
			staleKeys = append(staleKeys, key)
			return true
		})
		for _, k := range staleKeys {
			p.pendingAcks.Delete(k)
		}
	}()

	subReqID := p.generateReqID("aibot_subscribe")
	subFrame := map[string]any{
		"cmd":     "aibot_subscribe",
		"headers": map[string]string{"req_id": subReqID},
		"body": map[string]string{
			"bot_id": p.botID,
			"secret": p.secret,
		},
	}
	if err := p.writeJSON(subFrame); err != nil {
		slog.Info("wecom-ws: subscribe write failed", "error", err)
		return fmt.Errorf("subscribe: %w", err)
	}

	var subResp wsFrame
	if err := conn.ReadJSON(&subResp); err != nil {
		slog.Info("wecom-ws: subscribe response failed", "error", err)
		return fmt.Errorf("subscribe response: %w", err)
	}
	if subResp.ErrCode == nil || *subResp.ErrCode != 0 {
		errCode := 0
		if subResp.ErrCode != nil {
			errCode = *subResp.ErrCode
		}
		slog.Info("wecom-ws: subscribe rejected", "errcode", errCode, "errmsg", subResp.ErrMsg)
		return fmt.Errorf("subscribe failed: errcode=%d errmsg=%s", errCode, subResp.ErrMsg)
	}
	slog.Info("wecom-ws: subscribed successfully", "bot_id", p.botID, "subReqID", subReqID)
	p.missedPong.Store(0)

	heartCtx, heartCancel := context.WithCancel(p.ctx)
	defer heartCancel()
	slog.Debug("wecom-ws: heartbeat starting")
	go p.heartbeat(heartCtx, conn)

	for {
		select {
		case <-p.ctx.Done():
			return p.ctx.Err()
		default:
		}

		_, raw, err := conn.ReadMessage()
		if err != nil {
			slog.Info("wecom-ws: read message failed", "error", err)
			return fmt.Errorf("read: %w", err)
		}

		var frame wsFrame
		if err := json.Unmarshal(raw, &frame); err != nil {
			slog.Warn("wecom-ws: invalid json", "error", err)
			continue
		}

		p.handleFrame(frame)
	}
}

func (p *WSChannel) handleFrame(frame wsFrame) {
	switch frame.Cmd {
	case "aibot_msg_callback":
		p.handleMsgCallback(frame)
	case "aibot_event_callback":
		slog.Debug("wecom-ws: event callback received (ignored)", "req_id", frame.Headers.ReqID)
	case "ping", "":
		// pong response: cmd can be "ping" or "" depending on server version
		p.missedPong.Store(0)
		slog.Debug("wecom-ws: heartbeat ack received", "cmd", frame.Cmd, "req_id", frame.Headers.ReqID)
	case "aibot_subscribe":
		slog.Debug("wecom-ws: late subscribe ack", "req_id", frame.Headers.ReqID)
	default:
		var ackErr error
		if frame.ErrCode != nil && *frame.ErrCode != 0 {
			ackErr = fmt.Errorf("wecom-ws: ack error: errcode=%d errmsg=%s", *frame.ErrCode, frame.ErrMsg)
			slog.Warn("wecom-ws: reply/send ack error", "req_id", frame.Headers.ReqID, "errcode", *frame.ErrCode, "errmsg", frame.ErrMsg)
		}
		if ch, ok := p.pendingAcks.LoadAndDelete(frame.Headers.ReqID); ok {
			ch.(chan error) <- ackErr
		}
	}
}

func (p *WSChannel) heartbeat(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			missed := int(p.missedPong.Load())
			if missed >= wsMaxMissed {
				slog.Warn("wecom-ws: no heartbeat ack, connection considered dead", "missed", missed)
				conn.Close()
				return
			}

			p.missedPong.Add(1)
			pingFrame := map[string]any{
				"cmd":     "ping",
				"headers": map[string]string{"req_id": p.generateReqID("ping")},
			}
			if err := p.writeJSON(pingFrame); err != nil {
				slog.Warn("wecom-ws: ping failed", "error", err)
				return
			}
			slog.Debug("wecom-ws: ping sent", "missed_pong", p.missedPong.Load())
		}
	}
}

func (p *WSChannel) handleMsgCallback(frame wsFrame) {
	var body wsMsgCallbackBody
	if err := json.Unmarshal(frame.Body, &body); err != nil {
		slog.Warn("wecom-ws: parse msg_callback body failed", "error", err)
		return
	}

	reqID := frame.Headers.ReqID

	if p.dedup != nil {
		ctx := context.Background()
		isDup, _ := p.dedup.IsDuplicate(ctx, "wecom", body.MsgID)
		if isDup {
			slog.Info("wecom-ws: skipping duplicate message", "msg_id", body.MsgID)
			return
		}
	}

	// Check allow_from filter
	if !channels.AllowList(p.allowFrom, body.From.UserID) {
		slog.Info("wecom-ws: message from unauthorized user", "user", body.From.UserID)
		return
	}

	chatID := body.ChatID
	if chatID == "" {
		chatID = body.From.UserID
	}

	sessionKey := fmt.Sprintf("wecom:%s:%s", chatID, body.From.UserID)
	rctx := wsReplyContext{
		reqID:    reqID,
		chatID:   chatID,
		chatType: body.ChatType,
		userID:   body.From.UserID,
	}

	chatName := ""
	if body.ChatType == "group" {
		chatName = body.ChatID
	}

	switch body.MsgType {
	case "text":
		text := stripWeComAtMentions(body.Text.Content, p.botID)
		slog.Debug("wecom-ws: text received", "user", body.From.UserID, "len", len(text))
		go p.handler(p, &channel.Message{
			SessionKey: sessionKey, Channel: "wecom",
			MessageID: body.MsgID,
			UserID:    body.From.UserID, UserName: body.From.UserID,
			ChatName: chatName,
			Content:  text, ReplyCtx: rctx,
		})

	case "voice":
		text := stripWeComAtMentions(body.Voice.Text, p.botID)
		if text == "" {
			slog.Debug("wecom-ws: voice message with empty transcription, ignoring")
			return
		}
		slog.Debug("wecom-ws: voice received (transcribed)", "user", body.From.UserID, "len", len(text))
		go p.handler(p, &channel.Message{
			SessionKey: sessionKey, Channel: "wecom",
			MessageID: body.MsgID,
			UserID:    body.From.UserID, UserName: body.From.UserID,
			ChatName: chatName,
			Content:  text, ReplyCtx: rctx, FromVoice: true,
		})

	default:
		slog.Debug("wecom-ws: ignoring unsupported message type", "type", body.MsgType)
	}
}

func (p *WSChannel) Reply(ctx context.Context, rctx any, content string) error {
	if content == "" {
		return nil
	}
	streamID, err := p.StartStream(ctx, rctx, content)
	if err != nil {
		return err
	}
	return p.FinishStream(ctx, rctx, streamID, content)
}

// buildStreamFrame builds a streaming message frame.
func (p *WSChannel) buildStreamFrame(rc wsReplyContext, streamID, content string, finish bool) wsStreamFrame {
	return wsStreamFrame{
		Cmd:     "aibot_respond_msg",
		Headers: wsFrameHeaders{ReqID: rc.reqID},
		Body: wsStreamBody{
			MsgType: "stream",
			Stream: wsStreamContent{
				ID:      streamID,
				Finish:  finish,
				Content: content,
			},
		},
	}
}

// StartStream starts a new streaming message, returns the stream ID.
func (p *WSChannel) StartStream(ctx context.Context, rctx any, content string) (string, error) {
	rc, err := p.replyCtx(rctx)
	if err != nil {
		return "", err
	}
	streamID := p.generateReqID("stream")
	frame := p.buildStreamFrame(rc, streamID, content, false)
	if err := p.writeJSON(frame); err != nil {
		slog.Error("wecom-ws: start stream failed", "user", rc.userID, "error", err)
		return "", err
	}
	slog.Debug("wecom-ws: stream started", "user", rc.userID, "streamID", streamID, "len", len(content))
	return streamID, nil
}

// AppendStream appends content to an existing stream.
func (p *WSChannel) AppendStream(ctx context.Context, rctx any, streamID string, content string) error {
	rc, err := p.replyCtx(rctx)
	if err != nil {
		return err
	}
	frame := p.buildStreamFrame(rc, streamID, content, false)
	if err := p.writeJSON(frame); err != nil {
		slog.Warn("wecom-ws: append stream failed", "user", rc.userID, "streamID", streamID, "error", err)
		return err
	}
	slog.Debug("wecom-ws: stream appended", "user", rc.userID, "streamID", streamID, "len", len(content))
	return nil
}

// FinishStream ends the streaming message with final content.
func (p *WSChannel) FinishStream(ctx context.Context, rctx any, streamID string, finalContent string) error {
	rc, err := p.replyCtx(rctx)
	if err != nil {
		return err
	}
	frame := p.buildStreamFrame(rc, streamID, finalContent, true)
	if err := p.writeJSON(frame); err != nil {
		slog.Error("wecom-ws: finish stream failed", "user", rc.userID, "streamID", streamID, "error", err)
		return err
	}
	slog.Debug("wecom-ws: stream finished", "user", rc.userID, "streamID", streamID, "content_len", len(finalContent))
	return nil
}

func (p *WSChannel) Send(ctx context.Context, rctx any, content string) error {
	rc, err := p.replyCtx(rctx)
	if err != nil {
		return err
	}
	if content == "" {
		return nil
	}
	if rc.chatID == "" {
		slog.Info("wecom-ws: chatID is empty, cannot send proactive message")
		return fmt.Errorf("wecom-ws: chatID is empty, cannot send proactive message")
	}

	chunks := splitByBytes(content, 2000)
	for i, chunk := range chunks {
		reqID := p.generateReqID("aibot_send_msg")
		frame := map[string]any{
			"cmd":     "aibot_send_msg",
			"headers": map[string]string{"req_id": reqID},
			"body": map[string]any{
				"chatid":  rc.chatID,
				"msgtype": "markdown",
				"markdown": map[string]string{
					"content": chunk,
				},
			},
		}
		if err := p.writeAndWaitAck(ctx, frame, reqID); err != nil {
			slog.Error("wecom-ws: send failed", "user", rc.userID, "chunk", i, "error", err)
			return err
		}
	}
	slog.Debug("wecom-ws: message sent", "user", rc.userID, "chunks", len(chunks))
	return nil
}

func (p *WSChannel) ReconstructReplyCtx(sessionKey string) (any, error) {
	parts := strings.SplitN(sessionKey, ":", 3)
	if len(parts) < 3 || parts[0] != "wecom" {
		return nil, fmt.Errorf("wecom-ws: invalid session key %q", sessionKey)
	}
	return wsReplyContext{chatID: parts[1], userID: parts[2]}, nil
}

func (p *WSChannel) Stop() error {
	if p.cancel != nil {
		p.cancel()
	}
	p.mu.Lock()
	conn := p.conn
	p.mu.Unlock()
	if conn != nil {
		return conn.Close()
	}
	return nil
}

func (p *WSChannel) writeJSON(v any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn == nil {
		slog.Debug("wecom-ws: writeJSON called while not connected")
		return fmt.Errorf("wecom-ws: not connected")
	}
	return p.conn.WriteJSON(v)
}

func (p *WSChannel) writeAndWaitAck(ctx context.Context, frame map[string]any, reqID string) error {
	ch := make(chan error, 1)
	p.pendingAcks.Store(reqID, ch)

	if err := p.writeJSON(frame); err != nil {
		p.pendingAcks.Delete(reqID)
		return err
	}

	select {
	case err := <-ch:
		return err
	case <-ctx.Done():
		p.pendingAcks.Delete(reqID)
		return ctx.Err()
	case <-time.After(wsAckTimeout):
		p.pendingAcks.Delete(reqID)
		slog.Debug("wecom-ws: ack timeout, proceeding", "req_id", reqID)
		return nil
	}
}

// stripWeComAtMentions removes @bot mentions from message content.
func stripWeComAtMentions(content, botID string) string {
	if content == "" {
		return ""
	}
	botMention := "@" + botID
	content = strings.ReplaceAll(content, botMention, "")
	content = strings.ReplaceAll(content, "@所有人", "")
	return strings.TrimSpace(content)
}

// splitByBytes splits text by UTF-8 byte length (WeCom limit is ~2000 bytes).
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
