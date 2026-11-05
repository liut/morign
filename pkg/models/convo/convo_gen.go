// This file is generated - Do Not Edit.

package convo

import (
	"fmt"
	"time"

	comm "github.com/cupogo/andvari/models/comm"
	oid "github.com/cupogo/andvari/models/oid"
)

func init() {
	oid.RegistCate(SessionLabel, "cs")
}

// 状态
type SessionStatus int8

const (
	SessionStatusOpen   SessionStatus = 1 + iota //  1 开启
	SessionStatusClosed                          //  2 关闭
)

func (z *SessionStatus) Decode(s string) error {
	switch s {
	case "1", "open", "Open":
		*z = SessionStatusOpen
	case "2", "closed", "Closed":
		*z = SessionStatusClosed
	default:
		return fmt.Errorf("invalid sessionStatus: %q", s)
	}
	return nil
}
func (z *SessionStatus) UnmarshalText(b []byte) error {
	return z.Decode(string(b))
}
func (z SessionStatus) String() string {
	switch z {
	case SessionStatusOpen:
		return "open"
	case SessionStatusClosed:
		return "closed"
	default:
		return fmt.Sprintf("sessionStatus %d", int8(z))
	}
}
func (z SessionStatus) MarshalText() ([]byte, error) {
	return []byte(z.String()), nil
}

// consts of Session 会话
const (
	SessionTable = "convo_session"
	SessionAlias = "cs"
	SessionLabel = "session"
	SessionTypID = "convoSession"
)

// Session 会话
type Session struct {
	comm.BaseModel `bun:"table:convo_session,alias:cs" json:"-"`

	comm.DefaultModel

	SessionBasic

	comm.MetaField

	comm.OwnerField
} // @name convoSession

type SessionBasic struct {
	// 标题
	Title string `bson:"title" bun:",notnull" extensions:"x-order=A" form:"title" json:"title" pg:",notnull"`
	// 消息数
	MessageCount int `bson:"msgCount" bun:"msg_count,notnull,type:smallint" extensions:"x-order=B" form:"msgCount" json:"msgCount" pg:"msg_count,notnull,type:smallint"`
	// 状态
	//  * `open` - 开启
	//  * `closed` - 关闭
	Status SessionStatus `bson:"status" bun:",notnull,type:smallint" enums:"open,closed" extensions:"x-order=C" form:"status" json:"status" pg:",notnull,type:smallint" swaggertype:"string"`
	// 工具
	Tools []string `bson:"tools" bun:",notnull,default:'[]'" extensions:"x-order=D" json:"tools" pg:",notnull,default:'[]'"`
	// 频道
	Channel string `bson:"channel" bun:",notnull,type:varchar(23)" extensions:"x-order=E" form:"channel" json:"channel" pg:",notnull,type:varchar(23)"`
	// for meta update
	MetaDiff *comm.MetaDiff `bson:"-" bun:"-" json:"metaUp,omitempty" pg:"-" swaggerignore:"true"`
} // @name convoSessionBasic

type Sessions []Session

// Creating function call to it's inner fields defined hooks
func (z *Session) Creating() error {
	if z.IsZeroID() {
		id, ok := oid.NewWithCode(SessionLabel)
		if !ok {
			id = oid.NewID(oid.OtEvent)
		}
		z.SetID(id)
	}

	return z.DefaultModel.Creating()
}
func NewSessionWithBasic(in SessionBasic) *Session {
	obj := &Session{
		SessionBasic: in,
	}
	_ = obj.MetaUp(in.MetaDiff)
	return obj
}
func NewSessionWithID(id any) *Session {
	obj := new(Session)
	_ = obj.SetID(id)
	return obj
}
func (_ *Session) IdentityLabel() string { return SessionLabel }
func (_ *Session) IdentityModel() string { return SessionTypID }
func (_ *Session) IdentityTable() string { return SessionTable }
func (_ *Session) IdentityAlias() string { return SessionAlias }

type SessionSet struct {
	// 标题
	Title *string `extensions:"x-order=A" json:"title"`
	// 消息数
	MessageCount *int `extensions:"x-order=B" json:"msgCount"`
	// 状态
	//  * `open` - 开启
	//  * `closed` - 关闭
	Status *SessionStatus `enums:"open,closed" extensions:"x-order=C" json:"status" swaggertype:"string"`
	// 工具
	Tools *[]string `extensions:"x-order=D" json:"tools"`
	// 频道
	Channel *string `extensions:"x-order=E" json:"channel"`
	// for meta update
	MetaDiff *comm.MetaDiff `json:"metaUp,omitempty" swaggerignore:"true"`
	// 仅用于更新所有者(负责人)
	OwnerID *string `extensions:"x-order=F" json:"ownerID,omitempty"`
} // @name convoSessionSet

