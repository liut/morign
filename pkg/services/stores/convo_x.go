package stores

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/spf13/cast"

	"github.com/cupogo/andvari/models/oid"
	"github.com/liut/morign/pkg/models/convo"
	"github.com/liut/morign/pkg/models/corpus"
	"github.com/liut/morign/pkg/models/mcps"
	"github.com/liut/morign/pkg/settings"
)

// ConvoStoreX is the conversation storage extension interface
type ConvoStoreX interface {
	SaveSession(ctx context.Context, sess *convo.Session) error
	SaveUser(ctx context.Context, ub convo.UserBasic, id string) error
	SyncUserFromOAuth(ctx context.Context, user IUser) error
	GetUserWith(ctx context.Context, uid string) (*ConvoUser, error)

	GetMyMemoryWithKey(ctx context.Context, key string) (*convo.Memory, error)
	ListMyMomory(ctx context.Context, spec *ConvoMemorySpec) (convo.Memories, error)
	MatchMemories(ctx context.Context, ms MatchSpec) (data convo.Memories, err error)
	SyncEmbeddingMemories(ctx context.Context, spec *ConvoMemorySpec) error

	InvokerForMemoryList() mcps.Invoker
	InvokerForMemoryRecall() mcps.Invoker
	InvokerForMemoryStore() mcps.Invoker
	InvokerForMemoryForget() mcps.Invoker
}

const defaultMemoryLimit = 5
const maxMemoryLimit = 100

// SaveSession saves the session to database
func (s *convoStore) SaveSession(ctx context.Context, obj *convo.Session) error {
	if !obj.IsZeroID() {
		exist := new(convo.Session)
		if err := dbGetWithPKID(ctx, s.w.db, exist, obj.ID); err == nil {
			exist.SetIsUpdate(true)
			exist.SetWith(convo.SessionSet{
				MessageCount: &obj.MessageCount,
				Channel:      &obj.Channel,
			})
			dbMetaUp(ctx, s.w.db, exist)
			return dbUpdate(ctx, s.w.db, obj)
		}
	}
	dbMetaUp(ctx, s.w.db, obj)
	return dbInsert(ctx, s.w.db, obj)
}

func (s *convoStore) GetUserWith(ctx context.Context, uid string) (*ConvoUser, error) {
	user, err := s.GetUser(ctx, uid)
	if err == nil {
		return user, nil
	}
	if errors.Is(err, ErrNoRows) || errors.Is(err, ErrNotFound) {
		user = new(convo.User)
		err = s.w.db.NewSelect().Model(user).
			Where("meta->>'wecomUID' = ?", uid).Limit(1).
			Scan(ctx)
		if err == nil {
			logger().Info("got user with mismatch uid", "uid", uid, "name", user.Username)
			return user, nil
		}
	}

	logger().Info("get user fail", "uid", uid, "err", err)

	return nil, err
}

// SaveUser saves or updates user information
func (s *convoStore) SaveUser(ctx context.Context, in convo.UserBasic, id string) error {
	// 根据 username 查询用户是否存在
	existing := new(convo.User)
	err := dbGetWithUnique(ctx, s.w.db, existing, "username", in.Username)
	if err == nil {
		// 用户存在，更新
		up := convo.UserSet{
			Nickname:   &in.Nickname,
			AvatarPath: &in.AvatarPath,
			Email:      &in.Email,
			Phone:      &in.Phone,
		}
		up.MetaDiff = in.MetaDiff
		if err = s.UpdateUser(ctx, existing.StringID(), up); err != nil {
			logger().Info("update user fail", "err", err, "id", existing.ID)
			return err
		}
		return nil
	}

	if errors.Is(err, ErrNoRows) || errors.Is(err, ErrNotFound) {
		in.MetaAddKVs("oid", id)
		var obj *ConvoUser
		if obj, err = s.CreateUser(ctx, in); err != nil {
			logger().Info("create user fail", "err", err)
		} else {
			logger().Info("create user ok", "id", obj.ID, "input.id", id)
		}
	}

	return err
}

