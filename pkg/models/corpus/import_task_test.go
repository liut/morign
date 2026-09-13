package corpus

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestImportTaskStatusValues(t *testing.T) {
	assert.Equal(t, ImportTaskStatus(1), ImportTaskStatusPending)
	assert.Equal(t, ImportTaskStatus(2), ImportTaskStatusProcessing)
	assert.Equal(t, ImportTaskStatus(3), ImportTaskStatusSucceeded)
	assert.Equal(t, ImportTaskStatus(4), ImportTaskStatusFailed)
}

func TestImportTaskStatusString(t *testing.T) {
	assert.Equal(t, "pending", ImportTaskStatusPending.String())
	assert.Equal(t, "processing", ImportTaskStatusProcessing.String())
	assert.Equal(t, "succeeded", ImportTaskStatusSucceeded.String())
	assert.Equal(t, "failed", ImportTaskStatusFailed.String())
}

func TestImportTaskStatusDecode(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want ImportTaskStatus
	}{
		{"1", ImportTaskStatusPending},
		{"pending", ImportTaskStatusPending},
		{"Pending", ImportTaskStatusPending},
		{"2", ImportTaskStatusProcessing},
		{"processing", ImportTaskStatusProcessing},
		{"Processing", ImportTaskStatusProcessing},
		{"3", ImportTaskStatusSucceeded},
		{"succeeded", ImportTaskStatusSucceeded},
		{"Succeeded", ImportTaskStatusSucceeded},
		{"4", ImportTaskStatusFailed},
		{"failed", ImportTaskStatusFailed},
		{"Failed", ImportTaskStatusFailed},
	} {
		var got ImportTaskStatus
		assert.NoError(t, got.Decode(tc.in), "decode %q", tc.in)
		assert.Equal(t, tc.want, got)
	}

	var got ImportTaskStatus
	assert.Error(t, got.Decode("unknown"))
	assert.Error(t, got.Decode(""))
	assert.Error(t, got.Decode("5"))
}

func TestImportTaskStatusTextRoundTrip(t *testing.T) {
	for _, s := range []ImportTaskStatus{
		ImportTaskStatusPending,
		ImportTaskStatusProcessing,
		ImportTaskStatusSucceeded,
		ImportTaskStatusFailed,
	} {
		b, err := s.MarshalText()
		assert.NoError(t, err)

		var got ImportTaskStatus
		assert.NoError(t, got.UnmarshalText(b))
		assert.Equal(t, s, got)
	}
}

func TestImportFailureKindValues(t *testing.T) {
	assert.Equal(t, ImportFailureKind(1), ImportFailureKindFailed)
	assert.Equal(t, ImportFailureKind(2), ImportFailureKindSkipped)
}

func TestImportFailureKindString(t *testing.T) {
	assert.Equal(t, "failed", ImportFailureKindFailed.String())
	assert.Equal(t, "skipped", ImportFailureKindSkipped.String())
	// 零值表示「未分类」，只用于历史数据，取字符串时不应 panic
	assert.NotPanics(t, func() { _ = ImportFailureKind(0).String() })
}

func TestImportFailureKindDecode(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want ImportFailureKind
	}{
		{"1", ImportFailureKindFailed},
		{"failed", ImportFailureKindFailed},
		{"Failed", ImportFailureKindFailed},
		{"2", ImportFailureKindSkipped},
		{"skipped", ImportFailureKindSkipped},
		{"Skipped", ImportFailureKindSkipped},
	} {
		var got ImportFailureKind
		assert.NoError(t, got.Decode(tc.in), "decode %q", tc.in)
		assert.Equal(t, tc.want, got)
	}

	var got ImportFailureKind
	assert.Error(t, got.Decode("unknown"))
	assert.Error(t, got.Decode(""))
	assert.Error(t, got.Decode("3"))
}

func TestImportFailureKindTextRoundTrip(t *testing.T) {
	for _, s := range []ImportFailureKind{ImportFailureKindFailed, ImportFailureKindSkipped} {
		b, err := s.MarshalText()
		assert.NoError(t, err)

		var got ImportFailureKind
		assert.NoError(t, got.UnmarshalText(b))
		assert.Equal(t, s, got)
	}
}

func TestImportFailureKindJSON(t *testing.T) {
	b, err := json.Marshal(ImportFailure{Line: 2, Title: "t", Reason: "r", Kind: ImportFailureKindSkipped})
	assert.NoError(t, err)
	assert.Contains(t, string(b), `"kind":"skipped"`)

	// 零值（历史数据）序列化时省略 kind，前端据此走降级路径
	b, err = json.Marshal(ImportFailure{Line: 2, Title: "t", Reason: "r"})
	assert.NoError(t, err)
	assert.NotContains(t, string(b), `"kind"`)

	// 反序列化旧数据（无 kind）不得报错，零值可安全比较
	var got ImportFailure
	assert.NoError(t, json.Unmarshal([]byte(`{"line":2,"title":"t","reason":"r"}`), &got))
	assert.Equal(t, ImportFailureKind(0), got.Kind)
}
