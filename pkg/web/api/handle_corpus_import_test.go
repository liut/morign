package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cupogo/andvari/models/oid"
	"github.com/go-chi/chi/v5"
	auth "github.com/liut/simpauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/liut/morign/pkg/models/aigc"
	"github.com/liut/morign/pkg/models/corpus"
	"github.com/liut/morign/pkg/services/stores"
	"github.com/liut/morign/pkg/settings"
)

type fakeCorpuStore struct {
	stores.CorpuStore
	seq     int
	created []corpus.ImportTaskBasic
	tasks   []corpus.ImportTask
}

func (f *fakeCorpuStore) CreateImportTask(ctx context.Context, in corpus.ImportTaskBasic) (*corpus.ImportTask, error) {
	f.created = append(f.created, in)
	f.seq++
	obj := corpus.NewImportTaskWithBasic(in)
	obj.SetID(oid.NewID(oid.OtTask))
	f.tasks = append(f.tasks, *obj)
	return obj, nil
}

func (f *fakeCorpuStore) ListImportTask(ctx context.Context, spec *stores.CobImportTaskSpec) (corpus.ImportTasks, int, error) {
	var data corpus.ImportTasks
	for i := range f.tasks {
		t := f.tasks[i]
		if spec.Status != 0 && t.Status != spec.Status {
			continue
		}
		data = append(data, t)
	}
	return data, len(data), nil
}

func (f *fakeCorpuStore) GetImportTask(ctx context.Context, id string) (*corpus.ImportTask, error) {
	for i := range f.tasks {
		if f.tasks[i].StringID() == id {
			return &f.tasks[i], nil
		}
	}
	if !oid.Cast(id).Valid() {
		return nil, stores.ErrInvalidID
	}
	return nil, stores.ErrNotFound
}

type importFakeStorage struct {
	corpu *fakeCorpuStore
}

func (f *importFakeStorage) Preset() aigc.Preset                { return aigc.Preset{} }
func (f *importFakeStorage) Corpus() stores.CorpuStore          { return f.corpu }
func (f *importFakeStorage) KB() stores.CorpuStore              { return f.corpu }
func (f *importFakeStorage) MCP() stores.MCPStore               { return nil }
func (f *importFakeStorage) Convo() stores.ConvoStore           { return nil }
func (f *importFakeStorage) State() stores.StateStore           { return nil }
func (f *importFakeStorage) Capability() stores.CapabilityStore { return nil }
func (f *importFakeStorage) Skill() stores.SkillStore           { return nil }

func importAPI() (*api, *fakeCorpuStore) {
	fc := &fakeCorpuStore{}
	return &api{sto: &importFakeStorage{corpu: fc}}, fc
}

func importRouter(a *api) *chi.Mux {
	r := chi.NewRouter()
	r.Use(a.authPerm("corpus-imports"))
	r.Post("/api/corpus/imports", a.postCorpusImport)
	r.Get("/api/corpus/imports", a.getCorpusImports)
	r.Get("/api/corpus/imports/{id}", a.getCorpusImport)
	return r
}

func withKeeper(req *http.Request) *http.Request {
	ctx := auth.ContextWithUser(req.Context(), &auth.User{
		OID:   "o1",
		UID:   "keeper",
		Name:  "keeper",
		Roles: []string{settings.Current.KeeperRole},
	})
	return req.WithContext(ctx)
}

func withNonKeeper(req *http.Request) *http.Request {
	ctx := auth.ContextWithUser(req.Context(), &auth.User{
		OID:   "o2",
		UID:   "user",
		Name:  "user",
		Roles: []string{"user"},
	})
	return req.WithContext(ctx)
}

func multipartCSVRequest(t *testing.T, filename, content string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = fw.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, mw.Close())
	req := httptest.NewRequest(http.MethodPost, "/api/corpus/imports", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func decodeResult(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &m))
	return m
}

func TestImportUploadAPI(t *testing.T) {
	a, fc := importAPI()
	r := importRouter(a)

	csv := "\xef\xbb\xbftitle,heading,content\nhello,a,world\n"
	req := multipartCSVRequest(t, "docs.csv", csv)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, withKeeper(req))

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	result := decodeResult(t, rr)["result"].(map[string]any)
	assert.Equal(t, "docs.csv", result["filename"])
	assert.Equal(t, "pending", result["status"])
	assert.NotContains(t, result, "data")
	assert.NotContains(t, result, "errors")

	require.Len(t, fc.created, 1)
	assert.Equal(t, "title,heading,content\nhello,a,world\n", fc.created[0].Data, "BOM must be stripped")
	assert.Equal(t, corpus.ImportTaskStatusPending, fc.created[0].Status)
}

func TestImportUploadAPIAuth(t *testing.T) {
	a, fc := importAPI()
	r := importRouter(a)

	req := multipartCSVRequest(t, "docs.csv", "title,heading,content\nhello,a,world\n")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)

	req = multipartCSVRequest(t, "docs.csv", "title,heading,content\nhello,a,world\n")
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, withNonKeeper(req))
	assert.Equal(t, http.StatusForbidden, rr.Code)

	req = multipartCSVRequest(t, "docs.csv", "title,heading,content\nhello,a,world\n")
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, withKeeper(req))
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Len(t, fc.created, 1)
}

