// Package queue runs background jobs (archive compression, extraction, ...) on a
// small worker pool, exposing their status for polling. Jobs live in memory,
// which suits a single-node deployment; a clustered setup would back this with a
// shared store.
package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Status is a job's lifecycle state.
type Status string

const (
	StatusPending Status = "pending"
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

// Indeterminate is the Percent value for work whose size cannot be known until
// it is finished — a TAR has no index to count from. The UI shows a moving bar
// rather than a figure.
const Indeterminate = -1

// Progress is what a running task publishes about itself.
//
// Percent is the headline figure, or Indeterminate. Done and
// Total, when Total is non-zero, are what the remaining time is estimated
// from — and are worth reporting in bytes rather than items, since a hundred
// files of wildly different sizes make a per-file count a poor predictor.
type Progress struct {
	Percent int
	Message string
	Done    int64
	Total   int64
	// Unit labels what Done and Total count, for the UI: "bytes" or "files".
	Unit string
}

// Report lets a running task publish its progress.
type Report func(Progress)

// TaskFunc is the work a job performs; its result map is exposed to the caller.
type TaskFunc func(ctx context.Context, report Report) (map[string]any, error)

// Job is one unit of background work.
type Job struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	UserID   uint   `json:"user_id"`
	Status   Status `json:"status"`
	Progress int    `json:"progress"`
	Message  string `json:"message"`
	// Done and Total describe the work in Unit ("bytes", "files"); Total is 0
	// when the task cannot know it up front.
	Done  int64  `json:"done,omitempty"`
	Total int64  `json:"total,omitempty"`
	Unit  string `json:"unit,omitempty"`
	// ETASeconds is the estimated time left, or 0 when it cannot be estimated
	// yet. Computed here rather than in the browser: the estimate depends on
	// how long the job has been running, and a client comparing its own clock
	// to the server's start time would be off by whatever their skew is.
	ETASeconds int            `json:"eta_seconds,omitempty"`
	Error      string         `json:"error,omitempty"`
	Result     map[string]any `json:"result,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`

	// Cancellable reports whether the job can still be stopped, so a UI knows
	// whether to offer it.
	Cancellable bool `json:"cancellable,omitempty"`

	// startedAt is when the worker picked the job up, the baseline for the
	// estimate. Unexported: the estimate is published, its input is not.
	startedAt time.Time
	// cancel stops the job's own context. Set while it runs.
	cancel context.CancelFunc
	// cancelled records that the stop was asked for, so the job reports itself
	// cancelled rather than failed — a user who stopped a job does not need to
	// be told it went wrong.
	cancelled bool
}

type task struct {
	job *Job
	fn  TaskFunc
}

// ErrCancelled is the error a cancelled job finishes with.
var ErrCancelled = errors.New("cancelled")

// Queue schedules and tracks background jobs.
type Queue struct {
	mu      sync.Mutex
	jobs    map[string]*Job
	subs    map[uint]map[int]chan struct{} // per user, per subscription
	nextSub int
	ch      chan task
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	retain  time.Duration // how long finished jobs stay pollable before pruning
	closed  bool
}

// New builds a Queue with the given number of workers (at least one).
// New builds a Queue with the given number of workers (at least one). Finished
// jobs are kept pollable for an hour, then pruned by a background janitor so the
// in-memory job map does not grow without bound.
func New(workers int) *Queue {
	if workers < 1 {
		workers = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	q := &Queue{
		jobs:   make(map[string]*Job),
		subs:   make(map[uint]map[int]chan struct{}),
		ch:     make(chan task, 128),
		ctx:    ctx,
		cancel: cancel,
		retain: time.Hour,
	}
	for i := 0; i < workers; i++ {
		q.wg.Add(1)
		go q.worker()
	}
	go q.janitor()
	return q
}

// Close stops the queue: it cancels in-flight jobs' context and waits for the
// workers to return. Queued-but-unstarted jobs are dropped. Idempotent.
func (q *Queue) Close() {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	q.mu.Unlock()
	q.cancel()
	q.wg.Wait()
}

// Enqueue registers a job and schedules fn to run.
// Enqueue registers a job and schedules fn to run. If the queue is shutting
// down the job is returned already marked failed.
func (q *Queue) Enqueue(userID uint, typ string, fn TaskFunc) *Job {
	now := time.Now()
	job := &Job{
		ID:        newID(),
		Type:      typ,
		UserID:    userID,
		Status:    StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		job.Status = StatusFailed
		job.Error = "queue is shutting down"
		return job
	}
	q.jobs[job.ID] = job
	q.mu.Unlock()

	// Never blocks on a closed channel: the worker loop selects on ctx too, so we
	// hand off the task or fail it if the queue is cancelled meanwhile.
	select {
	case q.ch <- task{job: job, fn: fn}:
	case <-q.ctx.Done():
		q.set(job, func(j *Job) {
			j.Status = StatusFailed
			j.Error = "queue is shutting down"
		})
	}
	return job
}

// Get returns a snapshot of a job owned by userID.
func (q *Queue) Get(userID uint, id string) (Job, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[id]
	if !ok || j.UserID != userID {
		return Job{}, false
	}
	return *j, true
}

// ListByUser returns snapshots of a user's jobs.
func (q *Queue) ListByUser(userID uint) []Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	var out []Job
	for _, j := range q.jobs {
		if j.UserID == userID {
			out = append(out, *j)
		}
	}
	return out
}