func dbBeforeCreateUser(ctx context.Context, db ormDB, obj *convo.User) error {
	if id, ok := obj.Meta.Get("oid"); ok {
		if obj.SetID(id) {
			obj.MetaUnset("oid")
		}
	}
	return nil
}

func dbAfterSaveUser(ctx context.Context, db ormDB, obj *convo.User) error {
	if wuid := obj.Meta.GetStr(WecomUID); len(wuid) > 0 {
		tub := convo.ThirdUserBasic{OwnerID: obj.ID}
		tub.MetaAddKVs("username", obj.Username)
		tu := convo.NewThirdUserWithBasic(tub)
		tu.SetID(WecomHead + wuid)
		if err := dbInsert(ctx, db, tu, "id", "meta", "owner_id"); err != nil {
			return err
		}
	}
	return nil
}
func (s *convoStore) SyncUserFromOAuth(ctx context.Context, user IUser) error {
	cub := convo.UserBasic{
		Username:   user.GetUID(),
		Nickname:   user.GetName(),
		AvatarPath: user.GetAvatar(),
		Email:      user.GetEmail(),
		Phone:      user.GetPhone(),
	}
	if wuid := WecomUIDFromContext(ctx); len(wuid) > 0 {
		logger().Info("got wecomUID", "uid", wuid)
		cub.MetaAddKVs(WecomUID, wuid)
	}
	id := user.GetOID()
	if err := s.SaveUser(ctx, cub, id); err != nil {
		logger().Info("save user failed", "err", err,
			"oid", id, "uid", user.GetUID())
		return err
	}
	return nil
}

func (s *convoStore) GetMyMemoryWithKey(ctx context.Context, key string) (*convo.Memory, error) {
	user, uok := UserFromContext(ctx)
	if !uok {
		return nil, errors.New("need login")
	}
	ownerID := oid.Cast(user.OID)
	obj := new(convo.Memory)
	err := dbGet(ctx, s.w.db, obj, "owner_id = ? AND key iLIKE ?", ownerID, key)

	if err != nil {
		return nil, err
	}

	return obj, nil
}

// afterCreatedMemory generates vector after memory creation
// Uses the same DocVector table as CobDocument
func (s *convoStore) afterCreatedMemory(ctx context.Context, obj *convo.Memory) error {
	subject := obj.GetSubject()
	dvb := corpus.DocVectorBasic{
		DocID:   obj.ID,
		Subject: subject,
	}
	vec, err := GetEmbedding(ctx, dvb.Subject)
	if err != nil {
		return err
	}
	if len(vec) > 0 {
		dvb.Vector = vec
	}

	_, err = s.w.Corpus().CreateDocVector(ctx, dvb)
	if err != nil {
		logger().Info("create memory vector fail", "dvb", &dvb, "err", err)
		return err
	}
	return nil
}

func (spec *ConvoMemorySpec) SiftX(ctx context.Context, q *ormQuery) *ormQuery {
	if !spec.IsFull {
		q.ExcludeColumn("content")
	}
	if spec.IsOwner {
		user, uok := UserFromContext(ctx)
		if !uok {
			logger().Info("need login when query owner memories")
			return q.Where("FALSE")
		}
		spec.OwnerID = user.OID
		if spec.Limit == 0 {
			spec.Limit = defaultMemoryLimit
		}
	}
	if len(spec.Sort) == 0 {
		spec.Sort = "created DESC"
	}
	return q
}

func (s *convoStore) ListMyMomory(ctx context.Context, spec *ConvoMemorySpec) (convo.Memories, error) {
	user, uok := UserFromContext(ctx)
	if !uok {
		return nil, errors.New("need login")
	}
	spec.OwnerID = user.OID
	data, _, err := s.ListMemory(ctx, spec)
	return data, err
}

