package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// InteractionLog LLM 交互日志
type InteractionLog struct {
	Timestamp  string           `json:"timestamp"`
	Provider   string           `json:"provider"`
	Model      string           `json:"model"`
	Messages   []Message        `json:"messages"`
	Tools      []ToolDefinition `json:"tools"`
	Usage      *Usage           `json:"usage,omitempty"`
	Think      string           `json:"think,omitempty"`
	Response   string           `json:"response"`
	ToolCalls  []ToolCall       `json:"tool_calls,omitempty"`
	StopReason string           `json:"stop_reason,omitempty"`
	Error      string           `json:"error,omitempty"`
}

var auditMu sync.Mutex

// LogInteraction 写入交互日志
func LogInteraction(logDir, provider string, log *InteractionLog) {
	if logDir == "" {
		return
	}

	log.Timestamp = time.Now().UTC().Format(time.RFC3339)
	log.Provider = provider

	filename := filepath.Join(logDir, provider+"_"+time.Now().Format("2006-01-02_15")+".jsonl")

	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		logger().Warnw("create log dir failed", "path", filename, "err", err)
		return
	}

	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logger().Warnw("open log file failed", "path", filename, "err", err)
		return
	}

	auditMu.Lock()
	data, err := json.Marshal(log)
	if err != nil {
		auditMu.Unlock()
		_ = f.Close()
		logger().Infow("marshal log failed", "err", err)
		return
	}

	if _, err := f.Write(append(data, '\n')); err != nil {
		logger().Infow("write log failed", "err", err)
	}
	auditMu.Unlock()
	_ = f.Close()
}
