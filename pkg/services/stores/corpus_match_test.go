package stores

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strconv"
	"testing"

	"github.com/cupogo/andvari/models/oid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/liut/morign/pkg/models/corpus"
)

type probeConnector struct{}

func (probeConnector) Connect(context.Context) (driver.Conn, error) { return probeConn{}, nil }
func (probeConnector) Driver() driver.Driver                        { return probeDriver{} }

type probeDriver struct{}

func (probeDriver) Open(string) (driver.Conn, error) { return probeConn{}, nil }

type probeConn struct{}

func (probeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("no db") }
func (probeConn) Close() error                        { return nil }
func (probeConn) Begin() (driver.Tx, error)           { return nil, errors.New("no db") }

// probeQuery 只用于观察生成的 SQL，不会真正连接数据库
func probeQuery() *bun.SelectQuery {
	db := bun.NewDB(sql.OpenDB(probeConnector{}), pgdialect.New())
	return db.NewSelect().Model(&corpus.Document{})
}

func TestMatchCandidates(t *testing.T) {
	cases := []struct {
		page, limit, skip int
		want              int
	}{
		{0, 0, 0, defaultMatchCandidates},
		{1, 20, 0, defaultMatchCandidates},
		{2, 20, 0, defaultMatchCandidates},
		{2, 40, 0, 80},
		{0, 30, 30, 60},
	}
	for _, c := range cases {
		spec := &CobDocumentSpec{}
		spec.Page, spec.Limit, spec.Skip = c.page, c.limit, c.skip
		assert.Equal(t, c.want, spec.matchCandidates(), "page=%d limit=%d skip=%d", c.page, c.limit, c.skip)
	}
}

func TestSiftXWithoutMatch(t *testing.T) {
	spec := &CobDocumentSpec{}
	q := spec.SiftX(context.Background(), probeQuery())
	assert.NotContains(t, q.String(), "WHERE")
}

func TestSiftXMatchTooShort(t *testing.T) {
	for _, match := range []string{"a", " ", "  a  ", "·"} {
		spec := &CobDocumentSpec{Match: match}
		q := spec.SiftX(context.Background(), probeQuery())
		assert.Contains(t, q.String(), "FALSE", "match=%q", match)
	}
}

func TestMatchKeyword(t *testing.T) {
	cases := []struct {
		match string
		want  string
		ok    bool
	}{
		{"", "", false},
		{"a", "a", false},
		{" a ", "a", false},
		{"·", "·", false},
		{"  ", "", false},
		{"ab", "ab", true},
		{"a b", "ab", true},
		{"华封 科技", "华封科技", true},
	}
	for _, c := range cases {
		got, ok := matchKeyword(c.match)
		assert.Equal(t, c.want, got, "match=%q", c.match)
		assert.Equal(t, c.ok, ok, "match=%q", c.match)
	}
}

func TestSiftMatchIDs(t *testing.T) {
	ids := oid.OIDs{oid.NewID(oid.OtArticle), oid.NewID(oid.OtArticle)}
	sql := siftMatchIDs(probeQuery(), ids).String()

	require.Contains(t, sql, `"cd"."id" in`)
	assert.Contains(t, sql, strconv.FormatInt(int64(ids[0]), 10))
	assert.Contains(t, sql, strconv.FormatInt(int64(ids[1]), 10))

	assert.Contains(t, siftMatchIDs(probeQuery(), nil).String(), "FALSE")
}