// MatchMemories matches memories using vector similarity
func (s *convoStore) MatchMemories(ctx context.Context, ms MatchSpec) (data convo.Memories, err error) {
	ms.setDefaults()

	// Get embedding for the query
	vec, err := GetEmbedding(ctx, ms.Query)
	if err != nil {
		logger().Info("GetEmbedding fail", "err", err)
		return
	}

	// Match vectors
	var ps corpus.DocMatches
	ps, err = s.w.Corpus().MatchVectorWith(ctx, vec, ms.Threshold, ms.Limit)
	if err != nil || len(ps) == 0 {
		logger().Info("no match memories", "query", ms.Query)
		return
	}

	logger().Info("matched memories", "count", len(ps))

	// Load decay metadata for composite re-ranking
	memoryIDs := ps.DocumentIDs()
	decayMetas, loadErr := s.loadDecayMetas(ctx, memoryIDs)
	if loadErr != nil {
		logger().Info("load decay metas fail", "err", loadErr)
	}

	if len(decayMetas) > 0 {
		// Re-rank by composite score
		type scoredID struct {
			id    oid.OID
			score float64
		}
		metaByID := make(map[oid.OID]convo.MemoryDecayMeta, len(decayMetas))
		for _, m := range decayMetas {
			metaByID[m.ID] = m
		}

		scored := make([]scoredID, 0, len(ps))
		for _, m := range ps {
			meta, ok := metaByID[m.DocID]
			if !ok {
				scored = append(scored, scoredID{id: m.DocID, score: float64(m.Similarity)})
				continue
			}
			r := convo.CalcRetention(meta.LastAccessedAt, meta.DecayRate)
			fm := convo.ForgottenMultiplier(r, settings.Current.MemoryForgetThreshold)
			scored = append(scored, scoredID{id: m.DocID, score: float64(m.Similarity) * r * fm})
		}

		sort.Slice(scored, func(i, j int) bool { return scored[i].score > scored[j].score })

		limit := ms.Limit
		if limit > len(scored) {
			limit = len(scored)
		}
		topIDs := make(oid.OIDs, limit)
		for i := 0; i < limit; i++ {
			topIDs[i] = scored[i].id
		}

		spec := &ConvoMemorySpec{IsFull: true}
		spec.IDs = topIDs
		err = queryList(ctx, s.w.db, spec, &data).Scan(ctx)
	} else {
		// Fallback: fetch by original vector order
		spec := &ConvoMemorySpec{IsFull: true}
		spec.IDs = ps.DocumentIDs()
		err = queryList(ctx, s.w.db, spec, &data).Scan(ctx)
	}
	if err != nil {
		logger().Info("list memories fail", "err", err)
	}
	return
}

// SyncEmbeddingMemories generates vectors for all memories
func (s *convoStore) SyncEmbeddingMemories(ctx context.Context, spec *ConvoMemorySpec) error {
	spec.IsFull = true
	data, _, err := s.ListMemory(ctx, spec)
	if err != nil {
		return err
	}

	for _, mem := range data {
		subject := mem.GetSubject()
		vec, err := GetEmbedding(ctx, subject)
		if err != nil {
			return err
		}

		exist := new(corpus.DocVector)
		err = dbGetWithUnique(ctx, s.w.db, exist, "doc_id", mem.ID)
		if err == nil {
			exist.SetWith(corpus.DocVectorSet{
				Subject: &subject,
				Vector:  &vec,
			})
			if err = dbUpdate(ctx, s.w.db, exist); err != nil {
				return err
			}
		} else {
			dv := corpus.NewDocVectorWithBasic(corpus.DocVectorBasic{
				DocID:   mem.ID,
				Subject: subject,
				Vector:  vec,
			})
			if err = dbInsert(ctx, s.w.db, dv); err != nil {
				return err
			}
		}
	}
	return nil
}

