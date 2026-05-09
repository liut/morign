//go:build integration

// Integration Tests for stores package
//
// Prerequisites (本地测试前必须执行):
//
//  1. 创建测试数据库:
//     psql -U morign -c "CREATE DATABASE morign_test;"
//
//  2. 安装 vector 扩展（需要管理员权限）:
//     psql -U postgres -d morign_test -c "CREATE EXTENSION IF NOT EXISTS vector;"
//     psql -U postgres -d morign_test -c "ALTER EXTENSION vector SET SCHEMA public;"
//
//  3. 运行集成测试:
//     go test -tags=integration -v ./pkg/services/stores/...
//
// 环境变量:
//
//	M_TEST_DB_DSN      - 完整连接字符串 (优先级最高)
//	M_TEST_DB_HOST     - 数据库主机 (默认 localhost)
//	M_TEST_DB_PORT     - 数据库端口 (默认 5432)
//	M_TEST_DB_USER     - 数据库用户 (默认 morign)
//	M_TEST_DB_PASSWORD - 数据库密码
//	M_TEST_DB_NAME     - 数据库名 (默认 morign_test)
package stores

import (
	"context"
	"fmt"
	"iter"
	"math/rand"
	"os"
	"testing"

	"github.com/cupogo/andvari/models/oid"
	"github.com/liut/morign/pkg/models/convo"
	"github.com/liut/morign/pkg/models/corpus"
	"github.com/liut/morign/pkg/services/llm"
	"github.com/liut/morign/pkg/settings"
)

// mockEmbeddingClient is a mock implementation of llm.Client for testing
type mockEmbeddingClient struct{}

func (m *mockEmbeddingClient) Chat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (*llm.ChatResult, error) {
	return nil, nil
}

func (m *mockEmbeddingClient) StreamChat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) iter.Seq2[*llm.Event, error] {
	return func(yield func(*llm.Event, error) bool) {}
}

func (m *mockEmbeddingClient) Generate(ctx context.Context, prompt string) (string, *llm.Usage, error) {
	return "", nil, nil
}

func (m *mockEmbeddingClient) Embedding(ctx context.Context, texts []string) ([]float64, error) {
	// Return random vectors (same dimension as corpus.VectorLen)
	dim := corpus.VectorLen
	result := make([]float64, len(texts)*dim)
	for i := range result {
		result[i] = float64(rand.Float32())
	}
	return result, nil
}

// getTestDBDSN 构建测试数据库连接字符串
func getTestDBDSN() string {
	dsn := os.Getenv("M_TEST_DB_DSN")
	if dsn != "" {
		return dsn
	}

	host := getEnvOrDefault("M_TEST_DB_HOST", "localhost")
	port := getEnvOrDefault("M_TEST_DB_PORT", "5432")
	user := getEnvOrDefault("M_TEST_DB_USER", "morign")
	password := getEnvOrDefault("M_TEST_DB_PASSWORD", "")
	dbname := getEnvOrDefault("M_TEST_DB_NAME", "morign_test")

	if password != "" {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, password, host, port, dbname)
	}
	return fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=disable", user, host, port, dbname)
}

// getEnvOrDefault 获取环境变量或默认值
func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func init() {
	m := &mockEmbeddingClient{}
	llmEm = m
	llmSu = m
}

// TestMain 测试入口
func TestMain(m *testing.M) {
	// 替换数据库名为测试库
	dsn := getTestDBDSN()
	settings.Current.PgStoreDSN = dsn
	fmt.Printf("Using test DSN: %s\n", dsn)

	// 初始化数据库（InitSchemas + RunMigrations）
	if err := InitDB(context.Background()); err != nil {
		fmt.Printf("warning: InitDB failed: %v\n", err)
	}

	ret := m.Run()
	os.Exit(ret)
}

// 测试标题，用于避免测试数据冲突
func testDocTitle() string {
	return fmt.Sprintf("test-doc-%d", os.Getpid())
}