func (z *Session) SetWith(o SessionSet) {
	if o.Title != nil && z.Title != *o.Title {
		z.LogChangeValue("title", z.Title, o.Title)
		z.Title = *o.Title
	}
	if o.MessageCount != nil && z.MessageCount != *o.MessageCount {
		z.LogChangeValue("msg_count", z.MessageCount, o.MessageCount)
		z.MessageCount = *o.MessageCount
	}
	if o.Status != nil && z.Status != *o.Status {
		z.LogChangeValue("status", z.Status, o.Status)
		z.Status = *o.Status
	}
	if o.Tools != nil {
		z.LogChangeValue("tools", z.Tools, o.Tools)
		z.Tools = *o.Tools
	}
	if o.Channel != nil && z.Channel != *o.Channel {
		z.LogChangeValue("channel", z.Channel, o.Channel)
		z.Channel = *o.Channel
	}
	if o.MetaDiff != nil && z.MetaUp(o.MetaDiff) {
		z.SetChange("meta")
	}
	if o.OwnerID != nil {
		if id := oid.Cast(*o.OwnerID); z.OwnerID != id {
			z.LogChangeValue("owner_id", z.OwnerID, id)
			z.SetOwnerID(id)
		}
	}
}
func (in *SessionBasic) MetaAddKVs(args ...any) *SessionBasic {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}
func (in *SessionSet) MetaAddKVs(args ...any) *SessionSet {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}

// consts of Message 消息
const (
	MessageTable = "convo_message"
	MessageAlias = "cm"
	MessageLabel = "message"
	MessageTypID = "convoMessage"
)

// Message 消息
type Message struct {
	comm.BaseModel `bun:"table:convo_message,alias:cm" json:"-"`

	comm.DefaultModel

	MessageBasic

	comm.MetaField
} // @name convoMessage

type MessageBasic struct {
	// 会话编号
	SessionID oid.OID `binding:"required" bson:"session_id" bun:",notnull" extensions:"x-order=A" json:"session" pg:",notnull" swaggertype:"string"`
	// 角色
	Role string `binding:"required" bson:"role" bun:",notnull" extensions:"x-order=B" form:"role" json:"role" pg:",notnull"`
	// 内容
	Content string `binding:"required" bson:"content" bun:",notnull" extensions:"x-order=C" form:"content" json:"content" pg:",notnull"`
	// TokenCount
	TokenCount int `bson:"tokenCount" bun:",notnull,type:smallint" extensions:"x-order=D" form:"tokenCount" json:"tokenCount" pg:",notnull,type:smallint"`
	// 轮次编号
	TurnID oid.OID `bson:"turn_id" bun:",notnull,type:bigint" extensions:"x-order=E" json:"turnId" pg:",notnull,type:bigint" swaggertype:"string"`
	// 归属用户
	OwnerID oid.OID `bson:"owner_id" bun:"owner_id,notnull,type:bigint" extensions:"x-order=F" json:"ownerId" pg:"owner_id,notnull,type:bigint" swaggertype:"string"`
	// for meta update
	MetaDiff *comm.MetaDiff `bson:"-" bun:"-" json:"metaUp,omitempty" pg:"-" swaggerignore:"true"`
} // @name convoMessageBasic

type Messages []Message

// Creating function call to it's inner fields defined hooks
func (z *Message) Creating() error {
	if z.IsZeroID() {
		z.SetID(oid.NewID(oid.OtMessage))
	}

	return z.DefaultModel.Creating()
}
func NewMessageWithBasic(in MessageBasic) *Message {
	obj := &Message{
		MessageBasic: in,
	}
	_ = obj.MetaUp(in.MetaDiff)
	return obj
}
func NewMessageWithID(id any) *Message {
	obj := new(Message)
	_ = obj.SetID(id)
	return obj
}
func (_ *Message) IdentityLabel() string { return MessageLabel }
func (_ *Message) IdentityModel() string { return MessageTypID }
func (_ *Message) IdentityTable() string { return MessageTable }
func (_ *Message) IdentityAlias() string { return MessageAlias }

type MessageSet struct {
	// 会话编号
	SessionID *string `extensions:"x-order=A" json:"session"`
	// 角色
	Role *string `extensions:"x-order=B" json:"role"`
	// 内容
	Content *string `extensions:"x-order=C" json:"content"`
	// TokenCount
	TokenCount *int `extensions:"x-order=D" json:"tokenCount"`
	// 轮次编号
	TurnID *string `extensions:"x-order=E" json:"turnId"`
	// 归属用户
	OwnerID *string `extensions:"x-order=F" json:"ownerId"`
	// for meta update
	MetaDiff *comm.MetaDiff `json:"metaUp,omitempty" swaggerignore:"true"`
} // @name convoMessageSet

func (z *Message) SetWith(o MessageSet) {
	if o.SessionID != nil {
		if id := oid.Cast(*o.SessionID); z.SessionID != id {
			z.LogChangeValue("session_id", z.SessionID, id)
			z.SessionID = id
		}
	}
	if o.Role != nil && z.Role != *o.Role {
		z.LogChangeValue("role", z.Role, o.Role)
		z.Role = *o.Role
	}
	if o.Content != nil && z.Content != *o.Content {
		z.LogChangeValue("content", z.Content, o.Content)
		z.Content = *o.Content
	}
	if o.TokenCount != nil && z.TokenCount != *o.TokenCount {
		z.LogChangeValue("token_count", z.TokenCount, o.TokenCount)
		z.TokenCount = *o.TokenCount
	}
	if o.TurnID != nil {
		if id := oid.Cast(*o.TurnID); z.TurnID != id {
			z.LogChangeValue("turn_id", z.TurnID, id)
			z.TurnID = id
		}
	}
	if o.OwnerID != nil {
		if id := oid.Cast(*o.OwnerID); z.OwnerID != id {
			z.LogChangeValue("owner_id", z.OwnerID, id)
			z.OwnerID = id
		}
	}
	if o.MetaDiff != nil && z.MetaUp(o.MetaDiff) {
		z.SetChange("meta")
	}
}
func (in *MessageBasic) MetaAddKVs(args ...any) *MessageBasic {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}
func (in *MessageSet) MetaAddKVs(args ...any) *MessageSet {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}

