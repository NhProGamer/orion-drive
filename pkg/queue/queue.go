// Package queue runs background jobs (archive compression, extraction, ...) on a
// small worker pool, exposing their status for polling. Jobs live in memory,
// which suits a single-node deployment; a clustered setup would back this with a
// shared store.
package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

// Report lets a running task publish progress (0-100) and a status message.
type Report func(progress int, message string)

// TaskFunc is the work a job performs; its result map is exposed to the caller.
type TaskFunc func(ctx context.Context, report Report) (map[string]any, error)

// Job is one unit of background work.
type Job struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	UserID    uint           `json:"user_id"`
	Status    Status         `json:"status"`
	Progress  int            `json:"progress"`
	Message   string         `json:"message"`
	Error     string         `json:"error,omitempty"`
	Result    map[string]any `json:"result,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type task struct {
	job *Job
	fn  TaskFunc
}

// Queue schedules and tracks background jobs.
// Queue schedules and tracks background jobs.
type Queue struct {
	mu     sync.Mutex
	jobs   map[string]*Job
	ch     chan task
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	retain time.Duration // how long finished jobs stay pollable before pruning
	closed bool
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
	q.set(t.job, func(j *Job) { j.Status = StatusRunning })
	report := func(p int, msg string) {
		q.set(t.job, func(j *Job) {
			if p >= 0 {
				j.Progress = p
			}
			j.Message = msg
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
	result, err := t.fn(q.ctx, report)
	q.set(t.job, func(j *Job) {
		if err != nil {
			j.Status = StatusFailed
			j.Error = err.Error()
		} else {
			j.Status = StatusDone
			j.Progress = 100
			j.Result = result
		}
	})
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
	defer q.mu.Unlock()
	mut(job)
	job.UpdatedAt = time.Now()
}

func newID() string {
	buf := make([]byte, 12)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
