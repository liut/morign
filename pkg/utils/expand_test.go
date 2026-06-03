package utils

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExpandEnv(t *testing.T) {
	t.Setenv("FOO", "bar")
	t.Setenv("TOKEN", "abc123")

	tests := []struct {
		name  string
		input string
		want  string
	}{
		// Happy path
		{name: "single var exists", input: "${FOO}", want: "bar"},
		{name: "single var missing", input: "${MISSING}", want: ""},
		{name: "no var pattern", input: "plain text", want: "plain text"},
		{name: "bare dollar no braces", input: "$FOO", want: "bar"},
		{name: "multiple vars", input: "${FOO}-${TOKEN}", want: "bar-abc123"},
		{name: "var in URL", input: "https://api.example.com?key=${TOKEN}", want: "https://api.example.com?key=abc123"},
		// Edge cases
		{name: "empty string", input: "", want: ""},
		{name: "no dollars", input: "bot_id_value", want: "bot_id_value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := os.ExpandEnv(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