// consts of UsageRecord 使用情况
const (
	UsageRecordTable = "convo_usage_record"
	UsageRecordAlias = "ur"
	UsageRecordLabel = "usageRecord"
	UsageRecordTypID = "convoUsageRecord"
)

// UsageRecord 使用情况 每次请求后记录
type UsageRecord struct {
	comm.BaseModel `bun:"table:convo_usage_record,alias:ur" json:"-"`

	comm.DefaultModel

	UsageRecordBasic

	comm.MetaField
} // @name convoUsageRecord

type UsageRecordBasic struct {
	// 会话编号
	SessionID oid.OID `binding:"required" bson:"session_id" bun:",notnull" extensions:"x-order=A" json:"session" pg:",notnull" swaggertype:"string"`
	// 消息数
	MsgCount int `bson:"msgCount" bun:",notnull,type:smallint" extensions:"x-order=B" form:"msgCount" json:"msgCount" pg:",notnull,type:smallint"`
	// 输入Token数
	InputTokens int `bson:"inputTokens" bun:",notnull,type:int" extensions:"x-order=C" form:"inputTokens" json:"inputTokens" pg:",notnull,type:int"`
	// 输出Token数
	OutputTokens int `bson:"outputTokens" bun:",notnull,type:int" extensions:"x-order=D" form:"outputTokens" json:"outputTokens" pg:",notnull,type:int"`
	// 总Token数
	TotalTokens int `bson:"totalTokens" bun:",notnull,type:int" extensions:"x-order=E" form:"totalTokens" json:"totalTokens" pg:",notnull,type:int"`
	// 模型
	Model string `bson:"model" bun:",notnull,type:name" extensions:"x-order=F" form:"model" json:"model" pg:",notnull,type:name"`
	// for meta update
	MetaDiff *comm.MetaDiff `bson:"-" bun:"-" json:"metaUp,omitempty" pg:"-" swaggerignore:"true"`
} // @name convoUsageRecordBasic

type UsageRecords []UsageRecord

// Creating function call to it's inner fields defined hooks
func (z *UsageRecord) Creating() error {
	if z.IsZeroID() {
		z.SetID(oid.NewID(oid.OtEvent))
	}

	return z.DefaultModel.Creating()
}
func NewUsageRecordWithBasic(in UsageRecordBasic) *UsageRecord {
	obj := &UsageRecord{
		UsageRecordBasic: in,
	}
	_ = obj.MetaUp(in.MetaDiff)
	return obj
}
func NewUsageRecordWithID(id any) *UsageRecord {
	obj := new(UsageRecord)
	_ = obj.SetID(id)
	return obj
}
func (_ *UsageRecord) IdentityLabel() string { return UsageRecordLabel }
func (_ *UsageRecord) IdentityModel() string { return UsageRecordTypID }
func (_ *UsageRecord) IdentityTable() string { return UsageRecordTable }
func (_ *UsageRecord) IdentityAlias() string { return UsageRecordAlias }

type UsageRecordSet struct {
	// 消息数
	MsgCount *int `extensions:"x-order=A" json:"msgCount"`
	// for meta update
	MetaDiff *comm.MetaDiff `json:"metaUp,omitempty" swaggerignore:"true"`
} // @name convoUsageRecordSet

func (z *UsageRecord) SetWith(o UsageRecordSet) {
	if o.MsgCount != nil && z.MsgCount != *o.MsgCount {
		z.LogChangeValue("msg_count", z.MsgCount, o.MsgCount)
		z.MsgCount = *o.MsgCount
	}
	if o.MetaDiff != nil && z.MetaUp(o.MetaDiff) {
		z.SetChange("meta")
	}
}
func (in *UsageRecordBasic) MetaAddKVs(args ...any) *UsageRecordBasic {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}
func (in *UsageRecordSet) MetaAddKVs(args ...any) *UsageRecordSet {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}

// consts of ChatTurn 聊天轮次
const (
	ChatTurnTable = "convo_chat_turn"
	ChatTurnAlias = "ct"
	ChatTurnLabel = "chatTurn"
	ChatTurnTypID = "convoChatTurn"
)

// ChatTurn 聊天轮次
type ChatTurn struct {
	comm.BaseModel `bun:"table:convo_chat_turn,alias:ct" json:"-"`

	comm.DefaultModel

	ChatTurnBasic

	comm.MetaField
} // @name convoChatTurn