func (q *Queue) worker() {
	defer q.wg.Done()
	for {
		select {
		case <-q.ctx.Done():
			return
		case t := <-q.ch:
			q.run(t)
		}
	}
}

// run executes one task, publishing progress and the terminal status.
// run executes one task, publishing progress and the terminal status. A panic
// in the task is recovered and recorded as a failure so a single bad job cannot
// take down the worker goroutine (and with it the process).
func (q *Queue) run(t task) {
	// A context of the job's own, so one job can be stopped without touching
	// the others or the queue.
	ctx, cancel := context.WithCancel(q.ctx)
	defer cancel()
	q.set(t.job, func(j *Job) {
		j.Status = StatusRunning
		j.startedAt = time.Now()
		j.cancel = cancel
		j.Cancellable = true
	})
	report := func(p Progress) {
		q.set(t.job, func(j *Job) {
			// Assigned even when indeterminate: a task that cannot know its size
			// has to be able to say so, and the previous figure standing in for
			// it is what made the moving bar unreachable.
			j.Progress = p.Percent
			if p.Message != "" {
				j.Message = p.Message
			}
			if p.Unit != "" {
				j.Unit = p.Unit
			}
			j.Done = p.Done
			j.Total = p.Total
			j.ETASeconds = estimate(j.startedAt, p.Done, p.Total)
		})
	}
	defer func() {
		if r := recover(); r != nil {
			q.set(t.job, func(j *Job) {
				j.Status = StatusFailed
				j.Error = fmt.Sprintf("task panicked: %v", r)
			})
		}
	}()
	result, err := t.fn(ctx, report)
	q.set(t.job, func(j *Job) {
		j.ETASeconds = 0
		j.Cancellable = false
		j.cancel = nil
		switch {
		case j.cancelled:
			// Whatever the task returned, the user asked for this.
			j.Status = StatusFailed
			j.Error = ErrCancelled.Error()
		case err != nil:
			j.Status = StatusFailed
			j.Error = err.Error()
		default:
			j.Status = StatusDone
			j.Progress = 100
			j.Result = result
		}
	})
}

// Cancel stops one of the user's running jobs. It reports whether there was
// such a job to stop; a job that already finished is not an error to cancel,
// it is simply no longer there.
//
// The task is expected to notice its context and unwind — the archive jobs roll
// back what they had written — so cancelling is not a way to leave half a tree
// behind.
func (q *Queue) Cancel(userID uint, id string) bool {
	q.mu.Lock()
	job, ok := q.jobs[id]
	if !ok || job.UserID != userID || job.cancel == nil {
		q.mu.Unlock()
		return false
	}
	cancel := job.cancel
	job.cancelled = true
	job.Message = "Annulation…"
	q.mu.Unlock()

	cancel()
	// Wake the watchers: the UI should show the cancellation immediately, not
	// when the task next reports.
	q.set(job, func(*Job) {})
	return true
}

// estimate extrapolates the time left from how long the work so far took. It
// returns 0 while there is nothing to extrapolate from — no total, nothing done
// yet, or a job that has only just started — so the UI shows no estimate rather
// than a wild one.
func estimate(startedAt time.Time, done, total int64) int {
	if startedAt.IsZero() || total <= 0 || done <= 0 || done >= total {
		return 0
	}
	elapsed := time.Since(startedAt)
	if elapsed < 500*time.Millisecond {
		return 0
	}
	perByte := float64(elapsed) / float64(done)
	left := time.Duration(perByte * float64(total-done))
	if secs := int(left.Round(time.Second).Seconds()); secs > 0 {
		return secs
	}
	return 0
}

// janitor periodically drops finished jobs older than the retention window so
// the job map stays bounded over a long-running process.
func (q *Queue) janitor() {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-q.ctx.Done():
			return
		case <-t.C:
			q.prune()
		}
	}
}

func (q *Queue) prune() {
	cutoff := time.Now().Add(-q.retain)
	q.mu.Lock()
	defer q.mu.Unlock()
	for id, j := range q.jobs {
		if (j.Status == StatusDone || j.Status == StatusFailed) && j.UpdatedAt.Before(cutoff) {
			delete(q.jobs, id)
		}
	}
}

// set mutates a job under the lock and bumps its timestamp.
func (q *Queue) set(job *Job, mut func(*Job)) {
	q.mu.Lock()
	mut(job)
	job.UpdatedAt = time.Now()
	subs := q.subs[job.UserID]
	watchers := make([]chan struct{}, 0, len(subs))
	for _, ch := range subs {
		watchers = append(watchers, ch)
	}
	q.mu.Unlock()

	// A bare signal, not the job: the watcher re-reads the current state, so a
	// signal dropped because one is already pending costs nothing — the next
	// read still sees the latest.
	for _, ch := range watchers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Subscribe returns a channel that receives a signal whenever any of the user's
// jobs changes, and a function to stop listening. The signal carries nothing:
// the caller reads the jobs it wants with ListByUser.
func (q *Queue) Subscribe(userID uint) (<-chan struct{}, func()) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.subs[userID] == nil {
		q.subs[userID] = map[int]chan struct{}{}
	}
	id := q.nextSub
	q.nextSub++
	ch := make(chan struct{}, 1)
	q.subs[userID][id] = ch
	return ch, func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		if subs, ok := q.subs[userID]; ok {
			delete(subs, id)
			if len(subs) == 0 {
				delete(q.subs, userID)
			}
		}
	}
}

func newID() string {
	buf := make([]byte, 12)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
