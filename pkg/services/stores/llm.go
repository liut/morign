package stores

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/liut/morign/pkg/models/aigc"
	"github.com/liut/morign/pkg/services/llm"
	"github.com/liut/morign/pkg/settings"
	"github.com/liut/morign/pkg/utils/words"
)

const (
	KeywordTpl = "Summarize and extract key phrases; for questions, ignore interrogative forms and return only keywords, space-separated, single line output.:\n\n%s\n\nsummary:\n"

	TitleTpl = "Generate a concise title (no more than 10 words) based on the following chat history. The title should reflect only the chat topic. Return the title only, nothing else.%s\n\n%s\n\ntitle:"
)

var (
	// 新的 LLM Clients - 按用途分离
	llmEm llm.Client // for Embedding
	llmSu llm.Client // for Summarize/Completion
	llmRe llm.Client // for Rerank

	llmOnce sync.Once
)

func initLLMClients() {
	initLLMClient("Embedding", &settings.Current.Embedding, &llmEm)
	initLLMClient("Summarize", &settings.Current.Summarize, &llmSu)
	initRerankClient()
}

func initRerankClient() {
	p := settings.Current.Rerank
	if p.APIKey == "" && p.URL == "" {
		return
	}
	var err error
	llmRe, err = llm.NewClient(
		llm.WithProvider(p.Type),
		llm.WithAPIKey(p.APIKey),
		llm.WithBaseURL(p.URL),
		llm.WithModel(p.Model),
		llm.WithDebug(p.Debug),
		llm.WithLogDir(p.LogDir),
		llm.WithTemperature(0), // 重排需要确定性输出
	)
	if err != nil {
		logger().Error("create rerank llm client failed", "err", err)
		os.Exit(1)
	}
}

func initLLMClient(name string, p *settings.Provider, target *llm.Client) {
	if p.APIKey == "" && p.URL == "" {
		logger().Error("provider config invalid: API_KEY and URL cannot both be empty", "provider", name)
		return
	}

	var err error
	*target, err = NewLLMClient(p)
	if err != nil {
		logger().Error("create llm client failed", "provider", name, "err", err)
		os.Exit(1)
	}
}

func NewLLMClient(p *settings.Provider) (llm.Client, error) {
	opts := []llm.Option{
		llm.WithProvider(p.Type),
		llm.WithAPIKey(p.APIKey),
		llm.WithBaseURL(p.URL),
		llm.WithModel(p.Model),
		llm.WithDebug(p.Debug),
		llm.WithLogDir(p.LogDir),
	}
	if p.Temperature > 0 {
		opts = append(opts, llm.WithTemperature(p.Temperature))
	}
	if p.TimeoutSeconds > 0 {
		opts = append(opts, llm.WithTimeout(time.Duration(p.TimeoutSeconds)*time.Second))
	}
	return llm.NewClient(opts...)
}

// GetLLMEmbeddingClient 获取 Embedding 用 LLM Client
func GetLLMEmbeddingClient() llm.Client {
	llmOnce.Do(initLLMClients)
	return llmEm
}

// GetLLMSummarizeClient 获取 Summarize/Completion 用 LLM Client
func GetLLMSummarizeClient() llm.Client {
	llmOnce.Do(initLLMClients)
	return llmSu
}

// GetLLMRerankClient 获取 Rerank 用 LLM Client（温度固定为 0 确保确定性输出）
func GetLLMRerankClient() llm.Client {
	llmOnce.Do(initLLMClients)
	return llmRe
}

// GetSummary 让LLM根据模版要求生成摘要
// text tpl 参数为自定义提示内容模版
func GetSummary(ctx context.Context, text, tpl string) (summary string, err error) {
	if len(text) == 0 {
		err = ErrEmptyParam
		return
	}

	prompt := fmt.Sprintf(tpl, text)
	result, _, err := GetLLMSummarizeClient().Generate(ctx, prompt)
	if err != nil {
		logger().Info("summarize fail", "tpl", tpl, "text", text, "err", err)
		return
	}
	if _, b, ok := strings.Cut(result, "</think>"); ok {
		result = b
	}
	summary = strings.TrimSpace(result)
	logger().Info("summarize ok", "tpl", tpl, "text", words.TakeHead(text, 90, ".."),
		"result", words.TakeHead(summary, 50, ".."))
	return
}

func GetHistorySummary(ctx context.Context, history aigc.HistoryItems) (summary string, err error) {
	summary, err = GetSummary(ctx, history.ToText(), GetTemplateForTitle())
	if err != nil {
		return
	}
	logger().Info("history summary ok", "history", aigc.HiLogged(history))
	return
}

func GetTemplateForKeyword() string {
	preset, _ := LoadPreset()
	if len(preset.KeywordTpl) > 0 {
		return preset.KeywordTpl
	}
	return KeywordTpl
}

func GetTemplateForTitle() string {
	preset, _ := LoadPreset()
	if len(preset.TitleTpl) > 0 {
		return preset.TitleTpl
	}
	return TitleTpl
}
