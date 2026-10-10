package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cupogo/andvari/models/oid"
	"github.com/go-chi/chi/v5"
	auth "github.com/liut/simpauth"

	"github.com/liut/morign/pkg/models/convo"
	"github.com/liut/morign/pkg/services/stores"
	"github.com/liut/morign/pkg/settings"
)

type memConvoStore struct {
	stores.ConvoStore
	byID map[string]*convo.Memory
}

func (f *memConvoStore) ListMemory(ctx context.Context, spec *stores.ConvoMemorySpec) (convo.Memories, int, error) {
	return nil, 0, nil
}

func (f *memConvoStore) GetMemory(ctx context.Context, id string) (*convo.Memory, error) {
	obj, ok := f.byID[id]
	if !ok {
		return nil, stores.ErrNotFound
	}
	if !mayReachMemory(ctx, obj) {
		return nil, stores.ErrForbidden
	}
	return obj, nil
}

func (f *memConvoStore) UpdateMemory(ctx context.Context, id string, in convo.MemorySet) error {
	obj, ok := f.byID[id]
	if !ok {
		return stores.ErrNotFound
	}
	if !mayReachMemory(ctx, obj) {
		return stores.ErrForbidden
	}
	obj.SetWith(in)
	return nil
}

func (f *memConvoStore) DeleteMemory(ctx context.Context, id string) error {
	obj, ok := f.byID[id]
	if !ok {
		return nil
	}
	if !mayReachMemory(ctx, obj) {
		return stores.ErrForbidden
	}
	delete(f.byID, id)
	return nil
}

func (f *memConvoStore) CreateMemory(ctx context.Context, in convo.MemoryBasic) (*convo.Memory, error) {
	obj := convo.NewMemoryWithBasic(in)
	obj.SetID(oid.NewID(oid.OtEvent))
	f.byID[obj.ID.String()] = obj
	return obj, nil
}

// mayReachMemory 复刻存储层的归属判定，让接口层测试能构造 403/404 输入；
// 判定本身由 stores 包的单测覆盖。
func mayReachMemory(ctx context.Context, obj *convo.Memory) bool {
	if stores.IsKeeper(ctx) {
		return true
	}
	user, ok := stores.UserFromContext(ctx)
	return ok && obj.OwnerID == oid.Cast(user.OID)
}

type memConvoStorage struct {
	fakeStorage
	convo *memConvoStore
}

func (f *memConvoStorage) Convo() stores.ConvoStore { return f.convo }

func memoryAPI() (*api, *memConvoStore) {
	fc := &memConvoStore{byID: map[string]*convo.Memory{}}
	return &api{sto: &memConvoStorage{convo: fc}}, fc
}

func memoryRouter(a *api) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/api/convo/memories", a.getConvoMemories)
	r.Get("/api/convo/memories/{id}", a.getConvoMemory)
	r.Post("/api/convo/memories", a.postConvoMemory)
	r.Put("/api/convo/memories/{id}", a.putConvoMemory)
	r.Delete("/api/convo/memories/{id}", a.deleteConvoMemory)
	return r
}

func seedMemory(f *memConvoStore, owner oid.OID, key string) *convo.Memory {
	obj := convo.NewMemoryWithBasic(convo.MemoryBasic{OwnerID: owner, Key: key, Cate: "core", Content: "content-" + key})
	obj.SetID(oid.NewID(oid.OtEvent))
	f.byID[obj.ID.String()] = obj
	return obj
}

func asUser(id oid.OID) func(*http.Request) *http.Request {
	return func(req *http.Request) *http.Request { return withUser(req, id.String()) }
}

func asKeeper(id oid.OID) func(*http.Request) *http.Request {
	return func(req *http.Request) *http.Request {
		ctx := auth.ContextWithUser(req.Context(), &auth.User{
			OID:   id.String(),
			UID:   "keeper",
			Name:  "keeper",
			Roles: auth.Names{settings.Current.KeeperRole},
		})
		return req.WithContext(ctx)
	}
}

func doMemoryReq(r *chi.Mux, method, path, body string, apply func(*http.Request) *http.Request) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, apply(req))
	return rr
}

