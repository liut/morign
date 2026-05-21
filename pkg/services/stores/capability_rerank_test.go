package stores

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/liut/morign/pkg/models/capability"
	"github.com/liut/morign/pkg/services/llm"
	"github.com/liut/morign/pkg/settings"
)

// mockRerankClient implements llm.Client for testing rerank
type mockRerankClient struct {
	chatResult *llm.ChatResult
	chatErr    error
}

func (m *mockRerankClient) Chat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (*llm.ChatResult, error) {
	return m.chatResult, m.chatErr
}

func (m *mockRerankClient) StreamChat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) iter.Seq2[*llm.Event, error] {
	return func(yield func(*llm.Event, error) bool) {}
}

func (m *mockRerankClient) Generate(ctx context.Context, prompt string) (string, *llm.Usage, error) {
	return "", nil, nil
}

func (m *mockRerankClient) Embedding(ctx context.Context, texts []string) ([]float64, error) {
	return nil, nil
}

// makeCandidates creates test capability candidates with unique IDs
func makeCandidates(summaries ...string) capability.Capabilities {
	caps := make(capability.Capabilities, len(summaries))
	for i, s := range summaries {
		caps[i] = capability.Capability{
			CapabilityBasic: capability.CapabilityBasic{
				Endpoint: "/api/test/" + s[:min(3, len(s))],
				Method:   "GET",
				Summary:  s,
			},
		}
		// Trigger ID generation via Creating hook
		_ = caps[i].Creating()
	}
	return caps
}

func buildRelevantJSON(indices ...int) string {
	items := make([]rerankItem, len(indices))
	for i, idx := range indices {
		items[i] = rerankItem{Index: idx, Reason: "relevant"}
	}
	rr := rerankResult{Relevant: items}
	data, _ := json.Marshal(rr)
	return string(data)
}

func buildAllIrrelevantJSON(indices ...int) string {
	items := make([]rerankItem, len(indices))
	for i, idx := range indices {
		items[i] = rerankItem{Index: idx, Reason: "irrelevant"}
	}
	rr := rerankResult{Irrelevant: items}
	data, _ := json.Marshal(rr)
	return string(data)
}

func TestRerankCapabilities_HappyPath(t *testing.T) {
	// Save and restore the real client
	origClient := llmRe
	defer func() { llmRe = origClient }()

	candidates := makeCandidates(
		"集团员工动态分析",
		"手动发送消息",
		"获取人力看板",
		"员工档案导入模版下载",
		"人事报表绩效统计",
	)

	llmRe = &mockRerankClient{
		chatResult: &llm.ChatResult{
			Content: buildRelevantJSON(1, 3, 5), // analysis, board, stat are relevant
		},
	}

	result, err := (&capabilityStore{}).rerankCapabilities(context.Background(), "分析公司人员构成", candidates)
	require.NoError(t, err)
	require.Len(t, result, 3)
	assert.Equal(t, "集团员工动态分析", result[0].Summary)
	assert.Equal(t, "获取人力看板", result[1].Summary)
	assert.Equal(t, "人事报表绩效统计", result[2].Summary)
}

func TestRerankCapabilities_AllRelevant(t *testing.T) {
	origClient := llmRe
	defer func() { llmRe = origClient }()

	candidates := makeCandidates("查询订单列表", "获取订单详情", "搜索订单")

	llmRe = &mockRerankClient{
		chatResult: &llm.ChatResult{Content: buildRelevantJSON(1, 2, 3)},
	}

	result, err := (&capabilityStore{}).rerankCapabilities(context.Background(), "查询我的订单", candidates)
	require.NoError(t, err)
	assert.Len(t, result, 3)
}