// InvokerForMemoryList returns an invoker for listing memories
func (s *convoStore) InvokerForMemoryList() mcps.Invoker {
	return func(ctx context.Context, args map[string]any) (map[string]any, error) {

		limit := defaultMemoryLimit
		if l := cast.ToInt(args["limit"]); l > 0 {
			limit = l
			if limit > maxMemoryLimit {
				limit = maxMemoryLimit
			}
		}

		spec := &ConvoMemorySpec{
			Cate: cast.ToString(args["category"]),
		}
		spec.Limit = limit
		spec.Sort = "id desc"

		includeContent := true
		if v, ok := args["include_content"]; ok {
			if ic, err := cast.ToBoolE(v); err == nil {
				includeContent = ic
			}
		}
		spec.IsFull = includeContent

		// Support tier filtering
		if tier, ok := args["tier"]; ok {
			spec.Tier = cast.ToString(tier)
		}

		data, err := s.ListMyMomory(ctx, spec)
		if err != nil {
			return mcps.BuildToolErrorResult(err.Error()), nil
		}

		logger().Debug("invoke memory list", "args", args, "ic", includeContent)

		var results []map[string]any
		for _, m := range data {
			item := map[string]any{
				"key":      m.Key,
				"category": m.Cate,
				"tier":     m.Tier,
			}
			if includeContent {
				item["content"] = m.Content
			}
			results = append(results, item)
		}

		return mcps.BuildToolSuccessResult(results), nil
	}
}

// InvokerForMemoryRecall returns an invoker for searching memories
func (s *convoStore) InvokerForMemoryRecall() mcps.Invoker {
	return func(ctx context.Context, args map[string]any) (map[string]any, error) {
		query := cast.ToString(args["query"])
		if query == "" {
			return mcps.BuildToolErrorResult("missing required argument: query"), nil
		}

		limit := defaultMemoryLimit
		if l := cast.ToInt(args["limit"]); l > 0 {
			limit = min(l, maxMemoryLimit)
		}

		// Use vector-based matching
		data, err := s.MatchMemories(ctx, MatchSpec{
			Query: query,
			Limit: limit,
		})
		if err != nil {
			return mcps.BuildToolErrorResult(err.Error()), nil
		}

		if len(data) == 0 {
			return mcps.BuildToolSuccessResult("No matching memories found"), nil
		}

		var results []map[string]any
		for _, m := range data {
			results = append(results, map[string]any{
				"date":     m.GetUpdated().Format(time.DateOnly),
				"key":      m.Key,
				"category": m.Cate,
				"content":  m.Content,
				"tier":     m.Tier,
			})
		}

		// Reinforce accessed memories (best-effort, don't block results)
		for _, m := range data {
			s.reinforceMemory(ctx, m)
		}

		return mcps.BuildToolSuccessResult(results), nil
	}
}

// InvokerForMemoryStore returns an invoker for storing a memory
func (s *convoStore) InvokerForMemoryStore() mcps.Invoker {
	return func(ctx context.Context, args map[string]any) (map[string]any, error) {
		user, uok := UserFromContext(ctx)
		if !uok {
			return nil, errors.New("need login")
		}
		key := cast.ToString(args["key"])
		if key == "" {
			return mcps.BuildToolErrorResult("missing required argument: key"), nil
		}

		content := cast.ToString(args["content"])
		if content == "" {
			return mcps.BuildToolErrorResult("missing required argument: content"), nil
		}

		category := cast.ToString(args["category"])
		if category == "" {
			category = "custom"
		}

		// Try to get existing memory by key
		existing, err := s.GetMyMemoryWithKey(ctx, key)
		if err == nil {
			// Update existing
			existing.SetIsUpdate(true)
			existing.SetWith(convo.MemorySet{
				Cate:    &category,
				Content: &content,
			})
			dbMetaUp(ctx, s.w.db, existing)
			if err := dbUpdate(ctx, s.w.db, existing); err != nil {
				return mcps.BuildToolErrorResult(err.Error()), nil
			}
			return mcps.BuildToolSuccessResult(map[string]any{
				"action":   "updated",
				"key":      key,
				"category": category,
			}), nil
		}

		// Create new with tier routing
		importanceScore := convo.EvaluateImportance(content)
		tier := convo.RouteTier(importanceScore,
			settings.Current.MemoryLongTermThreshold,
			settings.Current.MemoryShortTermThreshold)
		dr := convo.TierDecayRate(tier)
		now := time.Now()

		mb := convo.MemoryBasic{
			Key:             key,
			Cate:            category,
			Content:         content,
			Tier:            tier,
			ImportanceScore: importanceScore,
			DecayRate:       dr,
			LastAccessedAt:  now,
		}
		mb.SetOwnerID(user.OID)
		obj, err := s.CreateMemory(ctx, mb)
		if err != nil {
			return mcps.BuildToolErrorResult(err.Error()), nil
		}

		return mcps.BuildToolSuccessResult(map[string]any{
			"action":     "created",
			"key":        key,
			"category":   category,
			"memory_id":  obj.StringID(),
			"tier":       tier,
			"importance": importanceScore,
		}), nil
	}
}

