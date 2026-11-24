// Package crontab runs the server's maintenance tasks: periodic jobs on fixed
// intervals, plus one-off tasks an admin action starts (a storage-policy
// deletion). It tracks what each one is doing — running or not, progress, last
// runs — for the admin Tasks view, and lets an admin start or stop them.
//
// Live state lives in memory; the run history of periodic jobs is persisted
// through a Store so "last run" survives a restart. Single-node, like the job
// queue.
package crontab

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/NhProGamer/orion-drive/pkg/queue"
)

// Report lets a running task publish how far it got; total is 0 when unknown.
type Report func(done, total int64)

// RunFunc is a task's work. It returns how many items it handled (files purged,
// uploads cleaned…), shown as the run's result, and must stop promptly once ctx
// is cancelled.
type RunFunc func(ctx context.Context, report Report) (int64, error)

// Status is a task's state, or a finished run's outcome.
type Status string

const (
	StatusIdle    Status = "idle"
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
	StatusStopped Status = "stopped"
)

// Task kinds.
const (
	KindScheduled = "scheduled"
	KindOnce      = "once"
)

// historySize is how many past runs a task keeps.
const historySize = 10

// onceRetention is how long a finished one-off task stays listed.
const onceRetention = 24 * time.Hour

var (
	ErrUnknown = errors.New("unknown task")
	ErrRunning = errors.New("task already running")
)

// Run is one finished execution of a task.
type Run struct {
	StartedAt  time.Time `json:"started_at"`
	DurationMs int64     `json:"duration_ms"`
	Status     Status    `json:"status"`
	Count      int64     `json:"count"`
	Error      string    `json:"error,omitempty"`
}

// State is a snapshot of a task, as the admin view shows it.
type State struct {
	ID              string            `json:"id"`
	Type            string            `json:"type"`
	Kind            string            `json:"kind"`
	Params          map[string]string `json:"params,omitempty"`
	IntervalSeconds int64             `json:"interval_seconds,omitempty"`
	Status          Status            `json:"status"`
	Done            int64             `json:"done,omitempty"`
	Total           int64             `json:"total,omitempty"`
	ETASeconds      int               `json:"eta_seconds,omitempty"`
	StartedAt       *time.Time        `json:"started_at,omitempty"`
	NextRun         *time.Time        `json:"next_run,omitempty"`
	History         []Run             `json:"history"`
}

// Store persists periodic tasks' history (repository.SettingRepo fits).
type Store interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
}

type task struct {
	id, typ, kind string
	params        map[string]string
	interval      time.Duration
	run           RunFunc

	running     bool
	cancel      context.CancelFunc
	stopAsked   bool
	done, total int64
	started     time.Time
	next        time.Time
	history     []Run
}

// Scheduler runs and tracks the tasks.
type Scheduler struct {
	mu     sync.Mutex
	tasks  map[string]*task
	order  []string // periodic tasks, in registration order
	logger *slog.Logger
	store  Store
	base   context.Context // parent of every run; set by Start
}

// New builds an empty scheduler. store may be nil (history then lives in memory only).
func New(logger *slog.Logger, store Store) *Scheduler {
	return &Scheduler{tasks: map[string]*task{}, logger: logger, store: store, base: context.Background()}
}

func historyKey(id string) string { return "tasks.history." + id }

// Add registers a periodic task (ignored when interval <= 0 or run is nil) and
// loads its persisted history.
func (s *Scheduler) Add(name string, interval time.Duration, run RunFunc) {
	if interval <= 0 || run == nil {
		return
	}
	t := &task{id: name, typ: name, kind: KindScheduled, interval: interval, run: run}
	if s.store != nil {
		if raw, err := s.store.Get(context.Background(), historyKey(name)); err == nil && raw != "" {
			_ = json.Unmarshal([]byte(raw), &t.history)
		}
	}
	s.mu.Lock()
	s.tasks[name] = t
	s.order = append(s.order, name)
	s.mu.Unlock()
}

// Start launches the periodic timers; they stop, and running tasks are
// cancelled, when ctx is.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	s.base = ctx
	ids := append([]string(nil), s.order...)
	s.mu.Unlock()
	for _, id := range ids {
		go s.loop(ctx, id)
	}
}

func (s *Scheduler) loop(ctx context.Context, id string) {
	s.mu.Lock()
	t := s.tasks[id]
	t.next = time.Now().Add(t.interval)
	s.mu.Unlock()
	tick := time.NewTicker(t.interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.mu.Lock()
			t.next = time.Now().Add(t.interval)
			s.mu.Unlock()
			// A tick that lands while a manual run is going is skipped.
			_ = s.RunNow(id)
		}
	}
}

