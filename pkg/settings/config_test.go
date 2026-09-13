package settings

import (
	"os"
	"testing"

	"github.com/kelseyhightower/envconfig"
)

// processConfig 在补齐必需变量后解析一份独立配置，避免污染 Current
func processConfig(t *testing.T) *Config {
	t.Helper()
	t.Setenv("MORIGN_HTTP_LISTEN", ":0")
	t.Setenv("MORIGN_REDIS_URI", "redis://localhost:6379/1")
	for _, name := range []string{"INTERACT", "EMBEDDING", "SUMMARIZE", "RERANK"} {
		t.Setenv("MORIGN_"+name+"_MODEL", "test-model")
	}

	cfg := new(Config)
	if err := envconfig.Process(Name, cfg); err != nil {
		t.Fatalf("envconfig.Process: %v", err)
	}
	return cfg
}

func TestToolResultMaxCharsDefault(t *testing.T) {
	t.Setenv("MORIGN_TOOL_RESULT_MAX_CHARS", "")
	if err := os.Unsetenv("MORIGN_TOOL_RESULT_MAX_CHARS"); err != nil {
		t.Fatal(err)
	}

	if got := processConfig(t).ToolResultMaxChars; got != 20000 {
		t.Errorf("ToolResultMaxChars = %d, want 20000", got)
	}
}

func TestToolResultMaxCharsFromEnv(t *testing.T) {
	t.Setenv("MORIGN_TOOL_RESULT_MAX_CHARS", "1234")

	if got := processConfig(t).ToolResultMaxChars; got != 1234 {
		t.Errorf("ToolResultMaxChars = %d, want 1234", got)
	}
}
