package stores

import (
	"context"
	"errors"
	"time"

	"github.com/liut/morign/pkg/models/corpus"
)

const importWorkerPollInterval = time.Second

// ImportTaskProcessor 是导入 worker 依赖的最小接口
type ImportTaskProcessor interface {
	RecoverImportTasks(ctx context.Context) (int, error)
	ClaimImportTask(ctx context.Context) (*corpus.ImportTask, error)
	ProcessImportTask(ctx context.Context, task *corpus.ImportTask) error
}

// ImportWorker 进程内串行消费导入任务
type ImportWorker struct {
	sto      ImportTaskProcessor
	interval time.Duration
}

func NewImportWorker(sto ImportTaskProcessor) *ImportWorker {
	return &ImportWorker{sto: sto, interval: importWorkerPollInterval}
}

// Run 阻塞执行：先恢复遗留任务，再循环认领/处理，ctx 取消时退出
func (w *ImportWorker) Run(ctx context.Context) {
	n, err := w.sto.RecoverImportTasks(ctx)
	if err != nil {
		logger().Warnw("recover import tasks fail", "err", err)
	} else if n > 0 {
		logger().Infow("recovered import tasks", "n", n)
	}

	for {
		if ctx.Err() != nil {
			return
		}
		task, err := w.sto.ClaimImportTask(ctx)
		if err != nil {
			if !errors.Is(err, ErrNotFound) {
				logger().Infow("claim import task fail", "err", err)
			}
			if !waitOrDone(ctx, w.interval) {
				return
			}
			continue
		}
		if err := w.sto.ProcessImportTask(ctx, task); err != nil {
			logger().Infow("process import task fail", "id", task.StringID(), "err", err)
		}
	}
}

func waitOrDone(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