func TestIntegration_DocumentCRUD(t *testing.T) {
	// 检查是否有 Embedding API_KEY（CreateDocument 需要 embedding）
	if settings.Current.Embedding.APIKey == "" {
		t.Skip("Embedding.APIKey not set, skipping document CRUD test (requires embedding)")
	}

	sto := Sgt()
	ctx := context.Background()

	title := testDocTitle()

	// Create
	doc, err := sto.Corpus().CreateDocument(ctx, corpus.DocumentBasic{
		Title:   title,
		Heading: "Test Heading",
		Content: "Test Content",
	})
	if err != nil {
		t.Fatalf("CreateDocument failed: %v", err)
	}
	if doc == nil {
		t.Fatal("CreateDocument returned nil")
	}

	// Get
	found, err := sto.Corpus().GetDocument(ctx, doc.ID.String())
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	if found == nil {
		t.Fatal("GetDocument returned nil")
	}

	// Delete
	if err := sto.Corpus().DeleteDocument(ctx, doc.ID.String()); err != nil {
		t.Logf("cleanup failed: %v", err)
	}
}

func TestIntegration_ListDocuments(t *testing.T) {
	sto := Sgt()
	ctx := context.Background()

	spec := &CobDocumentSpec{}
	data, total, err := sto.Corpus().ListDocument(ctx, spec)
	if err != nil {
		t.Fatalf("ListDocument failed: %v", err)
	}

	t.Logf("Total documents: %d, returned: %d", total, len(data))
}

func TestIntegration_DocVectorCRUD(t *testing.T) {
	// 检查是否有 Embedding API_KEY
	if settings.Current.Embedding.APIKey == "" {
		t.Skip("Embedding.APIKey not set, skipping vector test (requires embedding)")
	}

	sto := Sgt()
	ctx := context.Background()

	docTitle := testDocTitle()
	doc, err := sto.Corpus().CreateDocument(ctx, corpus.DocumentBasic{
		Title:   docTitle,
		Heading: "Vector Test",
		Content: "Content for vector test",
	})
	if err != nil {
		t.Fatalf("CreateDocument failed: %v", err)
	}
	docID := doc.ID.String()

	// 清理
	defer sto.Corpus().DeleteDocument(ctx, docID)

	// Create DocVector
	vec, err := sto.Corpus().CreateDocVector(ctx, corpus.DocVectorBasic{
		DocID:   doc.ID,
		Subject: "test-subject",
		Vector:  make(corpus.Vector, 1024),
	})
	if err != nil {
		t.Fatalf("CreateDocVector failed: %v", err)
	}
	if vec == nil {
		t.Fatal("CreateDocVector returned nil")
	}

	// Get DocVector
	found, err := sto.Corpus().GetDocVector(ctx, vec.ID.String())
	if err != nil {
		t.Fatalf("GetDocVector failed: %v", err)
	}
	if found == nil {
		t.Fatal("GetDocVector returned nil")
	}

	// Delete DocVector
	err = sto.Corpus().DeleteDocVector(ctx, vec.ID.String())
	if err != nil {
		t.Fatalf("DeleteDocVector failed: %v", err)
	}
}

func TestIntegration_ChatLogCRUD(t *testing.T) {
	sto := Sgt()
	ctx := context.Background()

	// Create ChatLog
	chatID := oid.NewID(oid.OtEvent)
	log, err := sto.Corpus().CreateChatLog(ctx, corpus.ChatLogBasic{
		ChatID:   chatID,
		Question: "What is Go?",
		Answer:   "Go is a programming language.",
	})
	if err != nil {
		t.Fatalf("CreateChatLog failed: %v", err)
	}
	if log == nil {
		t.Fatal("CreateChatLog returned nil")
	}

	// Get ChatLog
	found, err := sto.Corpus().GetChatLog(ctx, log.ID.String())
	if err != nil {
		t.Fatalf("GetChatLog failed: %v", err)
	}
	if found == nil {
		t.Fatal("GetChatLog returned nil")
	}
	if found.Question != "What is Go?" {
		t.Errorf("expected question 'What is Go?', got %q", found.Question)
	}

	// Delete ChatLog
	err = sto.Corpus().DeleteChatLog(ctx, log.ID.String())
	if err != nil {
		t.Fatalf("DeleteChatLog failed: %v", err)
	}
}

