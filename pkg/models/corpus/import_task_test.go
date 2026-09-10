package corpus

import (
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
