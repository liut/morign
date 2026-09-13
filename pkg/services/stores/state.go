package stores

import (
	"context"
	"net/http"
	"time"
)

const StateExpiration = time.Minute * 15

type StateStore interface {
	Save(w http.ResponseWriter, state string) error
	Verify(r *http.Request, state string) bool
	Wipe(w http.ResponseWriter, state string)
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
