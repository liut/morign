package convo

import "testing"

func TestTurnStatusString(t *testing.T) {
	cases := []struct {
		status TurnStatus
		want   string
	}{
		{TurnStatusQueued, "queued"},
		{TurnStatusRunning, "running"},
		{TurnStatusSucceeded, "succeeded"},
		{TurnStatusFailed, "failed"},
		{TurnStatusCancelled, "cancelled"},
		{TurnStatusInterrupted, "interrupted"},
	}
	for _, c := range cases {
		if got := c.status.String(); got != c.want {
			t.Errorf("TurnStatus(%d).String() = %q, want %q", int8(c.status), got, c.want)
		}
	}
}

func TestTurnStatusDecode(t *testing.T) {
	cases := []struct {
		in   string
		want TurnStatus
	}{
		{"1", TurnStatusQueued},
		{"queued", TurnStatusQueued},
		{"Queued", TurnStatusQueued},
		{"3", TurnStatusSucceeded},
		{"succeeded", TurnStatusSucceeded},
		{"6", TurnStatusInterrupted},
		{"interrupted", TurnStatusInterrupted},
	}
	for _, c := range cases {
		var got TurnStatus
		if err := got.Decode(c.in); err != nil {
			t.Errorf("Decode(%q) error = %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Decode(%q) = %d, want %d", c.in, int8(got), int8(c.want))
		}
	}
}

func TestTurnStatusDecodeRejectsUnknown(t *testing.T) {
	// 未知状态必须报错：拼错的值落进库里就再也查不出来。
	for _, in := range []string{"", "suceeded", "0", "7"} {
		var got TurnStatus
		if err := got.Decode(in); err == nil {
			t.Errorf("Decode(%q) error = nil, want an error", in)
		}
	}
}