func TestIntegration_MemoryCRUD(t *testing.T) {
	sto := Sgt()
	ctx := context.Background()

	// 使用测试用户 ID
	testOwnerID := oid.NewID(oid.OtEvent)
	testKey := fmt.Sprintf("test-memory-key-%d", os.Getpid())

	// Create Memory
	mem, err := sto.Convo().CreateMemory(ctx, convo.MemoryBasic{
		OwnerID: testOwnerID,
		Key:     testKey,
		Cate:    "core",
		Content: "Test memory content",
	})
	if err != nil {
		t.Fatalf("CreateMemory failed: %v", err)
	}
	if mem == nil {
		t.Fatal("CreateMemory returned nil")
	}

	// Get Memory
	found, err := sto.Convo().GetMemory(ctx, mem.ID.String())
	if err != nil {
		t.Fatalf("GetMemory failed: %v", err)
	}
	if found == nil {
		t.Fatal("GetMemory returned nil")
	}
	if found.Key != testKey {
		t.Errorf("expected key %q, got %q", testKey, found.Key)
	}
	if found.Content != "Test memory content" {
		t.Errorf("expected content 'Test memory content', got %q", found.Content)
	}

	// Update Memory
	newContent := "Updated memory content"
	err = sto.Convo().UpdateMemory(ctx, mem.ID.String(), convo.MemorySet{
		Content: &newContent,
	})
	if err != nil {
		t.Fatalf("UpdateMemory failed: %v", err)
	}

	// Verify update
	updated, err := sto.Convo().GetMemory(ctx, mem.ID.String())
	if err != nil {
		t.Fatalf("GetMemory after update failed: %v", err)
	}
	if updated.Content != newContent {
		t.Errorf("expected content %q, got %q", newContent, updated.Content)
	}

	// Delete Memory
	err = sto.Convo().DeleteMemory(ctx, mem.ID.String())
	if err != nil {
		t.Fatalf("DeleteMemory failed: %v", err)
	}

	// Verify deletion
	deleted, err := sto.Convo().GetMemory(ctx, mem.ID.String())
	if err == nil && deleted != nil {
		t.Error("Memory should have been deleted")
	}
}

func TestIntegration_ListMemories(t *testing.T) {
	sto := Sgt()
	ctx := context.Background()

	spec := &ConvoMemorySpec{}
	data, total, err := sto.Convo().ListMemory(ctx, spec)
	if err != nil {
		t.Fatalf("ListMemory failed: %v", err)
	}

	t.Logf("Total memories: %d, returned: %d", total, len(data))
}

func TestIntegration_HistoryStoreAppendEvent(t *testing.T) {
	sto := Sgt()
	ctx := context.Background()

	sessionID := oid.NewID(oid.OtEvent).String()
	hs := NewHistoryStore(sto)

	event := &llm.Event{
		Author:  "assistant",
		Delta: "Hello, this is a test response",
		Think:   "The user is testing",
	}

	if err := hs.AppendEvent(ctx, sessionID, event); err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}

	// Verify history was written
	cs := NewConversation(ctx, sessionID)
	history, err := cs.ListHistory(ctx)
	if err != nil {
		t.Fatalf("ListHistory failed: %v", err)
	}
	if len(history) == 0 {
		t.Fatal("expected at least one history item")
	}
	found := false
	for _, h := range history {
		if h.ChatItem != nil && h.ChatItem.Assistant == event.Delta {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("history item with content %q not found", event.Delta)
	}

	// Cleanup
	_ = cs.ClearHistory(ctx)
}

func TestIntegration_HistoryStoreCreateUsageRecord(t *testing.T) {
	sto := Sgt()
	ctx := context.Background()

	sessionID := oid.NewID(oid.OtEvent).String()
	hs := NewHistoryStore(sto)

	event := &llm.Event{
		Usage:    &llm.Usage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
		Model:    "test-model",
		MsgCount: 3,
	}

	if err := hs.CreateUsageRecord(ctx, sessionID, event); err != nil {
		t.Fatalf("CreateUsageRecord failed: %v", err)
	}

	spec := &ConvoUsageRecordSpec{}
	spec.Limit = 1
	data, total, err := sto.Convo().ListUsageRecord(ctx, spec)
	if err != nil {
		t.Fatalf("ListUsageRecord failed: %v", err)
	}
	t.Logf("Total usage records: %d", total)
	_ = data
}

func TestIntegration_SessionStoreMergeDelta(t *testing.T) {
	sto := Sgt()
	ctx := context.Background()

	sess := convo.NewSessionWithBasic(convo.SessionBasic{
		Title: "test-session-delta",
	})
	if err := sto.Convo().SaveSession(ctx, sess); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}
	sessionID := sess.StringID()

	ss := NewSessionStore(sto)
	delta := map[string]any{
		"last_tool": "kb_search",
		"count":     3,
	}

	if err := ss.MergeDelta(ctx, sessionID, delta); err != nil {
		t.Fatalf("MergeDelta failed: %v", err)
	}

	updated, err := sto.Convo().GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	_ = updated
	t.Logf("Session meta updated for %s", sessionID)
}
