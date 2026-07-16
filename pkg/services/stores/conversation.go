package stores

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/cupogo/andvari/models/oid"
	"gopkg.in/yaml.v3"

	"github.com/liut/morign/pkg/models/aigc"
	"github.com/liut/morign/pkg/models/convo"
	"github.com/liut/morign/pkg/settings"
)

const (
	historyLifetimeS = time.Second * 86400
	historyMaxLength = 25
)

type Conversation interface {
	GetID() string
	GetOID() oid.OID
	GetChannel() string
	SetTools(names ...string)
	Save(ctx context.Context) error
	CountHistory(ctx context.Context) int
	AddHistory(ctx context.Context, item *aigc.HistoryItem) error
	ListHistory(ctx context.Context) (aigc.HistoryItems, error)
	ClearHistory(ctx context.Context) error
}

// NewConversation 创建会话，使用默认 Redis 客户端
func NewConversation(ctx context.Context, id any) Conversation {
	return newConversation(ctx, id, SgtRC())
}

const sessionKeyCSIDPrefix = "platform:csid:"

// GetOrCreateConversationBySessionKey 根据渠道 sessionKey 查找或创建 Conversation
// sessionKey 格式: "{channel}:{chatID}:{userID}" 或 "{channel}:{userID}"
// 查找 Redis 映射获取已绑定的 Conversation OID，若无则创建新 Conversation 并写入映射
func GetOrCreateConversationBySessionKey(ctx context.Context, sessionKey string) Conversation {
	key := sessionKeyCSIDPrefix + sessionKey
	oidStr, _ := SgtRC().Get(ctx, key).Result()

	cs := newConversation(ctx, oidStr, SgtRC())
	if oidStr == "" {
		SgtRC().Set(ctx, key, cs.GetID(), 30*24*time.Hour)
	} else {
		SgtRC().Expire(ctx, key, 30*24*time.Hour)
	}

	// 从 sessionKey 提取 channel 和 chatID
	parts := strings.SplitN(sessionKey, ":", 3)
	if len(parts) >= 2 {
		cs.sess.SetWith(convo.SessionSet{Channel: &parts[0]})
		if len(parts) >= 3 {
			cs.sess.MetaSet("chatID", parts[1])
		}
	}
	return cs
}

// ResetSessionBySessionKey 删除 sessionKey -> csid 的映射，强制重建会话
func ResetSessionBySessionKey(ctx context.Context, sessionKey string) error {
	key := sessionKeyCSIDPrefix + sessionKey
	return SgtRC().Del(ctx, key).Err()
}

// newConversation is internal constructor, supports injecting Redis client (for testing)
func newConversation(ctx context.Context, id any, rc RedisClient) *conversation {
	sto := Sgt()
	cid := oid.Cast(id)
	var sess *convo.Session
	var err error
	if cid.Valid() {
		sess, err = sto.Convo().GetSession(ctx, cid.String())
		if err != nil {
			sess = convo.NewSessionWithID(cid)
		}
	} else {
		sess = new(convo.Session)
		sess.Creating() //nolint
	}

	return &conversation{
		id:   sess.ID,
		rc:   rc,
		sess: sess,
		sto:  sto,
	}
}

// conversation is the conversation implementation using Redis for history storage
type conversation struct {
	id oid.OID
	rc RedisClient

	sess *convo.Session
	sto  Storage
}

// GetID returns the conversation ID
func (s *conversation) GetID() string {
	return s.id.String()
}

// GetOID returns the conversation OID
func (s *conversation) GetOID() oid.OID {
	return s.id
}

// GetChannel returns the channel for the conversation
func (s *conversation) GetChannel() string {
	return s.sess.Channel
}

// SetTools sets the tool list for the conversation
func (s *conversation) SetTools(names ...string) {
	if len(names) > 0 {
		s.sess.Tools = names
	}
}

// Save saves the conversation to the database
func (s *conversation) Save(ctx context.Context) error {
	count := s.CountHistory(ctx)
	s.sess.MessageCount = count
	return s.sto.Convo().SaveSession(ctx, s.sess)
}