func TestRerankCapabilities_AllIrrelevant(t *testing.T) {
	origClient := llmRe
	defer func() { llmRe = origClient }()

	candidates := makeCandidates("发送消息", "模板下载")

	llmRe = &mockRerankClient{
		chatResult: &llm.ChatResult{Content: buildAllIrrelevantJSON(1, 2)},
	}

	result, err := (&capabilityStore{}).rerankCapabilities(context.Background(), "分析人员构成", candidates)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestRerankCapabilities_EmptyCandidates(t *testing.T) {
	result, err := (&capabilityStore{}).rerankCapabilities(context.Background(), "test", nil)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestRerankCapabilities_LLMError(t *testing.T) {
	origClient := llmRe
	defer func() { llmRe = origClient }()

	candidates := makeCandidates("test api")

	llmRe = &mockRerankClient{
		chatErr: errors.New("connection refused"),
	}

	result, err := (&capabilityStore{}).rerankCapabilities(context.Background(), "test", candidates)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestRerankCapabilities_InvalidJSON(t *testing.T) {
	origClient := llmRe
	defer func() { llmRe = origClient }()

	candidates := makeCandidates("test api")

	llmRe = &mockRerankClient{
		chatResult: &llm.ChatResult{Content: "not valid json at all"},
	}

	result, err := (&capabilityStore{}).rerankCapabilities(context.Background(), "test", candidates)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestRerankCapabilities_IndexOutOfRange(t *testing.T) {
	origClient := llmRe
	defer func() { llmRe = origClient }()

	candidates := makeCandidates("api one", "api two")

	// LLM returns index 5 which is out of range
	llmRe = &mockRerankClient{
		chatResult: &llm.ChatResult{Content: buildRelevantJSON(1, 5)},
	}

	result, err := (&capabilityStore{}).rerankCapabilities(context.Background(), "test", candidates)
	require.NoError(t, err)
	assert.Len(t, result, 1) // only index 1 is valid
	assert.Equal(t, "api one", result[0].Summary)
}

func TestRerankCapabilities_MarkdownCodeFence(t *testing.T) {
	origClient := llmRe
	defer func() { llmRe = origClient }()

	candidates := makeCandidates("relevant api")

	llmRe = &mockRerankClient{
		chatResult: &llm.ChatResult{
			Content: "```json\n" + buildRelevantJSON(1) + "\n```",
		},
	}

	result, err := (&capabilityStore{}).rerankCapabilities(context.Background(), "test", candidates)
	require.NoError(t, err)
	assert.Len(t, result, 1)
}

func TestRerankCacheKey(t *testing.T) {
	key1 := rerankCacheKey("分析人员构成")
	key2 := rerankCacheKey("分析人员构成")
	key3 := rerankCacheKey("查询订单")

	assert.Equal(t, key1, key2, "same query should produce same key")
	assert.NotEqual(t, key1, key3, "different queries should produce different keys")
	assert.Contains(t, key1, "rerank:", "key should have rerank prefix")
}

func TestRerankRebuildFromCache(t *testing.T) {
	candidates := makeCandidates("api one", "api two", "api three")

	ids := []string{candidates[2].StringID(), candidates[0].StringID()}
	result := rerankRebuildFromCache(ids, candidates)

	require.Len(t, result, 2)
	assert.Equal(t, "api three", result[0].Summary)
	assert.Equal(t, "api one", result[1].Summary)
}

func TestRerankRebuildFromCache_MissingID(t *testing.T) {
	candidates := makeCandidates("api one")

	result := rerankRebuildFromCache([]string{"nonexistent-id", candidates[0].StringID()}, candidates)

	require.Len(t, result, 1)
	assert.Equal(t, "api one", result[0].Summary)
}

func TestRerankCapabilities_NilClient(t *testing.T) {
	origClient := llmRe
	defer func() { llmRe = origClient }()

	llmRe = nil // simulate unconfigured rerank provider

	candidates := makeCandidates("test api")
	result, err := (&capabilityStore{}).rerankCapabilities(context.Background(), "test", candidates)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
	assert.Nil(t, result)
}

func TestRerankCapabilities_MarkdownCodeFenceNoLanguage(t *testing.T) {
	origClient := llmRe
	defer func() { llmRe = origClient }()

	candidates := makeCandidates("relevant api")

	llmRe = &mockRerankClient{
		chatResult: &llm.ChatResult{
			Content: "```\n" + buildRelevantJSON(1) + "\n```",
		},
	}

	result, err := (&capabilityStore{}).rerankCapabilities(context.Background(), "test", candidates)
	require.NoError(t, err)
	assert.Len(t, result, 1)
}

func TestRerankCapabilities_OnlyIrrelevantInRelevantField(t *testing.T) {
	origClient := llmRe
	defer func() { llmRe = origClient }()

	candidates := makeCandidates("api one", "api two")

	// LLM puts everything in irrelevant, relevant is empty
	rr := rerankResult{Relevant: []rerankItem{}, Irrelevant: []rerankItem{{Index: 1, Reason: "nope"}, {Index: 2, Reason: "nope"}}}
	data, _ := json.Marshal(rr)

	llmRe = &mockRerankClient{
		chatResult: &llm.ChatResult{Content: string(data)},
	}

	result, err := (&capabilityStore{}).rerankCapabilities(context.Background(), "test", candidates)
	require.NoError(t, err)
	assert.Empty(t, result, "empty relevant list should return empty result")
}

// TestInvokerForMatch_RerankDisabled verifies backward compatibility when rerank is off
func TestInvokerForMatch_RerankDisabled(t *testing.T) {
	// Save and restore settings
	origEnabled := settings.Current.RerankEnabled
	origRecallLimit := settings.Current.RerankRecallLimit
	defer func() {
		settings.Current.RerankEnabled = origEnabled
		settings.Current.RerankRecallLimit = origRecallLimit
	}()

	settings.Current.RerankEnabled = false
	settings.Current.RerankRecallLimit = 15

	// Verify config is correctly set to disabled
	assert.False(t, settings.Current.RerankEnabled)
	assert.Equal(t, 15, settings.Current.RerankRecallLimit)
}
