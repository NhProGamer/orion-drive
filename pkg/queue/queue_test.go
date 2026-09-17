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
		report(Progress{Percent: 50, Message: "halfway"})
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

func TestProgressCarriesUnitsAndAnEstimate(t *testing.T) {
	q := New(1)
	defer q.Close()

	// Hold the job open until the test has read a mid-flight snapshot.
	release := make(chan struct{})
	job := q.Enqueue(3, "test", func(ctx context.Context, report Report) (map[string]any, error) {
		// A quarter of the way through, after enough elapsed time for an
		// estimate to mean anything.
		time.Sleep(600 * time.Millisecond)
		report(Progress{Percent: 25, Message: "working", Done: 250, Total: 1000, Unit: "bytes"})
		<-release
		return nil, nil
	})

	var mid Job
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := q.Get(3, job.ID)
		if ok && got.Done == 250 {
			mid = got
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(release)

	if mid.Total != 1000 || mid.Unit != "bytes" || mid.Progress != 25 {
		t.Fatalf("snapshot = %+v", mid)
	}
	// A quarter done after ~0.6s extrapolates to roughly 1.8s left; assert the
	// shape rather than the number, which depends on scheduling.
	if mid.ETASeconds <= 0 || mid.ETASeconds > 30 {
		t.Fatalf("eta = %ds, want a small positive estimate", mid.ETASeconds)
	}

	done := waitFor(t, q, 3, job.ID)
	if done.ETASeconds != 0 {
		t.Fatalf("a finished job must carry no estimate, got %ds", done.ETASeconds)
	}
}

func TestEstimateStaysSilentWithoutEnoughToGoOn(t *testing.T) {
	start := time.Now().Add(-10 * time.Second)
	cases := []struct {
		name        string
		startedAt   time.Time
		done, total int64
	}{
		{"no total", start, 50, 0},
		{"nothing done", start, 0, 100},
		{"already complete", start, 100, 100},
		{"never started", time.Time{}, 50, 100},
		{"only just started", time.Now(), 50, 100},
	}
	for _, c := range cases {
		if got := estimate(c.startedAt, c.done, c.total); got != 0 {
			t.Errorf("%s: estimate = %d, want 0", c.name, got)
		}
	}
	// With real ground to stand on it does estimate: half of a job that took
	// 10s has about 10s left.
	if got := estimate(start, 50, 100); got < 8 || got > 12 {
		t.Errorf("estimate = %ds, want about 10", got)
	}
}

func TestSubscribeSignalsTheUsersJobs(t *testing.T) {
	q := New(1)
	defer q.Close()

	signals, stop := q.Subscribe(11)
	defer stop()
	other, stopOther := q.Subscribe(99)
	defer stopOther()

	q.Enqueue(11, "test", func(ctx context.Context, report Report) (map[string]any, error) {
		report(Progress{Percent: 10, Message: "step"})
		return nil, nil
	})

	select {
	case <-signals:
	case <-time.After(5 * time.Second):
		t.Fatal("no signal for the user's own job")
	}
	// Another user's subscription must not be woken by it.
	select {
	case <-other:
		t.Fatal("a job signalled a subscription belonging to another user")
	case <-time.After(100 * time.Millisecond):
	}

	// After unsubscribing, signals stop reaching the channel.
	stop()
	q.Enqueue(11, "test", func(ctx context.Context, report Report) (map[string]any, error) { return nil, nil })
	// Drain whatever was already queued, then confirm nothing new arrives.
	select {
	case <-signals:
	default:
	}
	select {
	case <-signals:
		t.Fatal("a cancelled subscription still received a signal")
	case <-time.After(200 * time.Millisecond):
	}
}
