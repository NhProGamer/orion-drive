// Package crontab runs periodic background maintenance jobs on fixed intervals.
package crontab

import (
	"context"
	"log/slog"
	"time"
)

// Job is a named periodic task.
type Job struct {
	Name     string
	Interval time.Duration
	// Run performs the work and returns a short human-readable summary for logs.
	Run func(context.Context) (string, error)
}

// Scheduler runs registered jobs until its context is cancelled.
type Scheduler struct {
	jobs   []Job
	logger *slog.Logger
}

// New builds an empty scheduler.
func New(logger *slog.Logger) *Scheduler { return &Scheduler{logger: logger} }

// Add registers a job (ignored when interval <= 0 or run is nil).
func (s *Scheduler) Add(name string, interval time.Duration, run func(context.Context) (string, error)) {
	if interval <= 0 || run == nil {
		return
	}
	s.jobs = append(s.jobs, Job{Name: name, Interval: interval, Run: run})
}

// Start launches each job in its own goroutine; they stop when ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) {
	for _, j := range s.jobs {
		go s.loop(ctx, j)
	}
}

func (s *Scheduler) loop(ctx context.Context, j Job) {
	t := time.NewTicker(j.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			summary, err := j.Run(ctx)
			if err != nil {
				s.logger.Error("maintenance job failed", "job", j.Name, "error", err)
			} else if summary != "" {
				s.logger.Info("maintenance job", "job", j.Name, "result", summary)
			}
		}
	}
}