type ChatTurnBasic struct {
	// 会话编号
	SessionID oid.OID `bson:"session_id" bun:",notnull" extensions:"x-order=A" json:"session" pg:",notnull" swaggertype:"string"`
	// 幂等键
	IdempotencyKey string `bson:"idempotencyKey" bun:",notnull,type:varchar(255),unique:uk_turn_idem_key" extensions:"x-order=B" form:"idempotencyKey" json:"idempotencyKey" pg:",notnull,type:varchar(255),unique:uk_turn_idem_key"`
	// 来源 web/channel
	Source string `bson:"source" bun:",notnull,type:varchar(32)" extensions:"x-order=C" form:"source" json:"source" pg:",notnull,type:varchar(32)"`
	// 状态 queued/running/succeeded/failed/cancelled/interrupted
	Status string `bson:"status" bun:",notnull,type:varchar(16)" extensions:"x-order=D" form:"status" json:"status" pg:",notnull,type:varchar(16)"`
	// 请求摘要
	RequestDigest string `bson:"requestDigest" bun:",notnull,type:varchar(64)" extensions:"x-order=E" form:"requestDigest" json:"requestDigest" pg:",notnull,type:varchar(64)"`
	// 归属用户
	OwnerID oid.OID `bson:"owner_id" bun:"owner_id,notnull,type:bigint" extensions:"x-order=F" json:"ownerId" pg:"owner_id,notnull,type:bigint" swaggertype:"string"`
	// 最终结果
	Result string `bson:"result" bun:",notnull,type:text" extensions:"x-order=G" form:"result" json:"result" pg:",notnull,type:text"`
	// for meta update
	MetaDiff *comm.MetaDiff `bson:"-" bun:"-" json:"metaUp,omitempty" pg:"-" swaggerignore:"true"`
} // @name convoChatTurnBasic

type ChatTurns []ChatTurn

// Creating function call to it's inner fields defined hooks
func (z *ChatTurn) Creating() error {
	if z.IsZeroID() {
		z.SetID(oid.NewID(oid.OtEvent))
	}

	return z.DefaultModel.Creating()
}
func NewChatTurnWithBasic(in ChatTurnBasic) *ChatTurn {
	obj := &ChatTurn{
		ChatTurnBasic: in,
	}
	_ = obj.MetaUp(in.MetaDiff)
	return obj
}
func NewChatTurnWithID(id any) *ChatTurn {
	obj := new(ChatTurn)
	_ = obj.SetID(id)
	return obj
}
func (_ *ChatTurn) IdentityLabel() string { return ChatTurnLabel }
func (_ *ChatTurn) IdentityModel() string { return ChatTurnTypID }
func (_ *ChatTurn) IdentityTable() string { return ChatTurnTable }
func (_ *ChatTurn) IdentityAlias() string { return ChatTurnAlias }

type ChatTurnSet struct {
	// 会话编号
	SessionID *string `extensions:"x-order=A" json:"session"`
	// 幂等键
	IdempotencyKey *string `extensions:"x-order=B" json:"idempotencyKey"`
	// 来源 web/channel
	Source *string `extensions:"x-order=C" json:"source"`
	// 状态 queued/running/succeeded/failed/cancelled/interrupted
	Status *string `extensions:"x-order=D" json:"status"`
	// 请求摘要
	RequestDigest *string `extensions:"x-order=E" json:"requestDigest"`
	// 归属用户
	OwnerID *string `extensions:"x-order=F" json:"ownerId"`
	// 最终结果
	Result *string `extensions:"x-order=G" json:"result"`
	// for meta update
	MetaDiff *comm.MetaDiff `json:"metaUp,omitempty" swaggerignore:"true"`
} // @name convoChatTurnSet

func (z *ChatTurn) SetWith(o ChatTurnSet) {
	if o.SessionID != nil {
		if id := oid.Cast(*o.SessionID); z.SessionID != id {
			z.LogChangeValue("session_id", z.SessionID, id)
			z.SessionID = id
		}
	}
	if o.IdempotencyKey != nil && z.IdempotencyKey != *o.IdempotencyKey {
		z.LogChangeValue("idempotency_key", z.IdempotencyKey, o.IdempotencyKey)
		z.IdempotencyKey = *o.IdempotencyKey
	}
	if o.Source != nil && z.Source != *o.Source {
		z.LogChangeValue("source", z.Source, o.Source)
		z.Source = *o.Source
	}
	if o.Status != nil && z.Status != *o.Status {
		z.LogChangeValue("status", z.Status, o.Status)
		z.Status = *o.Status
	}
	if o.RequestDigest != nil && z.RequestDigest != *o.RequestDigest {
		z.LogChangeValue("request_digest", z.RequestDigest, o.RequestDigest)
		z.RequestDigest = *o.RequestDigest
	}
	if o.OwnerID != nil {
		if id := oid.Cast(*o.OwnerID); z.OwnerID != id {
			z.LogChangeValue("owner_id", z.OwnerID, id)
			z.OwnerID = id
		}
	}
	if o.Result != nil && z.Result != *o.Result {
		z.LogChangeValue("result", z.Result, o.Result)
		z.Result = *o.Result
	}
	if o.MetaDiff != nil && z.MetaUp(o.MetaDiff) {
		z.SetChange("meta")
	}
}
func (in *ChatTurnBasic) MetaAddKVs(args ...any) *ChatTurnBasic {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}
func (in *ChatTurnSet) MetaAddKVs(args ...any) *ChatTurnSet {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}

// consts of ToolRecord 工具调用记录
const (
	ToolRecordTable = "convo_tool_record"
	ToolRecordAlias = "tr"
	ToolRecordLabel = "toolRecord"
	ToolRecordTypID = "convoToolRecord"
)

// ToolRecord 工具调用记录
type ToolRecord struct {
	comm.BaseModel `bun:"table:convo_tool_record,alias:tr" json:"-"`

	comm.DefaultModel

	ToolRecordBasic

	comm.MetaField
} // @name convoToolRecord