func TestMemoryAPIForbidden(t *testing.T) {
	a, fc := memoryAPI()
	r := memoryRouter(a)
	owner, other := oid.NewID(oid.OtAccount), oid.NewID(oid.OtAccount)
	obj := seedMemory(fc, other, "other")

	cases := []struct {
		method string
		body   string
	}{
		{http.MethodGet, ""},
		{http.MethodPut, `{"content":"hijack"}`},
		{http.MethodDelete, ""},
	}
	for _, c := range cases {
		rr := doMemoryReq(r, c.method, "/api/convo/memories/"+obj.ID.String(), c.body, asUser(owner))
		if got := respCode(t, rr); got != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403 (body %s)", c.method, got, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "forbidden") {
			t.Errorf("%s: body missing message: %s", c.method, rr.Body.String())
		}
	}
	if fc.byID[obj.ID.String()].Content != "content-other" {
		t.Error("forbidden update must not change the memory")
	}
}

func TestMemoryAPINotFound(t *testing.T) {
	a, _ := memoryAPI()
	r := memoryRouter(a)
	missing := oid.NewID(oid.OtEvent).String()

	for _, method := range []string{http.MethodGet, http.MethodPut} {
		body := ""
		if method == http.MethodPut {
			body = `{"content":"x"}`
		}
		rr := doMemoryReq(r, method, "/api/convo/memories/"+missing, body, asUser(oid.NewID(oid.OtAccount)))
		if got := respCode(t, rr); got != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404 (body %s)", method, got, rr.Body.String())
		}
	}
}

func TestMemoryAPIKeeper(t *testing.T) {
	a, fc := memoryAPI()
	r := memoryRouter(a)
	keeper, owner := oid.NewID(oid.OtAccount), oid.NewID(oid.OtAccount)
	obj := seedMemory(fc, owner, "other")

	rr := doMemoryReq(r, http.MethodGet, "/api/convo/memories/"+obj.ID.String(), "", asKeeper(keeper))
	if rr.Code != http.StatusOK {
		t.Fatalf("keeper detail status = %d, body %s", rr.Code, rr.Body.String())
	}

	rr = doMemoryReq(r, http.MethodPut, "/api/convo/memories/"+obj.ID.String(), `{"content":"rewritten"}`, asKeeper(keeper))
	if rr.Code != http.StatusOK {
		t.Fatalf("keeper update status = %d, body %s", rr.Code, rr.Body.String())
	}
	if fc.byID[obj.ID.String()].Content != "rewritten" {
		t.Error("keeper update not applied")
	}

	rr = doMemoryReq(r, http.MethodDelete, "/api/convo/memories/"+obj.ID.String(), "", asKeeper(keeper))
	if rr.Code != http.StatusOK {
		t.Fatalf("keeper delete status = %d, body %s", rr.Code, rr.Body.String())
	}
	if fc.byID[obj.ID.String()] != nil {
		t.Error("keeper delete not applied")
	}
}

func TestMemoryAPIOwnerAndCreate(t *testing.T) {
	a, fc := memoryAPI()
	r := memoryRouter(a)
	owner := oid.NewID(oid.OtAccount)
	obj := seedMemory(fc, owner, "mine")

	rr := doMemoryReq(r, http.MethodGet, "/api/convo/memories/"+obj.ID.String(), "", asUser(owner))
	if rr.Code != http.StatusOK {
		t.Fatalf("owner detail status = %d, body %s", rr.Code, rr.Body.String())
	}

	rr = doMemoryReq(r, http.MethodDelete, "/api/convo/memories/"+obj.ID.String(), "", asUser(owner))
	if rr.Code != http.StatusOK {
		t.Fatalf("owner delete status = %d, body %s", rr.Code, rr.Body.String())
	}

	body := `{"ownerID":"` + oid.NewID(oid.OtAccount).String() + `","key":"k","cate":"core","content":"c"}`
	rr = doMemoryReq(r, http.MethodPost, "/api/convo/memories", body, asUser(owner))
	if rr.Code != http.StatusOK {
		t.Fatalf("create status = %d, body %s", rr.Code, rr.Body.String())
	}
	var out struct {
		Result struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode create response: %v (body %s)", err, rr.Body.String())
	}
	if fc.byID[out.Result.ID] == nil {
		t.Errorf("created memory %q not stored (body %s)", out.Result.ID, rr.Body.String())
	}
}
