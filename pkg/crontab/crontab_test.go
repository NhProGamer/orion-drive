package crontab

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type memStore struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *memStore) Get(_ context.Context, k string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[k], nil
}

func (s *memStore) Set(_ context.Context, k, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = v
	return nil
}

func waitIdle(t *testing.T, s *Scheduler, id string) State {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		for _, st := range s.List() {
			if st.ID == id && st.Status != StatusRunning {
				return st
			}
		}
	}
	t.Fatalf("task %s never finished", id)
	return State{}
}

// TestHistoryPersistsAndCaps: runs are recorded newest first, capped, and a
// scheduler built on the same store sees them (the "last run" after a restart).
func TestHistoryPersistsAndCaps(t *testing.T) {
	store := &memStore{m: map[string]string{}}
	s := New(slog.Default(), store)
	calls := 0
	s.Add("job", time.Hour, func(context.Context, Report) (int64, error) {
		calls++
		return int64(calls), nil
	})
	for i := 0; i < historySize+2; i++ {
		if err := s.RunNow("job"); err != nil {
			t.Fatal(err)
		}
		waitIdle(t, s, "job")
	}
	st := waitIdle(t, s, "job")
	if len(st.History) != historySize || st.History[0].Count != int64(historySize+2) || st.Status != StatusDone {
		t.Fatalf("history = %+v", st.History)
	}

	again := New(slog.Default(), store)
	again.Add("job", time.Hour, func(context.Context, Report) (int64, error) { return 0, nil })
	if got := again.List()[0].History; len(got) != historySize || got[0].Count != int64(historySize+2) {
		t.Fatalf("reloaded history = %+v", got)
	}
}

// TestOnceTaskFailure: a one-off task is listed after the periodic ones, and a
// failure is recorded with its error.
func TestOnceTaskFailure(t *testing.T) {
	s := New(slog.Default(), nil)
	s.Add("job", time.Hour, func(context.Context, Report) (int64, error) { return 0, nil })
	id := s.Once("policy-delete", map[string]string{"name": "slave"}, func(context.Context, Report) (int64, error) {
		return 2, context.DeadlineExceeded
	})
	st := waitIdle(t, s, id)
	if st.Kind != KindOnce || st.Status != StatusFailed || st.History[0].Error == "" || st.Params["name"] != "slave" {
		t.Fatalf("once state = %+v", st)
	}
	if l := s.List(); len(l) != 2 || l[0].ID != "job" || l[1].ID != id {
		t.Fatalf("order = %+v", l)
	}
	if s.RunNow(id) != ErrUnknown {
		t.Fatal("a one-off task must not be re-runnable")
	}
}
