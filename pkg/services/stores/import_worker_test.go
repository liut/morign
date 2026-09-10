package stores

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/liut/morign/pkg/models/corpus"
)

func importTestTask(id string) *corpus.ImportTask {
	obj := new(corpus.ImportTask)
	obj.Data = id
	return obj
}

type fakeTaskProcessor struct {
	mu         sync.Mutex
	queue      []*corpus.ImportTask
	processErr error
	recoverN   int
	seq        []string
}

func (f *fakeTaskProcessor) RecoverImportTasks(ctx context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq = append(f.seq, "recover")
	return f.recoverN, nil
}

func (f *fakeTaskProcessor) ClaimImportTask(ctx context.Context) (*corpus.ImportTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queue) == 0 {
		return nil, ErrNotFound
	}
	task := f.queue[0]
	f.queue = f.queue[1:]
	f.seq = append(f.seq, "claim:"+task.Data)
	return task, nil
}

func (f *fakeTaskProcessor) ProcessImportTask(ctx context.Context, task *corpus.ImportTask) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq = append(f.seq, "process:"+task.Data)
	return f.processErr
}

func (f *fakeTaskProcessor) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.seq...)
}

func runWorker(t *testing.T, fake *fakeTaskProcessor) (context.CancelFunc, <-chan struct{}) {
	t.Helper()
	w := NewImportWorker(fake)
	w.interval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()
	return cancel, done
}

func eventuallySeq(t *testing.T, fake *fakeTaskProcessor, want []string) {
	t.Helper()
	require.Eventually(t, func() bool {
		got := fake.snapshot()
		if len(got) != len(want) {
			return false
		}
		for i := range want {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}, 3*time.Second, 10*time.Millisecond)
}

func TestImportWorker_ProcessesTasksSerially(t *testing.T) {
	fake := &fakeTaskProcessor{queue: []*corpus.ImportTask{importTestTask("t1"), importTestTask("t2")}}
	cancel, done := runWorker(t, fake)
	defer func() { cancel(); <-done }()

	eventuallySeq(t, fake, []string{"recover", "claim:t1", "process:t1", "claim:t2", "process:t2"})
}

func TestImportWorker_WaitsForNewTask(t *testing.T) {
	fake := &fakeTaskProcessor{}
	cancel, done := runWorker(t, fake)
	defer func() { cancel(); <-done }()

	require.Eventually(t, func() bool {
		return len(fake.snapshot()) > 0
	}, 3*time.Second, 10*time.Millisecond)

	fake.mu.Lock()
	fake.queue = append(fake.queue, importTestTask("later"))
	fake.mu.Unlock()

	eventuallySeq(t, fake, []string{"recover", "claim:later", "process:later"})
}

func TestImportWorker_RecoversBeforeClaim(t *testing.T) {
	fake := &fakeTaskProcessor{
		queue:    []*corpus.ImportTask{importTestTask("t1")},
		recoverN: 2,
	}
	cancel, done := runWorker(t, fake)
	defer func() { cancel(); <-done }()

	eventuallySeq(t, fake, []string{"recover", "claim:t1", "process:t1"})
	assert.Equal(t, 2, fake.recoverN)
}

func TestImportWorker_ProcessErrorContinues(t *testing.T) {
	fake := &fakeTaskProcessor{
		queue:      []*corpus.ImportTask{importTestTask("t1"), importTestTask("t2")},
		processErr: errors.New("boom"),
	}
	cancel, done := runWorker(t, fake)
	defer func() { cancel(); <-done }()

	eventuallySeq(t, fake, []string{"recover", "claim:t1", "process:t1", "claim:t2", "process:t2"})
}

func TestImportWorker_CancelExits(t *testing.T) {
	fake := &fakeTaskProcessor{}
	w := NewImportWorker(fake)
	w.interval = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not exit after cancel")
	}
}
