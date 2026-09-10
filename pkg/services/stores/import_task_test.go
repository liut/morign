//go:build integration

package stores

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/liut/morign/pkg/models/corpus"
	"github.com/liut/morign/pkg/services/llm"
)

var importTaskSeq int

func importTaskTitle(prefix string) string {
	importTaskSeq++
	return fmt.Sprintf("it-%s-%d-%d", prefix, os.Getpid(), importTaskSeq)
}

// runImportTask 创建 pending 任务并走完 claim → process → 回读
func runImportTask(t *testing.T, data string) *corpus.ImportTask {
	t.Helper()
	ctx := context.Background()
	sto := Sgt().Corpus()

	obj, err := sto.CreateImportTask(ctx, corpus.ImportTaskBasic{
		Filename: "test.csv",
		Status:   corpus.ImportTaskStatusPending,
		Data:     data,
	})
	require.NoError(t, err)

	claimed, err := sto.ClaimImportTask(ctx)
	require.NoError(t, err)
	require.Equal(t, obj.StringID(), claimed.StringID())
	assert.Equal(t, corpus.ImportTaskStatusProcessing, claimed.Status)
	assert.NotNil(t, claimed.StartedAt)

	require.NoError(t, sto.ProcessImportTask(ctx, claimed))

	got, err := sto.GetImportTask(ctx, obj.StringID())
	require.NoError(t, err)
	return got
}

func assertImportCounts(t *testing.T, task *corpus.ImportTask, total, success, failed, skipped int) {
	t.Helper()
	assert.Equal(t, total, task.Total)
	assert.Equal(t, success, task.Success)
	assert.Equal(t, failed, task.Failed)
	assert.Equal(t, skipped, task.Skipped)
	assert.Equal(t, total, success+failed+skipped, "Total must equal Success+Failed+Skipped")
}

func getDocument(t *testing.T, title, heading string) *corpus.Document {
	t.Helper()
	doc := new(corpus.Document)
	err := dbGet(context.Background(), Sgt().db, doc, "title = ? AND heading = ?", title, heading)
	require.NoError(t, err)
	return doc
}

func TestIntegration_ImportTaskSuccess(t *testing.T) {
	title := importTaskTitle("ok")
	data := "title,heading,content\n" +
		title + ",a,hello a\n" +
		title + ",b,hello b\n" +
		title + ",c,hello c\n"

	got := runImportTask(t, data)

	assert.Equal(t, corpus.ImportTaskStatusSucceeded, got.Status)
	assertImportCounts(t, got, 3, 3, 0, 0)
	assert.NotNil(t, got.StartedAt)
	assert.NotNil(t, got.FinishedAt)
	assert.Empty(t, got.Errors)

	for _, h := range []string{"a", "b", "c"} {
		doc := getDocument(t, title, h)
		dv := new(corpus.DocVector)
		err := dbGetWithUnique(context.Background(), Sgt().db, dv, "doc_id", doc.ID)
		assert.NoError(t, err, "vector missing for %s", h)
	}
}

func TestIntegration_ImportTaskEmptyAndHeaderOnly(t *testing.T) {
	for name, data := range map[string]string{
		"empty":       "",
		"header_only": "title,heading,content\n",
	} {
		t.Run(name, func(t *testing.T) {
			got := runImportTask(t, data)
			assert.Equal(t, corpus.ImportTaskStatusSucceeded, got.Status)
			assertImportCounts(t, got, 0, 0, 0, 0)
		})
	}
}

func TestIntegration_ImportTaskDuplicateInFile(t *testing.T) {
	title := importTaskTitle("dup")
	data := "title,heading,content\n" +
		title + ",h,first\n" +
		title + ",h,second\n"

	got := runImportTask(t, data)

	assert.Equal(t, corpus.ImportTaskStatusSucceeded, got.Status)
	assertImportCounts(t, got, 2, 1, 0, 1)
	require.Len(t, got.Errors, 1)
	assert.Equal(t, 3, got.Errors[0].Line)
	assert.Contains(t, got.Errors[0].Reason, "重复")

	doc := getDocument(t, title, "h")
	assert.Equal(t, "first", doc.Content, "duplicate row must not overwrite existing document")
}

func TestIntegration_ImportTaskInvalidRows(t *testing.T) {
	title := importTaskTitle("bad")
	data := "title,heading,content\n" +
		title + ",ok,hello\n" +
		",,\n" +
		"short\n"

	got := runImportTask(t, data)

	assert.Equal(t, corpus.ImportTaskStatusSucceeded, got.Status)
	assertImportCounts(t, got, 3, 1, 2, 0)
	require.Len(t, got.Errors, 2)
	assert.Equal(t, "数据无效", got.Errors[0].Reason)
	assert.Equal(t, "数据无效", got.Errors[1].Reason)
	assert.Equal(t, 3, got.Errors[0].Line)
	assert.Equal(t, 4, got.Errors[1].Line)
}

// failEmbeddingClient 对包含指定文本的 embedding 请求返回错误
type failEmbeddingClient struct {
	llm.Client
	failFor string
}

