package mcps

import (
	"encoding/json"
	"testing"
)

func TestToolNames_String(t *testing.T) {
	tests := []struct {
		name     string
		tools    []ToolDescriptor
		expected string
	}{
		{
			name:     "empty",
			tools:    []ToolDescriptor{},
			expected: "[]",
		},
		{
			name: "single tool",
			tools: []ToolDescriptor{
				{Name: "get_weather"},
			},
			expected: "[get_weather]",
		},
		{
			name: "multiple tools",
			tools: []ToolDescriptor{
				{Name: "get_weather"},
				{Name: "search"},
				{Name: "kb_search"},
			},
			expected: "[get_weather, search, kb_search]",
		},
		{
			name: "with description",
			tools: []ToolDescriptor{
				{Name: "get_weather", Description: "Get weather info"},
				{Name: "search", Description: "Search the web"},
			},
			expected: "[get_weather, search]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logged := ToolNames(tt.tools)
			result := logged.String()
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestStringArg(t *testing.T) {
	tests := []struct {
		name      string
		arguments map[string]any
		key       string
		expected  string
	}{
		{
			name:      "nil arguments",
			arguments: nil,
			key:       "name",
			expected:  "",
		},
		{
			name:      "key not found",
			arguments: map[string]any{"other": "value"},
			key:       "name",
			expected:  "",
		},
		{
			name:      "string value",
			arguments: map[string]any{"name": "test value"},
			key:       "name",
			expected:  "test value",
		},
		{
			name:      "int value",
			arguments: map[string]any{"name": 123},
			key:       "name",
			expected:  "123",
		},
		{
			name:      "with whitespace",
			arguments: map[string]any{"name": "  test  "},
			key:       "name",
			expected:  "test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StringArg(tt.arguments, tt.key)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestIntArg(t *testing.T) {
	tests := []struct {
		name      string
		arguments map[string]any
		key       string
		wantVal   int
		wantOk    bool
		wantErr   bool
	}{
		{
			name:      "nil arguments",
			arguments: nil,
			key:       "count",
			wantVal:   0,
			wantOk:    false,
			wantErr:   false,
		},
		{
			name:      "key not found",
			arguments: map[string]any{"other": 1},
			key:       "count",
			wantVal:   0,
			wantOk:    false,
			wantErr:   false,
		},
		{
			name:      "int value",
			arguments: map[string]any{"count": 42},
			key:       "count",
			wantVal:   42,
			wantOk:    true,
			wantErr:   false,
		},
		{
			name:      "float64 value",
			arguments: map[string]any{"count": 42.5},
			key:       "count",
			wantVal:   42,
			wantOk:    true,
			wantErr:   false,
		},
		{
			name:      "json number",
			arguments: map[string]any{"count": json.Number("100")},
			key:       "count",
			wantVal:   100,
			wantOk:    true,
			wantErr:   false,
		},
		{
			name:      "invalid string",
			arguments: map[string]any{"count": "abc"},
			key:       "count",
			wantOk:    true,
			wantErr:   true,
		},
		{
			name:      "nil value",
			arguments: map[string]any{"count": nil},
			key:       "count",
			wantVal:   0,
			wantOk:    false,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, ok, err := IntArg(tt.arguments, tt.key)
			if ok != tt.wantOk {
				t.Errorf("ok = %v, want %v", ok, tt.wantOk)
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if val != tt.wantVal {
				t.Errorf("val = %d, want %d", val, tt.wantVal)
			}
		})
	}
}

func TestBoolArg(t *testing.T) {
	tests := []struct {
		name      string
		arguments map[string]any
		key       string
		wantVal   bool
		wantOk    bool
		wantErr   bool
	}{
		{
			name:      "nil arguments",
			arguments: nil,
			key:       "enabled",
			wantOk:    false,
			wantErr:   false,
		},
		{
			name:      "key not found",
			arguments: map[string]any{"other": true},
			key:       "enabled",
			wantOk:    false,
			wantErr:   false,
		},
		{
			name:      "bool value true",
			arguments: map[string]any{"enabled": true},
			key:       "enabled",
			wantVal:   true,
			wantOk:    true,
			wantErr:   false,
		},
		{
			name:      "bool value false",
			arguments: map[string]any{"enabled": false},
			key:       "enabled",
			wantVal:   false,
			wantOk:    true,
			wantErr:   false,
		},
		{
			name:      "string value",
			arguments: map[string]any{"enabled": "true"},
			key:       "enabled",
			wantOk:    true,
			wantErr:   true,
		},
		{
			name:      "nil value",
			arguments: map[string]any{"enabled": nil},
			key:       "enabled",
			wantOk:    false,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, ok, err := BoolArg(tt.arguments, tt.key)
			if ok != tt.wantOk {
				t.Errorf("ok = %v, want %v", ok, tt.wantOk)
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if val != tt.wantVal {
				t.Errorf("val = %v, want %v", val, tt.wantVal)
			}
		})
	}
}

func TestBuildToolSuccessResult(t *testing.T) {
	tests := []struct {
		name       string
		structured any
		wantKeys   []string
	}{
		{
			name:       "nil",
			structured: nil,
			wantKeys:   []string{"content"},
		},
		{
			name:       "string",
			structured: "hello",
			wantKeys:   []string{"content"},
		},
		{
			name:       "map",
			structured: map[string]any{"key": "value"},
			wantKeys:   []string{"structuredContent"},
		},
		{
			name:       "empty string",
			structured: "",
			wantKeys:   []string{"content"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildToolSuccessResult(tt.structured)
			for _, key := range tt.wantKeys {
				if _, ok := result[key]; !ok {
					t.Errorf("missing key %q in result", key)
				}
			}
		})
	}
}

func TestBuildToolErrorResult(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{
			name:    "normal message",
			message: "tool not found",
			want:    "tool not found",
		},
		{
			name:    "empty message",
			message: "",
			want:    "tool execution failed",
		},
		{
			name:    "whitespace message",
			message: "   ",
			want:    "tool execution failed",
		},
		{
			name:    "with spaces",
			message: "  error message  ",
			want:    "error message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildToolErrorResult(tt.message)
			if _, ok := result["isError"]; !ok {
				t.Error("missing isError key")
			}
			content, ok := result["content"].([]map[string]any)
			if !ok {
				t.Fatal("content is not []map[string]any")
			}
			if len(content) == 0 {
				t.Fatal("content is empty")
			}
			text, ok := content[0]["text"].(string)
			if !ok {
				t.Fatal("text is not string")
			}
			if text != tt.want {
				t.Errorf("text = %q, want %q", text, tt.want)
			}
		})
	}
}

func TestIsToolError(t *testing.T) {
	tests := []struct {
		name   string
		result map[string]any
		want   bool
	}{
		{
			name:   "error result",
			result: BuildToolErrorResult("tool not found"),
			want:   true,
		},
		{
			name:   "success result",
			result: BuildToolSuccessResult("ok"),
			want:   false,
		},
		{
			name:   "missing key",
			result: map[string]any{"content": "ok"},
			want:   false,
		},
		{
			name:   "non bool value",
			result: map[string]any{"isError": "true"},
			want:   false,
		},
		{
			name:   "nil result",
			result: nil,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsToolError(tt.result); got != tt.want {
				t.Errorf("IsToolError() = %v, want %v", got, tt.want)
			}
		})
	}
}