type ToolRecordBasic struct {
	// 轮次编号
	TurnID oid.OID `bson:"turn_id" bun:",notnull,type:bigint" extensions:"x-order=A" json:"turnId" pg:",notnull,type:bigint" swaggertype:"string"`
	// 会话编号
	SessionID oid.OID `bson:"session_id" bun:",notnull,type:bigint" extensions:"x-order=B" json:"session" pg:",notnull,type:bigint" swaggertype:"string"`
	// 归属用户
	OwnerID oid.OID `bson:"owner_id" bun:"owner_id,notnull,type:bigint" extensions:"x-order=C" json:"ownerId" pg:"owner_id,notnull,type:bigint" swaggertype:"string"`
	// 轮内顺序
	Seq int `bson:"seq" bun:",notnull,type:int" extensions:"x-order=D" form:"seq" json:"seq" pg:",notnull,type:int"`
	// 工具调用编号
	CallID string `bson:"callId" bun:",notnull,type:varchar(64)" extensions:"x-order=E" form:"callId" json:"callId" pg:",notnull,type:varchar(64)"`
	// 工具名
	ToolName string `bson:"toolName" bun:",notnull,type:varchar(128)" extensions:"x-order=F" form:"toolName" json:"toolName" pg:",notnull,type:varchar(128)"`
	// 入参原文
	Arguments string `bson:"arguments" bun:",notnull,type:text" extensions:"x-order=G" form:"arguments" json:"arguments" pg:",notnull,type:text"`
	// 结果状态
	Status string `bson:"status" bun:",notnull,type:varchar(16)" extensions:"x-order=H" form:"status" json:"status" pg:",notnull,type:varchar(16)"`
	// 结果全文
	Content string `bson:"content" bun:",notnull,type:text" extensions:"x-order=I" form:"content" json:"content" pg:",notnull,type:text"`
	// 发给模型的预览原文
	Preview string `bson:"preview" bun:",notnull,type:text" extensions:"x-order=J" form:"preview" json:"preview" pg:",notnull,type:text"`
	// 是否走 handle 外溢
	Spilled bool `bson:"spilled" bun:",notnull" extensions:"x-order=K" form:"spilled" json:"spilled" pg:",notnull"`
	// 错误码
	ErrorCode string `bson:"errorCode" bun:",notnull,type:varchar(64)" extensions:"x-order=L" form:"errorCode" json:"errorCode" pg:",notnull,type:varchar(64)"`
	// 耗时毫秒
	DurationMS int `bson:"durationMs" bun:",notnull,type:int" extensions:"x-order=M" form:"durationMs" json:"durationMs" pg:",notnull,type:int"`
	// for meta update
	MetaDiff *comm.MetaDiff `bson:"-" bun:"-" json:"metaUp,omitempty" pg:"-" swaggerignore:"true"`
} // @name convoToolRecordBasic

type ToolRecords []ToolRecord

// Creating function call to it's inner fields defined hooks
func (z *ToolRecord) Creating() error {
	if z.IsZeroID() {
		z.SetID(oid.NewID(oid.OtEvent))
	}

	return z.DefaultModel.Creating()
}
func NewToolRecordWithBasic(in ToolRecordBasic) *ToolRecord {
	obj := &ToolRecord{
		ToolRecordBasic: in,
	}
	_ = obj.MetaUp(in.MetaDiff)
	return obj
}
func NewToolRecordWithID(id any) *ToolRecord {
	obj := new(ToolRecord)
	_ = obj.SetID(id)
	return obj
}
func (_ *ToolRecord) IdentityLabel() string { return ToolRecordLabel }
func (_ *ToolRecord) IdentityModel() string { return ToolRecordTypID }
func (_ *ToolRecord) IdentityTable() string { return ToolRecordTable }
func (_ *ToolRecord) IdentityAlias() string { return ToolRecordAlias }

type ToolRecordSet struct {
	// 轮次编号
	TurnID *string `extensions:"x-order=A" json:"turnId"`
	// 会话编号
	SessionID *string `extensions:"x-order=B" json:"session"`
	// 归属用户
	OwnerID *string `extensions:"x-order=C" json:"ownerId"`
	// 轮内顺序
	Seq *int `extensions:"x-order=D" json:"seq"`
	// 工具调用编号
	CallID *string `extensions:"x-order=E" json:"callId"`
	// 工具名
	ToolName *string `extensions:"x-order=F" json:"toolName"`
	// 入参原文
	Arguments *string `extensions:"x-order=G" json:"arguments"`
	// 结果状态
	Status *string `extensions:"x-order=H" json:"status"`
	// 结果全文
	Content *string `extensions:"x-order=I" json:"content"`
	// 发给模型的预览原文
	Preview *string `extensions:"x-order=J" json:"preview"`
	// 是否走 handle 外溢
	Spilled *bool `extensions:"x-order=K" json:"spilled"`
	// 错误码
	ErrorCode *string `extensions:"x-order=L" json:"errorCode"`
	// 耗时毫秒
	DurationMS *int `extensions:"x-order=M" json:"durationMs"`
	// for meta update
	MetaDiff *comm.MetaDiff `json:"metaUp,omitempty" swaggerignore:"true"`
} // @name convoToolRecordSet

