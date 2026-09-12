package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/liut/morign/pkg/models/convo"
	"github.com/liut/morign/pkg/models/corpus"
	"github.com/liut/morign/pkg/services/stores"
)

// query 绑定由 github.com/cupogo/querybind 提供：这里用真实 chi 路由与生成的
// list handler 验证参数确实进了 spec（而不是只看状态码）。

type bindCorpuStore struct {
	stores.CorpuStore
	got   stores.CobDocumentSpec
	data  corpus.Documents
	total int
}

func (f *bindCorpuStore) ListDocument(_ context.Context, spec *stores.CobDocumentSpec) (corpus.Documents, int, error) {
	f.got = *spec
	return f.data, f.total, nil
}

type bindConvoStore struct {
	stores.ConvoStore
	got stores.ConvoSessionSpec
}

func (f *bindConvoStore) ListSession(_ context.Context, spec *stores.ConvoSessionSpec) (convo.Sessions, int, error) {
	f.got = *spec
	return nil, 0, nil
}

type bindFakeStorage struct {
	stores.Storage
	corpu stores.CorpuStore
	convo stores.ConvoStore
}

func (f *bindFakeStorage) Corpus() stores.CorpuStore { return f.corpu }
func (f *bindFakeStorage) Convo() stores.ConvoStore  { return f.convo }

func bindRouter(cs *bindCorpuStore, vs *bindConvoStore) *chi.Mux {
	a := &api{sto: &bindFakeStorage{corpu: cs, convo: vs}}
	r := chi.NewRouter()
	r.Get("/api/corpus/documents", a.getCorpusDocuments)
	r.Get("/api/convo/sessions", a.getConvoSessions)
	return r
}

func doGet(t *testing.T, r *chi.Mux, target string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, target, nil))
	return rr
}

func resultTotal(t *testing.T, rr *httptest.ResponseRecorder) float64 {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &m), rr.Body.String())
	result, ok := m["result"].(map[string]any)
	require.True(t, ok, "unexpected body: %s", rr.Body.String())
	total, _ := result["total"].(float64)
	return total
}

// 匿名嵌入的 PageSpec / ModelSpec 必须被展开绑定。
func TestListQueryBindingEmbeddedSpec(t *testing.T) {
	cs := &bindCorpuStore{}
	r := bindRouter(cs, &bindConvoStore{})

	rr := doGet(t, r, "/api/corpus/documents?page=2&limit=20&skip=5&sort=-updated"+
		"&ids=aaa,bbb&created=2026-01-01&isDelete=true&title=abc&match=semantic")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	assert.Equal(t, 2, cs.got.Page)
	assert.Equal(t, 20, cs.got.Limit)
	assert.Equal(t, 5, cs.got.Skip)
	assert.Equal(t, "-updated", cs.got.Sort)
	assert.Equal(t, "aaa,bbb", string(cs.got.IDsStr))
	assert.Equal(t, "2026-01-01", cs.got.Created)
	assert.True(t, cs.got.IsDelete)
	assert.Equal(t, "abc", cs.got.Title)
	assert.Equal(t, "semantic", cs.got.Match)
	assert.Empty(t, cs.got.IDs, `form:"-" 的字段不应被绑定`)
}

// 文本枚举参数（status=closed）必须被解码，非法值返回 400。
func TestListQueryBindingEnum(t *testing.T) {
	vs := &bindConvoStore{}
	r := bindRouter(&bindCorpuStore{}, vs)

	rr := doGet(t, r, "/api/convo/sessions?status=closed&title=abc")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, convo.SessionStatusClosed, vs.got.Status)
	assert.Equal(t, "abc", vs.got.Title)

	rr = doGet(t, r, "/api/convo/sessions?status=bogus")
	assert.Equal(t, http.StatusBadRequest, respCode(t, rr), rr.Body.String())
}

// 无 limit/page 时底层不统计总数，用返回行数兜底；分页查询里的 0 是真实结果。
func TestListQueryBindingTotalFallback(t *testing.T) {
	cs := &bindCorpuStore{data: corpus.Documents{{}, {}, {}}, total: 0}
	r := bindRouter(cs, &bindConvoStore{})

	rr := doGet(t, r, "/api/corpus/documents")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, float64(3), resultTotal(t, rr))

	cs = &bindCorpuStore{total: 0}
	r = bindRouter(cs, &bindConvoStore{})
	rr = doGet(t, r, "/api/corpus/documents?page=1&limit=20")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, float64(0), resultTotal(t, rr))
	assert.Equal(t, 20, cs.got.Limit)
}

// 绑定失败要变成 400，而不是 500。
func TestListQueryBindingBadValue(t *testing.T) {
	cs := &bindCorpuStore{}
	r := bindRouter(cs, &bindConvoStore{})

	rr := doGet(t, r, "/api/corpus/documents?page=abc")
	assert.Equal(t, http.StatusBadRequest, respCode(t, rr), rr.Body.String())
	assert.Equal(t, 0, cs.got.Page)
}