// InvokerForMemoryForget returns an invoker for deleting a memory
func (s *convoStore) InvokerForMemoryForget() mcps.Invoker {
	return func(ctx context.Context, args map[string]any) (map[string]any, error) {
		key := cast.ToString(args["key"])
		if key == "" {
			return mcps.BuildToolErrorResult("missing required argument: key"), nil
		}

		// Try to get memory by key
		existing, err := s.GetMyMemoryWithKey(ctx, key)
		if err != nil {
			return mcps.BuildToolSuccessResult(map[string]any{
				"action": "not_found",
				"key":    key,
			}), nil
		}

		// Clean up vector entry
		if _, err := s.w.db.NewDelete().Model((*corpus.DocVector)(nil)).
			Where("doc_id = ?", existing.ID).Exec(ctx); err != nil {
			logger().Info("delete memory vector fail", "id", existing.ID, "err", err)
		}

		if err := s.DeleteMemory(ctx, existing.StringID()); err != nil {
			return mcps.BuildToolErrorResult(err.Error()), nil
		}

		return mcps.BuildToolSuccessResult(map[string]any{
			"action": "deleted",
			"key":    key,
		}), nil
	}
}

// loadDecayMetas loads decay metadata for a set of memory IDs.
func (s *convoStore) loadDecayMetas(ctx context.Context, ids oid.OIDs) ([]convo.MemoryDecayMeta, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var metas []convo.MemoryDecayMeta
	err := s.w.db.NewSelect().
		Table("convo_memory").
		Column("id", "tier", "decay_rate", "last_accessed_at").
		Where("id IN (?)", pgList(ids)).
		Scan(ctx, &metas)
	if err != nil {
		logger().Info("load decay metas fail", "err", err, "ids", ids)
		return nil, err
	}
	return metas, nil
}

// reinforceMemory updates access stats and checks tier promotion for a recalled memory.
func (s *convoStore) reinforceMemory(ctx context.Context, m convo.Memory) {
	now := time.Now()
	accessCount := m.AccessCount + 1

	// Check tier promotion
	ms := convo.MemorySet{
		LastAccessedAt: &now,
		AccessCount:    &accessCount,
	}
	newTier := convo.PromoteTier(m.Tier, accessCount,
		settings.Current.MemoryPromoteW2S,
		settings.Current.MemoryPromoteS2L)
	if newTier != "" {
		decayRate := convo.TierDecayRate(newTier)
		ms.Tier = &newTier
		ms.DecayRate = &decayRate
	}

	m.SetIsUpdate(true)
	m.SetWith(ms)
	dbMetaUp(ctx, s.w.db, &m)
	if err := dbUpdate(ctx, s.w.db, &m); err != nil {
		logger().Info("reinforce memory fail", "id", m.ID, "err", err)
	}
}
