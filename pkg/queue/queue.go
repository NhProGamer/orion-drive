// Package queue runs background jobs (archive compression, extraction, ...) on a
// small worker pool, exposing their status for polling. Jobs live in memory,
// which suits a single-node deployment; a clustered setup would back this with a
// shared store.
package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
type Queue struct {
	mu   sync.Mutex
	jobs map[string]*Job
	ch   chan task
	ctx  context.Context
}

// New builds a Queue with the given number of workers (at least one).
func New(workers int) *Queue {
	if workers < 1 {
		workers = 1
	}
	q := &Queue{
		jobs: make(map[string]*Job),
		ch:   make(chan task, 128),
		ctx:  context.Background(),
	}
	for i := 0; i < workers; i++ {
		go q.worker()
	}
	return q
}

// Enqueue registers a job and schedules fn to run.
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
	q.jobs[job.ID] = job
	q.mu.Unlock()

	q.ch <- task{job: job, fn: fn}
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
	for t := range q.ch {
		q.set(t.job, func(j *Job) { j.Status = StatusRunning })
		report := func(p int, msg string) {
			q.set(t.job, func(j *Job) {
				if p >= 0 {
					j.Progress = p
				}
				j.Message = msg
			})
		}
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