func TestImportUploadAPIValidation(t *testing.T) {
	a, fc := importAPI()
	r := importRouter(a)

	cases := []struct {
		name    string
		content string
		want    int
	}{
		{"bad header", "a,b,c\nx,y,z\n", http.StatusBadRequest},
		{"non utf8", "title,heading,content\n\xff\xfe\x00\n", http.StatusBadRequest},
		{"too large", strings.Repeat("a", maxImportUploadSize+1), http.StatusRequestEntityTooLarge},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := multipartCSVRequest(t, "docs.csv", c.content)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, withKeeper(req))
			assert.Equal(t, c.want, respCode(t, rr), rr.Body.String())
		})
	}
	assert.Empty(t, fc.created)
}

func TestImportUploadAPIMissingFile(t *testing.T) {
	a, fc := importAPI()
	r := importRouter(a)

	req := httptest.NewRequest(http.MethodPost, "/api/corpus/imports", strings.NewReader("title,heading,content\n"))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, withKeeper(req))

	assert.Equal(t, http.StatusBadRequest, respCode(t, rr))
	assert.Empty(t, fc.created)
}

func TestImportListAPI(t *testing.T) {
	a, fc := importAPI()
	r := importRouter(a)

	for i, status := range []corpus.ImportTaskStatus{
		corpus.ImportTaskStatusPending,
		corpus.ImportTaskStatusSucceeded,
		corpus.ImportTaskStatusFailed,
	} {
		obj := corpus.NewImportTaskWithBasic(corpus.ImportTaskBasic{
			Filename: "docs.csv",
			Status:   status,
			Data:     "secret-csv",
			Total:    i + 1,
		})
		obj.SetID(oid.NewID(oid.OtTask))
		fc.tasks = append(fc.tasks, *obj)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/corpus/imports", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, withKeeper(req))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	result := decodeResult(t, rr)["result"].(map[string]any)
	assert.Equal(t, float64(3), result["total"])
	items := result["data"].([]any)
	require.Len(t, items, 3)
	for _, item := range items {
		m := item.(map[string]any)
		assert.NotContains(t, m, "data")
		assert.NotContains(t, m, "errors")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/corpus/imports?status=pending", nil)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, withKeeper(req))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	result = decodeResult(t, rr)["result"].(map[string]any)
	assert.Equal(t, float64(1), result["total"])

	req = httptest.NewRequest(http.MethodGet, "/api/corpus/imports?status=1", nil)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, withKeeper(req))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	result = decodeResult(t, rr)["result"].(map[string]any)
	assert.Equal(t, float64(1), result["total"])

	req = httptest.NewRequest(http.MethodGet, "/api/corpus/imports?status=bogus", nil)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, withKeeper(req))
	assert.Equal(t, http.StatusBadRequest, respCode(t, rr))
}

func TestImportListAPIAuth(t *testing.T) {
	a, _ := importAPI()
	r := importRouter(a)

	req := httptest.NewRequest(http.MethodGet, "/api/corpus/imports", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, withNonKeeper(req))
	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestImportDetailAPI(t *testing.T) {
	a, fc := importAPI()
	r := importRouter(a)

	obj := corpus.NewImportTaskWithBasic(corpus.ImportTaskBasic{
		Filename: "docs.csv",
		Status:   corpus.ImportTaskStatusSucceeded,
		Data:     "secret-csv",
		Total:    2,
		Success:  1,
		Failed:   1,
		Errors: []corpus.ImportFailure{{
			Line:   3,
			Title:  "hello",
			Reason: "数据无效",
		}},
	})
	obj.SetID(oid.NewID(oid.OtTask))
	fc.tasks = append(fc.tasks, *obj)

	req := httptest.NewRequest(http.MethodGet, "/api/corpus/imports/"+obj.StringID(), nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, withKeeper(req))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	result := decodeResult(t, rr)["result"].(map[string]any)
	assert.NotContains(t, result, "data")
	assert.Equal(t, "succeeded", result["status"])
	assert.Equal(t, float64(2), result["total"])
	errs := result["errors"].([]any)
	require.Len(t, errs, 1)
	assert.Equal(t, float64(3), errs[0].(map[string]any)["line"])
}

func TestImportDetailAPINotFound(t *testing.T) {
	a, _ := importAPI()
	r := importRouter(a)

	req := httptest.NewRequest(http.MethodGet, "/api/corpus/imports/"+oid.NewObjID(oid.OtTask), nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, withKeeper(req))
	assert.Equal(t, http.StatusNotFound, respCode(t, rr))
}

func TestImportDetailAPIInvalidID(t *testing.T) {
	a, _ := importAPI()
	r := importRouter(a)

	req := httptest.NewRequest(http.MethodGet, "/api/corpus/imports/not-an-oid", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, withKeeper(req))
	assert.Equal(t, http.StatusBadRequest, respCode(t, rr))
}

func TestImportListAPIInvalidPaging(t *testing.T) {
	a, _ := importAPI()
	r := importRouter(a)

	for _, q := range []string{"limit=abc", "limit=-1", "limit=0", "page=abc", "page=-1", "page=0"} {
		req := httptest.NewRequest(http.MethodGet, "/api/corpus/imports?"+q, nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, withKeeper(req))
		assert.Equal(t, http.StatusBadRequest, respCode(t, rr), "query %s", q)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/corpus/imports?limit=1&page=1", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, withKeeper(req))
	assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
}
