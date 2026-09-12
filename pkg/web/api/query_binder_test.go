package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cupogo/andvari/models/field"

	"github.com/liut/morign/pkg/models/corpus"
	"github.com/liut/morign/pkg/services/stores"
)

func bindQuery(t *testing.T, spec any, raw string) {
	t.Helper()
	u, err := url.Parse("/api/m/?" + raw)
	require.NoError(t, err)
	require.NoError(t, queryBinder.Bind(spec, u), raw)
}

func TestQueryBinderWithEmbeddedSpec(t *testing.T) {
	var spec stores.CobDocumentSpec
	bindQuery(t, &spec, "page=2&limit=20&sort=-updated&ids=aaa,bbb&created=2026-01-01&updated=2026-02-02&isDelete=true&creatorID=u1&title=abc&heading=h&match=semantic")

	assert.Equal(t, 2, spec.Page)
	assert.Equal(t, 20, spec.Limit)
	assert.Equal(t, 20, spec.GetSkip())
	assert.Equal(t, "-updated", spec.Sort)
	assert.Equal(t, "aaa,bbb", string(spec.IDsStr))
	assert.Equal(t, "2026-01-01", spec.Created)
	assert.Equal(t, "2026-02-02", spec.Updated)
	assert.Equal(t, "u1", spec.CreatorID)
	assert.True(t, spec.IsDelete)
	assert.Equal(t, "abc", spec.Title)
	assert.Equal(t, "h", spec.Heading)
	assert.Equal(t, "semantic", spec.Match)
}

func TestQueryBinderWithEnumText(t *testing.T) {
	var task stores.CobImportTaskSpec
	bindQuery(t, &task, "status=failed&filename=a.csv&sort=created")
	assert.Equal(t, corpus.ImportTaskStatusFailed, task.Status)
	assert.Equal(t, "a.csv", task.Filename)
	assert.Equal(t, "created", task.Sort)

	var session stores.ConvoSessionSpec
	bindQuery(t, &session, "status=closed")
	assert.Equal(t, "closed", session.Status.String())

	var server stores.MCPServerSpec
	bindQuery(t, &server, "status=connected")
	assert.Equal(t, "connected", server.Status.String())

	var skill stores.SkillSpec
	bindQuery(t, &skill, "visible=1&name=demo")
	assert.True(t, skill.VisibleOnly)
	assert.Equal(t, "demo", skill.Name)
}

func TestQueryBinderLenientAndErrors(t *testing.T) {
	var spec stores.CobDocumentSpec
	bindQuery(t, &spec, "sort=&limit=&unknown=1")
	assert.Equal(t, 0, spec.Limit)
	assert.Empty(t, spec.Sort)

	u, err := url.Parse("/api/m/?page=abc")
	require.NoError(t, err)
	assert.Error(t, queryBinder.Bind(&spec, u))
}

// onlyText 只实现 encoding.TextUnmarshaler
type onlyText struct {
	value string
}

func (z *onlyText) UnmarshalText(b []byte) error {
	z.value = string(b)
	return nil
}

type decoderSpec struct {
	stores.PageSpec

	// andvari 的 field.OperateType 只实现 Decode(string) error
	Operate field.OperateType `form:"operate"`
	Kind    onlyText          `form:"kind"`
}

func TestQueryBinderTextInterfaces(t *testing.T) {
	var spec decoderSpec
	bindQuery(t, &spec, "limit=5&operate=create&kind=whatever")
	assert.Equal(t, 5, spec.Limit)
	assert.Equal(t, field.OperateTypeCreate, spec.Operate)
	assert.Equal(t, "whatever", spec.Kind.value)

	u, err := url.Parse("/api/m/?operate=nope")
	require.NoError(t, err)
	assert.Error(t, queryBinder.Bind(&spec, u))
}

type fakeDocStore struct {
	stores.CorpuStore
	got  *stores.CobDocumentSpec
	docs corpus.Documents
}

func (f *fakeDocStore) ListDocument(_ context.Context, spec *stores.CobDocumentSpec) (corpus.Documents, int, error) {
	f.got = spec
	return f.docs, 0, nil // 无 limit 时底层不返回总数，由 handler 兜底
}

type docFakeStorage struct {
	importFakeStorage
	docs *fakeDocStore
}

func (f *docFakeStorage) Corpus() stores.CorpuStore { return f.docs }

func TestGetCorpusDocumentsBindsPaging(t *testing.T) {
	docs := &fakeDocStore{docs: corpus.Documents{{}, {}}}
	a := &api{sto: &docFakeStorage{docs: docs}}

	req := httptest.NewRequest(http.MethodGet, "/api/m/corpus/documents?page=3&limit=20&sort=-updated&heading=guide", nil)
	rr := httptest.NewRecorder()
	a.getCorpusDocuments(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NotNil(t, docs.got)
	assert.Equal(t, 3, docs.got.Page)
	assert.Equal(t, 20, docs.got.Limit)
	assert.Equal(t, 40, docs.got.GetSkip())
	assert.Equal(t, "-updated", docs.got.Sort)
	assert.Equal(t, "guide", docs.got.Heading)

	var body struct {
		Result struct {
			Data  json.RawMessage `json:"data"`
			Total int             `json:"total"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, len(docs.docs), body.Result.Total)
}