func (z *ToolRecord) SetWith(o ToolRecordSet) {
	if o.TurnID != nil {
		if id := oid.Cast(*o.TurnID); z.TurnID != id {
			z.LogChangeValue("turn_id", z.TurnID, id)
			z.TurnID = id
		}
	}
	if o.SessionID != nil {
		if id := oid.Cast(*o.SessionID); z.SessionID != id {
			z.LogChangeValue("session_id", z.SessionID, id)
			z.SessionID = id
		}
	}
	if o.OwnerID != nil {
		if id := oid.Cast(*o.OwnerID); z.OwnerID != id {
			z.LogChangeValue("owner_id", z.OwnerID, id)
			z.OwnerID = id
		}
	}
	if o.Seq != nil && z.Seq != *o.Seq {
		z.LogChangeValue("seq", z.Seq, o.Seq)
		z.Seq = *o.Seq
	}
	if o.CallID != nil && z.CallID != *o.CallID {
		z.LogChangeValue("call_id", z.CallID, o.CallID)
		z.CallID = *o.CallID
	}
	if o.ToolName != nil && z.ToolName != *o.ToolName {
		z.LogChangeValue("tool_name", z.ToolName, o.ToolName)
		z.ToolName = *o.ToolName
	}
	if o.Arguments != nil && z.Arguments != *o.Arguments {
		z.LogChangeValue("arguments", z.Arguments, o.Arguments)
		z.Arguments = *o.Arguments
	}
	if o.Status != nil && z.Status != *o.Status {
		z.LogChangeValue("status", z.Status, o.Status)
		z.Status = *o.Status
	}
	if o.Content != nil && z.Content != *o.Content {
		z.LogChangeValue("content", z.Content, o.Content)
		z.Content = *o.Content
	}
	if o.Preview != nil && z.Preview != *o.Preview {
		z.LogChangeValue("preview", z.Preview, o.Preview)
		z.Preview = *o.Preview
	}
	if o.Spilled != nil && z.Spilled != *o.Spilled {
		z.LogChangeValue("spilled", z.Spilled, o.Spilled)
		z.Spilled = *o.Spilled
	}
	if o.ErrorCode != nil && z.ErrorCode != *o.ErrorCode {
		z.LogChangeValue("error_code", z.ErrorCode, o.ErrorCode)
		z.ErrorCode = *o.ErrorCode
	}
	if o.DurationMS != nil && z.DurationMS != *o.DurationMS {
		z.LogChangeValue("duration_ms", z.DurationMS, o.DurationMS)
		z.DurationMS = *o.DurationMS
	}
	if o.MetaDiff != nil && z.MetaUp(o.MetaDiff) {
		z.SetChange("meta")
	}
}
func (in *ToolRecordBasic) MetaAddKVs(args ...any) *ToolRecordBasic {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}
func (in *ToolRecordSet) MetaAddKVs(args ...any) *ToolRecordSet {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}

// consts of User 用户
const (
	UserTable = "convo_user"
	UserAlias = "u"
	UserLabel = "user"
	UserTypID = "convoUser"
)

// User 用户 来自 OAuth SP 的拷贝
type User struct {
	comm.BaseModel `bun:"table:convo_user,alias:u" json:"-"`

	comm.DefaultModel

	UserBasic

	comm.MetaField
} // @name convoUser

type UserBasic struct {
	// 登录名 唯一
	Username string `bun:"username,notnull,type:name,unique" extensions:"x-order=A" form:"username" json:"username" pg:"username,notnull,type:name,unique"`
	// 昵称
	Nickname string `bun:"nickname,notnull,type:varchar(45)" extensions:"x-order=B" form:"nickname" json:"nickname" pg:"nickname,notnull,type:varchar(45)"`
	// 头像路径
	AvatarPath string `bun:"avatar,notnull,type:varchar(125)" extensions:"x-order=C" form:"avatar" json:"avatar,omitempty" pg:"avatar,notnull,type:varchar(125)"`
	// 邮箱
	Email string `bun:"email,notnull,type:varchar(43)" extensions:"x-order=D" form:"email" json:"email,omitempty" pg:"email,notnull,use_zero,type:varchar(43)"`
	// 电话
	Phone string `bun:"phone,notnull,type:varchar(15)" extensions:"x-order=E" form:"phone" json:"phone,omitempty" pg:"phone,notnull,use_zero,type:varchar(15)"`
	// for meta update
	MetaDiff *comm.MetaDiff `bson:"-" bun:"-" json:"metaUp,omitempty" pg:"-" swaggerignore:"true"`
} // @name convoUserBasic

type Users []User

// Creating function call to it's inner fields defined hooks
func (z *User) Creating() error {
	if z.IsZeroID() {
		z.SetID(oid.NewID(oid.OtAccount))
	}

	return z.DefaultModel.Creating()
}
func NewUserWithBasic(in UserBasic) *User {
	obj := &User{
		UserBasic: in,
	}
	_ = obj.MetaUp(in.MetaDiff)
	return obj
}
func NewUserWithID(id any) *User {
	obj := new(User)
	_ = obj.SetID(id)
	return obj
}
func (_ *User) IdentityLabel() string { return UserLabel }
func (_ *User) IdentityModel() string { return UserTypID }
func (_ *User) IdentityTable() string { return UserTable }
func (_ *User) IdentityAlias() string { return UserAlias }