func (m *failEmbeddingClient) Embedding(ctx context.Context, texts []string) ([]float64, error) {
	for _, text := range texts {
		if strings.Contains(text, m.failFor) {
			return nil, errors.New(m.failFor)
		}
	}
	return m.Client.Embedding(ctx, texts)
}

func TestIntegration_ImportTaskEmbeddingFailure(t *testing.T) {
	old := llmEm
	defer func() { llmEm = old }()
	bad := "emb-boom-heading"
	llmEm = &failEmbeddingClient{Client: old, failFor: bad}

	title := importTaskTitle("emb")
	data := "title,heading,content\n" +
		title + ",good1,hello\n" +
		title + "," + bad + ",hello\n" +
		title + ",good2,hello\n"

	got := runImportTask(t, data)

	assert.Equal(t, corpus.ImportTaskStatusSucceeded, got.Status)
	assertImportCounts(t, got, 3, 2, 1, 0)
	require.Len(t, got.Errors, 1)
	assert.Equal(t, 3, got.Errors[0].Line)
	assert.Contains(t, got.Errors[0].Reason, bad)
}

func TestIntegration_ImportTaskMalformedCSV(t *testing.T) {
	data := "title,heading,content\n" +
		"ok,h,\"unclosed\n"

	got := runImportTask(t, data)

	assert.Equal(t, corpus.ImportTaskStatusFailed, got.Status)
	assertImportCounts(t, got, 0, 0, 0, 0)
	assert.NotNil(t, got.FinishedAt)
}

func TestIntegration_ImportTaskErrorCap(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("title,heading,content\n")
	for i := 0; i < 510; i++ {
		sb.WriteString(",,invalid-row\n")
	}

	got := runImportTask(t, sb.String())

	assert.Equal(t, corpus.ImportTaskStatusSucceeded, got.Status)
	assertImportCounts(t, got, 510, 0, 510, 0)
	require.Len(t, got.Errors, maxImportErrors)
}

func TestIntegration_ImportTaskReasonTruncation(t *testing.T) {
	old := llmEm
	defer func() { llmEm = old }()
	bad := strings.Repeat("x", 300)
	llmEm = &failEmbeddingClient{Client: old, failFor: bad}

	title := importTaskTitle("trunc")
	data := "title,heading,content\n" +
		title + "," + bad + ",hello\n"

	got := runImportTask(t, data)

	assert.Equal(t, corpus.ImportTaskStatusSucceeded, got.Status)
	assertImportCounts(t, got, 1, 0, 1, 0)
	require.Len(t, got.Errors, 1)
	assert.Len(t, []rune(got.Errors[0].Reason), maxImportReasonLen)
}

func TestIntegration_ImportTaskSkipExisting(t *testing.T) {
	ctx := context.Background()
	title := importTaskTitle("exist")

	_, err := Sgt().Corpus().CreateDocument(ctx, corpus.DocumentBasic{
		Title:   title,
		Heading: "h",
		Content: "original",
	})
	require.NoError(t, err)

	data := "title,heading,content\n" +
		title + ",h,updated\n"
	got := runImportTask(t, data)

	assert.Equal(t, corpus.ImportTaskStatusSucceeded, got.Status)
	assertImportCounts(t, got, 1, 0, 0, 1)
	require.Len(t, got.Errors, 1)
	assert.Contains(t, got.Errors[0].Reason, "重复")

	doc := getDocument(t, title, "h")
	assert.Equal(t, "original", doc.Content, "existing document must not be overwritten")
}

func TestIntegration_ImportTaskRecover(t *testing.T) {
	ctx := context.Background()
	sto := Sgt().Corpus()

	o1, err := sto.CreateImportTask(ctx, corpus.ImportTaskBasic{
		Filename: "a.csv",
		Status:   corpus.ImportTaskStatusPending,
		Data:     "title,heading,content\n",
	})
	require.NoError(t, err)
	o2, err := sto.CreateImportTask(ctx, corpus.ImportTaskBasic{
		Filename: "b.csv",
		Status:   corpus.ImportTaskStatusPending,
		Data:     "title,heading,content\n",
	})
	require.NoError(t, err)
	defer func() {
		_ = sto.DeleteImportTask(ctx, o1.StringID())
		_ = sto.DeleteImportTask(ctx, o2.StringID())
	}()

	claimed, err := sto.ClaimImportTask(ctx)
	require.NoError(t, err)
	require.Equal(t, o1.StringID(), claimed.StringID())

	n, err := sto.RecoverImportTasks(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	got1, err := sto.GetImportTask(ctx, o1.StringID())
	require.NoError(t, err)
	got2, err := sto.GetImportTask(ctx, o2.StringID())
	require.NoError(t, err)
	assert.Equal(t, corpus.ImportTaskStatusPending, got1.Status)
	assert.Equal(t, corpus.ImportTaskStatusPending, got2.Status)

	reclaimed, err := sto.ClaimImportTask(ctx)
	require.NoError(t, err)
	assert.Equal(t, o1.StringID(), reclaimed.StringID())
}
