package scheduler

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// Scheduler wraps robfig/cron and tracks the next scheduled run.
type Scheduler struct {
	mu       sync.RWMutex
	c        *cron.Cron
	entryID  cron.EntryID
	cronExpr string
}

// New creates a stopped Scheduler. Call Start to activate it.
func New() *Scheduler {
	return &Scheduler{
		c: cron.New(),
	}
}

// SetJob replaces the current cron job with the given expression and callback.
// If the scheduler is already running, the new job takes effect immediately.
func (s *Scheduler) SetJob(expr string, fn func()) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.entryID != 0 {
		s.c.Remove(s.entryID)
	}

	id, err := s.c.AddFunc(expr, fn)
	if err != nil {
		return err
	}
	s.entryID = id
	s.cronExpr = expr
	slog.Info("scheduler: job set", "cron", expr)
	return nil
}

// AddJob adds a background job that fires on the given cron expression.
// Unlike SetJob, this does not replace the tracked scan job.
func (s *Scheduler) AddJob(expr string, fn func()) error {
	_, err := s.c.AddFunc(expr, fn)
	if err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", expr, err)
	}
	slog.Info("scheduler: background job added", "cron", expr)
	return nil
}

// Start begins the cron loop.
func (s *Scheduler) Start() {
	s.c.Start()
}

// Stop halts the cron loop gracefully.
func (s *Scheduler) Stop() {
	s.c.Stop()
}

// NextRunAt returns the next scheduled time, or nil if no job is set.
func (s *Scheduler) NextRunAt() *time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.entryID == 0 {
		return nil
	}
	entry := s.c.Entry(s.entryID)
	if entry.ID == 0 {
		return nil
	}
	t := entry.Next
	return &t
}

// CronExpr returns the current cron expression.
func (s *Scheduler) CronExpr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cronExpr
}