type UserSet struct {
	// 登录名 唯一
	Username *string `extensions:"x-order=A" json:"username"`
	// 昵称
	Nickname *string `extensions:"x-order=B" json:"nickname"`
	// 头像路径
	AvatarPath *string `extensions:"x-order=C" form:"avatar" json:"avatar,omitempty"`
	// 邮箱
	Email *string `extensions:"x-order=D" form:"email" json:"email,omitempty"`
	// 电话
	Phone *string `extensions:"x-order=E" form:"phone" json:"phone,omitempty"`
	// for meta update
	MetaDiff *comm.MetaDiff `json:"metaUp,omitempty" swaggerignore:"true"`
} // @name convoUserSet

func (z *User) SetWith(o UserSet) {
	if o.Username != nil && z.Username != *o.Username {
		z.LogChangeValue("username", z.Username, o.Username)
		z.Username = *o.Username
	}
	if o.Nickname != nil && z.Nickname != *o.Nickname {
		z.LogChangeValue("nickname", z.Nickname, o.Nickname)
		z.Nickname = *o.Nickname
	}
	if o.AvatarPath != nil && z.AvatarPath != *o.AvatarPath {
		z.LogChangeValue("avatar", z.AvatarPath, o.AvatarPath)
		z.AvatarPath = *o.AvatarPath
	}
	if o.Email != nil && z.Email != *o.Email {
		z.LogChangeValue("email", z.Email, o.Email)
		z.Email = *o.Email
	}
	if o.Phone != nil && z.Phone != *o.Phone {
		z.LogChangeValue("phone", z.Phone, o.Phone)
		z.Phone = *o.Phone
	}
	if o.MetaDiff != nil && z.MetaUp(o.MetaDiff) {
		z.SetChange("meta")
	}
}
func (in *UserBasic) MetaAddKVs(args ...any) *UserBasic {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}
func (in *UserSet) MetaAddKVs(args ...any) *UserSet {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}

// consts of ThirdUser 第三方用户
const (
	ThirdUserTable = "convo_third_user"
	ThirdUserAlias = "tu"
	ThirdUserLabel = "thirdUser"
	ThirdUserTypID = "convoThirdUser"
)

// ThirdUser 第三方用户 来自不同平台 如微信和飞书等 PK为第三方平台标识+账号
type ThirdUser struct {
	comm.BaseModel `bun:"table:convo_third_user,alias:tu" json:"-"`

	comm.DunceModel

	ThirdUserBasic

	comm.MetaField
} // @name convoThirdUser

type ThirdUserBasic struct {
	// 所有人编号
	OwnerID oid.OID `bun:"owner_id,notnull,type:bigint" extensions:"x-order=A" json:"ownerID" pg:"owner_id,notnull,type:bigint" swaggertype:"string"`
	// for meta update
	MetaDiff *comm.MetaDiff `bson:"-" bun:"-" json:"metaUp,omitempty" pg:"-" swaggerignore:"true"`
} // @name convoThirdUserBasic

type ThirdUsers []ThirdUser

// Creating function call to it's inner fields defined hooks
func (z *ThirdUser) Creating() error {
	if z.IsZeroID() {
		return comm.ErrEmptyID
	}

	return z.DunceModel.Creating()
}
func NewThirdUserWithBasic(in ThirdUserBasic) *ThirdUser {
	obj := &ThirdUser{
		ThirdUserBasic: in,
	}
	_ = obj.MetaUp(in.MetaDiff)
	return obj
}
func NewThirdUserWithID(id any) *ThirdUser {
	obj := new(ThirdUser)
	_ = obj.SetID(id)
	return obj
}
func (_ *ThirdUser) IdentityLabel() string { return ThirdUserLabel }
func (_ *ThirdUser) IdentityModel() string { return ThirdUserTypID }
func (_ *ThirdUser) IdentityTable() string { return ThirdUserTable }
func (_ *ThirdUser) IdentityAlias() string { return ThirdUserAlias }

type ThirdUserSet struct {
	// for meta update
	MetaDiff *comm.MetaDiff `json:"metaUp,omitempty" swaggerignore:"true"`
} // @name convoThirdUserSet

func (z *ThirdUser) SetWith(o ThirdUserSet) {
	if o.MetaDiff != nil && z.MetaUp(o.MetaDiff) {
		z.SetChange("meta")
	}
}
func (in *ThirdUserBasic) MetaAddKVs(args ...any) *ThirdUserBasic {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}
func (in *ThirdUserSet) MetaAddKVs(args ...any) *ThirdUserSet {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}

// consts of Memory 记忆
const (
	MemoryTable = "convo_memory"
	MemoryAlias = "m"
	MemoryLabel = "memory"
	MemoryTypID = "convoMemory"
)

// Memory 记忆
type Memory struct {
	comm.BaseModel `bun:"table:convo_memory,alias:m" json:"-"`

	comm.DefaultModel

	MemoryBasic

	comm.MetaField
} // @name convoMemory

