// scheduler/common.go：模块内可复用逻辑归口。
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"excel_worker_service/base"
)

// Handler executes one decoded scheduler task payload.
type Handler func(context.Context, []byte) error

// RunnerOptions controls task claim, retry and polling behavior.
type RunnerOptions struct {
	OwnerPID      string
	LeaseDuration time.Duration
	PollInterval  time.Duration
	BatchSize     int
	RetryInitial  time.Duration
	RetryMax      time.Duration
	Now           func() time.Time
}

// Runner claims due tasks and dispatches them by task type.
type Runner struct {
	repository *base.TaskRepository
	options    RunnerOptions

	mu       sync.RWMutex
	handlers map[string]Handler
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewRunner constructs a scheduler loop with safe defaults.
func NewRunner(repository *base.TaskRepository, options RunnerOptions) *Runner {
	if options.OwnerPID == "" {
		hostname, _ := os.Hostname()
		options.OwnerPID = fmt.Sprintf("%s:%d", hostname, os.Getpid())
	}
	if options.LeaseDuration <= 0 {
		options.LeaseDuration = 30 * time.Second
	}
	if options.PollInterval <= 0 {
		options.PollInterval = time.Second
	}
	if options.BatchSize <= 0 {
		options.BatchSize = 20
	}
	if options.RetryInitial <= 0 {
		options.RetryInitial = time.Second
	}
	if options.RetryMax < options.RetryInitial {
		options.RetryMax = time.Minute
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Runner{
		repository: repository,
		options:    options,
		handlers:   make(map[string]Handler),
	}
}

// Register binds one task type to a handler before Start.
func (runner *Runner) Register(taskType string, handler Handler) error {
	if runner == nil {
		return errors.New("scheduler runner is required")
	}
	if taskType == "" || handler == nil {
		return errors.New("scheduler task type and handler are required")
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.cancel != nil {
		return errors.New("scheduler runner already started")
	}
	runner.handlers[taskType] = handler
	return nil
}

// RunOnce claims and handles one available batch.
func (runner *Runner) RunOnce(ctx context.Context) (int, error) {
	if runner == nil || runner.repository == nil {
		return 0, errors.New("scheduler repository is required")
	}
	now := runner.options.Now().UTC()
	tasks, err := runner.repository.ClaimDue(
		ctx,
		now,
		runner.options.OwnerPID,
		runner.options.LeaseDuration,
		runner.options.BatchSize,
	)
	if err != nil {
		return 0, err
	}
	succeeded := 0
	var runErrors []error
	for _, task := range tasks {
		runner.mu.RLock()
		handler := runner.handlers[task.Type]
		runner.mu.RUnlock()
		if handler == nil {
			if _, markErr := runner.repository.MarkFailed(
				ctx,
				task,
				now,
				runner.options.RetryInitial,
				runner.options.RetryMax,
				fmt.Errorf("scheduler task type %q has no handler", task.Type),
			); markErr != nil {
				runErrors = append(runErrors, markErr)
			}
			continue
		}
		if handlerErr := handler(ctx, task.Payload); handlerErr != nil {
			if _, markErr := runner.repository.MarkFailed(
				ctx,
				task,
				now,
				runner.options.RetryInitial,
				runner.options.RetryMax,
				handlerErr,
			); markErr != nil {
				runErrors = append(runErrors, markErr)
			}
			continue
		}
		if _, markErr := runner.repository.MarkSucceeded(ctx, task, now); markErr != nil {
			runErrors = append(runErrors, markErr)
			continue
		}
		succeeded++
	}
	return succeeded, errors.Join(runErrors...)
}

// Start launches the recurring task loop.
func (runner *Runner) Start(ctx context.Context) error {
	if runner == nil || runner.repository == nil {
		return errors.New("scheduler repository is required")
	}
	runner.mu.Lock()
	if runner.cancel != nil {
		runner.mu.Unlock()
		return errors.New("scheduler runner already started")
	}
	stop := make(chan struct{})
	runner.cancel = func() { close(stop) }
	runner.done = make(chan struct{})
	done := runner.done
	runner.mu.Unlock()

	go func() {
		defer close(done)
		ticker := time.NewTicker(runner.options.PollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			default:
			}
			_, _ = runner.RunOnce(ctx)
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
			}
		}
	}()
	return nil
}

// Stop terminates the recurring loop and waits for its current batch.
func (runner *Runner) Stop(ctx context.Context) error {
	if runner == nil {
		return nil
	}
	runner.mu.Lock()
	cancel := runner.cancel
	done := runner.done
	runner.cancel = nil
	runner.done = nil
	runner.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
