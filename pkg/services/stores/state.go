package stores

import (
	"context"
	"net/http"
	"time"

	redis "github.com/redis/go-redis/v9"
)

const StateExpiration = time.Minute * 15

type StateStore interface {
	Save(w http.ResponseWriter, state string) error
	Verify(r *http.Request, state string) bool
	Wipe(w http.ResponseWriter, state string)
}

// VerifierStore 按 state 存取 OAuth2 PKCE code verifier
type VerifierStore interface {
	Save(state, verifier string) error
	Load(state string) string
	Wipe(state string)
}

type stateStore struct{ rc RedisClient }

func (ss *stateStore) Save(_ http.ResponseWriter, state string) error {
	hit := time.Now().UnixMilli()
	err := ss.rc.Set(context.Background(), getStateKey(state), hit, StateExpiration).Err()
	logger().Info("save state", "state", state, "hit", hit, "err", err)
	return err
}

func (ss *stateStore) Verify(r *http.Request, state string) bool {
	if state == "" && r != nil {
		state = r.FormValue("state")
	}
	var hit int64
	err := ss.rc.Get(context.Background(), getStateKey(state)).Scan(&hit)
	return err == nil && time.UnixMilli(hit).After(time.Now().Add(-StateExpiration))
}

func (ss *stateStore) Wipe(_ http.ResponseWriter, state string) {
	_ = ss.rc.Del(context.Background(), getStateKey(state))
}

func getStateKey(state string) string {
	return "state-" + state
}

type verifierStore struct{ rc RedisClient }

func (vs *verifierStore) Save(state, verifier string) error {
	err := vs.rc.Set(context.Background(), getVerifierKey(state), verifier, StateExpiration).Err()
	logger().Info("save verifier", "state", state, "err", err)
	return err
}

func (vs *verifierStore) Load(state string) string {
	res := vs.rc.Get(context.Background(), getVerifierKey(state))
	if err := res.Err(); err != nil {
		if err != redis.Nil {
			logger().Info("load verifier", "state", state, "err", err)
		}
		return ""
	}
	return res.Val()
}

func (vs *verifierStore) Wipe(state string) {
	_ = vs.rc.Del(context.Background(), getVerifierKey(state))
}

func getVerifierKey(state string) string {
	return "verifier-" + state
}