type MemoryBasic struct {
	// 所有人编号
	OwnerID oid.OID `bun:"owner_id,notnull,type:bigint,unique:mm_key_uid_key" extensions:"x-order=A" json:"ownerID" pg:"owner_id,notnull,type:bigint,unique:mm_key_uid_key" swaggertype:"string"`
	// 关键点
	Key string `bun:",notnull,type:text,unique:mm_key_uid_key" extensions:"x-order=B" form:"key" json:"key" pg:",notnull,type:text,unique:mm_key_uid_key"`
	// 分类
	Cate string `bun:",notnull,type:text" extensions:"x-order=C" form:"cate" json:"cate" pg:",notnull,type:text"`
	// 内容
	Content string `bun:",notnull,type:text" extensions:"x-order=D" form:"content" json:"content" pg:",notnull,type:text"`
	// 分层  (working/short-term/long-term)
	Tier string `bun:",notnull,type:text,default:'working'" extensions:"x-order=E" form:"tier" json:"tier" pg:",notnull,type:text,default:'working'"`
	// 重要性评分  (0-1)
	ImportanceScore float64 `bun:"importance_score,notnull,type:float,default:0.5" extensions:"x-order=F" json:"importanceScore" pg:"importance_score,notnull,type:float,default:0.5"`
	// 最近访问时间
	LastAccessedAt time.Time `bun:"last_accessed_at,notnull,type:timestamptz,default:now()" extensions:"x-order=G" json:"lastAccessedAt" pg:"last_accessed_at,notnull,type:timestamptz,default:now()"`
	// 访问计数
	AccessCount int `bun:"access_count,notnull,type:int,default:0" extensions:"x-order=H" form:"accessCount" json:"accessCount" pg:"access_count,notnull,type:int,default:0"`
	// 衰减率
	DecayRate float64 `bun:"decay_rate,notnull,type:float,default:1.0" extensions:"x-order=I" json:"decayRate" pg:"decay_rate,notnull,type:float,default:1.0"`
	// for meta update
	MetaDiff *comm.MetaDiff `bson:"-" bun:"-" json:"metaUp,omitempty" pg:"-" swaggerignore:"true"`
} // @name convoMemoryBasic

type Memories []Memory

// Creating function call to it's inner fields defined hooks
func (z *Memory) Creating() error {
	if z.IsZeroID() {
		z.SetID(oid.NewID(oid.OtEvent))
	}

	return z.DefaultModel.Creating()
}
func NewMemoryWithBasic(in MemoryBasic) *Memory {
	obj := &Memory{
		MemoryBasic: in,
	}
	_ = obj.MetaUp(in.MetaDiff)
	return obj
}
func NewMemoryWithID(id any) *Memory {
	obj := new(Memory)
	_ = obj.SetID(id)
	return obj
}
func (_ *Memory) IdentityLabel() string { return MemoryLabel }
func (_ *Memory) IdentityModel() string { return MemoryTypID }
func (_ *Memory) IdentityTable() string { return MemoryTable }
func (_ *Memory) IdentityAlias() string { return MemoryAlias }

type MemorySet struct {
	// 分类
	Cate *string `extensions:"x-order=A" json:"cate"`
	// 内容
	Content *string `extensions:"x-order=B" json:"content"`
	// 分层  (working/short-term/long-term)
	Tier *string `extensions:"x-order=C" json:"tier"`
	// 重要性评分  (0-1)
	ImportanceScore *float64 `extensions:"x-order=D" json:"importanceScore"`
	// 最近访问时间
	LastAccessedAt *time.Time `extensions:"x-order=E" json:"lastAccessedAt"`
	// 访问计数
	AccessCount *int `extensions:"x-order=F" json:"accessCount"`
	// 衰减率
	DecayRate *float64 `extensions:"x-order=G" json:"decayRate"`
	// for meta update
	MetaDiff *comm.MetaDiff `json:"metaUp,omitempty" swaggerignore:"true"`
} // @name convoMemorySet

func (z *Memory) SetWith(o MemorySet) {
	if o.Cate != nil && z.Cate != *o.Cate {
		z.LogChangeValue("cate", z.Cate, o.Cate)
		z.Cate = *o.Cate
	}
	if o.Content != nil && z.Content != *o.Content {
		z.LogChangeValue("content", z.Content, o.Content)
		z.Content = *o.Content
	}
	if o.Tier != nil && z.Tier != *o.Tier {
		z.LogChangeValue("tier", z.Tier, o.Tier)
		z.Tier = *o.Tier
	}
	if o.ImportanceScore != nil {
		z.LogChangeValue("importance_score", z.ImportanceScore, o.ImportanceScore)
		z.ImportanceScore = *o.ImportanceScore
	}
	if o.LastAccessedAt != nil {
		z.LogChangeValue("last_accessed_at", z.LastAccessedAt, o.LastAccessedAt)
		z.LastAccessedAt = *o.LastAccessedAt
	}
	if o.AccessCount != nil && z.AccessCount != *o.AccessCount {
		z.LogChangeValue("access_count", z.AccessCount, o.AccessCount)
		z.AccessCount = *o.AccessCount
	}
	if o.DecayRate != nil {
		z.LogChangeValue("decay_rate", z.DecayRate, o.DecayRate)
		z.DecayRate = *o.DecayRate
	}
	if o.MetaDiff != nil && z.MetaUp(o.MetaDiff) {
		z.SetChange("meta")
	}
}
func (in *MemoryBasic) MetaAddKVs(args ...any) *MemoryBasic {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}
func (in *MemorySet) MetaAddKVs(args ...any) *MemorySet {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}
