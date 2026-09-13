package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestShichen(t *testing.T) {
	tests := []struct {
		hour int
		want string
	}{
		{0, "子时"}, {1, "丑时"}, {2, "丑时"}, {3, "寅时"}, {4, "寅时"},
		{5, "卯时"}, {6, "卯时"}, {7, "辰时"}, {8, "辰时"}, {9, "巳时"},
		{10, "巳时"}, {11, "午时"}, {12, "午时"}, {13, "未时"}, {14, "未时"},
		{15, "申时"}, {16, "申时"}, {17, "酉时"}, {18, "酉时"}, {19, "戌时"},
		{20, "戌时"}, {21, "亥时"}, {22, "亥时"}, {23, "子时"},
	}
	for _, tt := range tests {
		if got := shichen(tt.hour); got != tt.want {
			t.Errorf("shichen(%d) = %q, want %q", tt.hour, got, tt.want)
		}
	}
}

func TestInvokeCurrentTime(t *testing.T) {
	res, err := invokeCurrentTime(context.Background(), nil)
	if err != nil {
		t.Fatalf("invokeCurrentTime: %v", err)
	}
	text := resultText(res)

	if !strings.Contains(text, time.Now().Format("2006-01-02")) {
		t.Errorf("result missing today's date: %q", text)
	}
	if !strings.Contains(text, "当前时辰: ") {
		t.Errorf("result missing the 时辰 line: %q", text)
	}
}

func TestRegistryRegistersCurrentTime(t *testing.T) {
	reg := NewRegistry(nil)
	var found bool
	for _, d := range reg.ToolsFor(context.Background()) {
		if d.Name == ToolNameCurrentTime {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("current_time not registered: %s", reg.ToolsFor(context.Background()))
	}
}
