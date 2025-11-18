package engine

import "context"

// Scheduler is a placeholder for future DAG scheduling logic.
type Scheduler interface {
	Schedule(ctx context.Context, job *Job) error
}

// NoopScheduler is a stub scheduler used for the initial milestone.
type NoopScheduler struct{}

// Schedule immediately succeeds because the BasicEngine handles execution directly.
func (NoopScheduler) Schedule(ctx context.Context, job *Job) error {
	return nil
}
