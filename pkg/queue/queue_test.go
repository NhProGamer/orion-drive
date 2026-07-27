package queue

import (
	"context"
	"errors"
	"testing"
	"time"
)

// waitFor polls until a job reaches a terminal state or times out.
func waitFor(t *testing.T, q *Queue, uid uint, id string) Job {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		j, ok := q.Get(uid, id)
		if ok && (j.Status == StatusDone || j.Status == StatusFailed) {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("job did not finish in time")
	return Job{}
}

func TestJobSucceeds(t *testing.T) {
	q := New(2)
	job := q.Enqueue(7, "test", func(ctx context.Context, report Report) (map[string]any, error) {
		report(50, "halfway")
		return map[string]any{"file_id": 42}, nil
	})
	done := waitFor(t, q, 7, job.ID)
	if done.Status != StatusDone {
		t.Fatalf("status = %s", done.Status)
	}
	if done.Progress != 100 {
		t.Fatalf("progress = %d", done.Progress)
	}
	if done.Result["file_id"] != 42 {
		t.Fatalf("result = %v", done.Result)
	}
}

func TestJobFails(t *testing.T) {
	q := New(1)
	job := q.Enqueue(7, "test", func(ctx context.Context, report Report) (map[string]any, error) {
		return nil, errors.New("boom")
	})
	done := waitFor(t, q, 7, job.ID)
	if done.Status != StatusFailed || done.Error != "boom" {
		t.Fatalf("status=%s err=%q", done.Status, done.Error)
	}
}

func TestOwnershipIsolation(t *testing.T) {
	q := New(1)
	job := q.Enqueue(7, "test", func(ctx context.Context, report Report) (map[string]any, error) {
		return nil, nil
	})
	waitFor(t, q, 7, job.ID)
	if _, ok := q.Get(9, job.ID); ok {
		t.Fatal("job leaked to another user")
	}
}

func TestQueuePrunesFinishedJobs(t *testing.T) {
	q := New(1)
	defer q.Close()
	job := q.Enqueue(1, "test", func(ctx context.Context, report Report) (map[string]any, error) {
		return nil, nil
	})
	waitFor(t, q, 1, job.ID)

	// Age the finished job past the retention window and sweep it.
	q.mu.Lock()
	q.jobs[job.ID].UpdatedAt = time.Now().Add(-2 * time.Hour)
	q.mu.Unlock()
	q.prune()

	if _, ok := q.Get(1, job.ID); ok {
		t.Fatal("finished job past retention should be pruned")
	}
}

func TestQueueCloseRejectsEnqueue(t *testing.T) {
	q := New(1)
	q.Close()
	q.Close() // idempotent
	job := q.Enqueue(1, "test", func(ctx context.Context, report Report) (map[string]any, error) {
		return nil, nil
	})
	if job.Status != StatusFailed {
		t.Fatalf("enqueue after close should fail, got %q", job.Status)
	}
}