// CountHistory returns the number of history records
func (s *conversation) CountHistory(ctx context.Context) int {
	key := s.getKey()
	n, err := s.rc.LLen(ctx, key).Result()
	if err != nil {
		logger().Infow("llen fail", "key", key, "err", err)
		return 0
	}
	return int(n)
}

// TODO: AddMessages()

// AddHistory adds a history record
func (s *conversation) AddHistory(ctx context.Context, item *aigc.HistoryItem) error {
	key := s.getKey()

	// 检查最后一条历史，如果 User 相同，则删除旧记录（保留最新答案）
	lastMsg, err := s.getLastUserMessage(ctx)
	if err == nil && lastMsg != nil && lastMsg.ChatItem != nil && item.ChatItem != nil {
		if lastMsg.ChatItem.User == item.ChatItem.User {
			logger().Debugw("replace last history with same user", "key", key, "user", item.ChatItem.User)
			// 删除最后一条
			if err := s.rc.RPop(ctx, key).Err(); err != nil {
				logger().Infow("rpop last history fail", "key", key, "err", err)
			}
		}
	}

	b, err := item.MarshalBinary()
	if err != nil {
		return err
	}

	res := s.rc.RPush(ctx, key, b)
	err = res.Err()
	if err == nil {
		count, _ := res.Result()
		if err = s.rc.Expire(ctx, key, historyLifetimeS).Err(); err != nil {
			return err
		}
		if count > historyMaxLength {
			logger().Infow("history length overflow", "count", count)
			err = s.rc.LPop(ctx, key).Err()
		}
	}
	if err != nil {
		logger().Infow("add history fail", "key", key, "err", err)
		return err
	}

	return nil
}

// getLastUserMessage gets the last user message from the list
func (s *conversation) getLastUserMessage(ctx context.Context) (*aigc.HistoryItem, error) {
	key := s.getKey()
	b, err := s.rc.LIndex(ctx, key, -1).Bytes()
	if err != nil {
		return nil, err
	}
	var item aigc.HistoryItem
	if err := item.UnmarshalBinary(b); err != nil {
		return nil, err
	}
	return &item, nil
}

// ListHistory returns the history record list
func (s *conversation) ListHistory(ctx context.Context) (data aigc.HistoryItems, err error) {
	key := s.getKey()
	ss := s.rc.LRange(ctx, key, 0, -1)
	err = ss.ScanSlice(&data)
	return
}

// ClearHistory clears the history records
func (s *conversation) ClearHistory(ctx context.Context) error {
	return s.rc.Del(ctx, s.getKey()).Err()
}

// getKey returns the Redis key for storing history records
func (s *conversation) getKey() string {
	return "convs-" + s.GetID()
}

type convoIDKeyType struct{}

var convoIDKey = convoIDKeyType{}

// ConvoIDFromContext 从 context 获取 csid
func ConvoIDFromContext(ctx context.Context) string {
	if s, ok := ctx.Value(convoIDKey).(string); ok {
		return s
	}
	return ""
}

// ContextWithConvoID 将 csid 添加到 context
func ContextWithConvoID(ctx context.Context, csid string) context.Context {
	return context.WithValue(ctx, convoIDKey, csid)
}

// LoadPreset loads preset configuration from file
func LoadPreset() (doc aigc.Preset, err error) {
	if len(settings.Current.PresetFile) == 0 {
		logger().Infow("preset file is not set")
		return
	}

	var yf *os.File
	yf, err = os.Open(settings.Current.PresetFile)
	if err != nil {
		logger().Infow("load preset fail", "file", settings.Current.PresetFile, "err", err)
		return
	}
	defer func() { _ = yf.Close() }()
	err = yaml.NewDecoder(yf).Decode(&doc)
	if err != nil {
		logger().Infow("decode preset fail", "err", err)
		return
	}
	logger().Debugw("loaded preset", "name", settings.Current.PresetFile)

	return
}