// RunNow starts a periodic task immediately, in the background.
func (s *Scheduler) RunNow(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok || t.kind != KindScheduled {
		return ErrUnknown
	}
	if t.running {
		return ErrRunning
	}
	s.startLocked(t)
	return nil
}

// Once starts a one-off task of the given type and returns its id. params
// describe it for the UI (e.g. the policy name).
func (s *Scheduler) Once(typ string, params map[string]string, run RunFunc) string {
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	t := &task{id: typ + "-" + hex.EncodeToString(buf), typ: typ, kind: KindOnce, params: params, run: run}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[t.id] = t
	s.startLocked(t)
	return t.id
}

// Stop cancels a running task. It reports whether there was one to stop.
func (s *Scheduler) Stop(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok || !t.running {
		return false
	}
	t.stopAsked = true
	t.cancel()
	return true
}

// startLocked marks t running and executes it in a goroutine. Caller holds mu.
func (s *Scheduler) startLocked(t *task) {
	ctx, cancel := context.WithCancel(s.base)
	t.running, t.cancel, t.stopAsked = true, cancel, false
	t.done, t.total, t.started = 0, 0, time.Now()
	go s.exec(ctx, t)
}

func (s *Scheduler) exec(ctx context.Context, t *task) {
	report := func(done, total int64) {
		s.mu.Lock()
		t.done, t.total = done, total
		s.mu.Unlock()
	}
	var (
		n   int64
		err error
	)
	func() {
		// One bad task must not take the process down.
		defer func() {
			if r := recover(); r != nil {
				err = errors.New("task panicked")
				s.logger.Error("maintenance task panicked", "task", t.id, "panic", r)
			}
		}()
		n, err = t.run(ctx, report)
	}()

	s.mu.Lock()
	run := Run{StartedAt: t.started, DurationMs: time.Since(t.started).Milliseconds(), Status: StatusDone, Count: n}
	switch {
	case t.stopAsked:
		run.Status = StatusStopped
	case err != nil:
		run.Status, run.Error = StatusFailed, err.Error()
	}
	t.history = append([]Run{run}, t.history...)
	if len(t.history) > historySize {
		t.history = t.history[:historySize]
	}
	release := t.cancel
	t.running, t.cancel = false, nil
	var raw []byte
	if t.kind == KindScheduled {
		raw, _ = json.Marshal(t.history)
	}
	s.mu.Unlock()
	release()

	if raw != nil && s.store != nil {
		if err := s.store.Set(context.Background(), historyKey(t.id), string(raw)); err != nil {
			s.logger.Warn("saving task history failed", "task", t.id, "error", err)
		}
	}
	if run.Status == StatusFailed {
		s.logger.Error("maintenance task failed", "task", t.id, "error", run.Error)
	} else {
		s.logger.Info("maintenance task finished", "task", t.id, "status", run.Status, "count", n)
	}
}

// List returns every task: periodic ones in registration order, then one-off
// ones newest first. Finished one-off tasks older than a day are dropped.
func (s *Scheduler) List() []State {
	s.mu.Lock()
	defer s.mu.Unlock()
	var once []*task
	for id, t := range s.tasks {
		if t.kind != KindOnce {
			continue
		}
		if !t.running && len(t.history) > 0 && time.Since(t.history[0].StartedAt) > onceRetention {
			delete(s.tasks, id)
			continue
		}
		once = append(once, t)
	}
	sort.Slice(once, func(i, j int) bool { return once[i].started.After(once[j].started) })
	out := make([]State, 0, len(s.order)+len(once))
	for _, id := range s.order {
		out = append(out, s.tasks[id].state())
	}
	for _, t := range once {
		out = append(out, t.state())
	}
	return out
}

// state snapshots t. Caller holds mu.
func (t *task) state() State {
	st := State{
		ID: t.id, Type: t.typ, Kind: t.kind, Params: t.params,
		IntervalSeconds: int64(t.interval / time.Second),
		Status:          StatusIdle,
		History:         append([]Run{}, t.history...),
	}
	if len(t.history) > 0 {
		st.Status = t.history[0].Status
	}
	if t.running {
		started := t.started
		st.Status, st.Done, st.Total, st.StartedAt = StatusRunning, t.done, t.total, &started
		st.ETASeconds = queue.Estimate(t.started, t.done, t.total)
	}
	if !t.next.IsZero() {
		next := t.next
		st.NextRun = &next
	}
	return st
}
