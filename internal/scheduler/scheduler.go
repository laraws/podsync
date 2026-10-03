// Package scheduler serializes feed updates and coalesces duplicate triggers.
package scheduler

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/internal/config"
)

type UpdateFunc func(context.Context, *config.Feed) error
type Scheduler struct {
	ctx     context.Context
	cron    *cron.Cron
	update  UpdateFunc
	queue   chan *config.Feed
	initial []*config.Feed
	entries map[string]cron.EntryID
	mu      sync.Mutex
	pending map[string]bool
}

// New validates every schedule before workers start, and never mutates configuration.
func New(ctx context.Context, feeds map[string]*config.Feed, update UpdateFunc) (*Scheduler, error) {
	s := &Scheduler{ctx: ctx, cron: cron.New(), update: update, queue: make(chan *config.Feed, len(feeds)), entries: make(map[string]cron.EntryID, len(feeds)), pending: make(map[string]bool, len(feeds))}
	ids := make([]string, 0, len(feeds))
	for id := range feeds {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		feed := feeds[id]
		schedule := feed.CronSchedule
		if schedule == "" {
			schedule = fmt.Sprintf("@every %s", feed.UpdatePeriod)
			s.initial = append(s.initial, feed)
		}
		entry, err := s.cron.AddFunc(schedule, func() { s.enqueue(feed) })
		if err != nil {
			return nil, fmt.Errorf("schedule feed %s: %w", id, err)
		}
		s.entries[id] = entry
	}
	return s, nil
}
func (s *Scheduler) enqueue(feed *config.Feed) {
	if s.ctx.Err() != nil {
		return
	}
	s.mu.Lock()
	if s.pending[feed.ID] {
		s.mu.Unlock()
		return
	}
	s.pending[feed.ID] = true
	s.mu.Unlock()
	select {
	case s.queue <- feed:
	case <-s.ctx.Done():
		s.release(feed.ID)
	}
}
func (s *Scheduler) release(id string) { s.mu.Lock(); delete(s.pending, id); s.mu.Unlock() }
func (s *Scheduler) Run() error {
	s.cron.Start()
	// Wait for callbacks before returning. The queue is not closed: cancellation
	// owns termination, so no producer can race a send against channel closure.
	defer func() { <-s.cron.Stop().Done() }()
	for _, feed := range s.initial {
		s.enqueue(feed)
	}
	for {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case feed := <-s.queue:
			err := s.update(s.ctx, feed)
			s.release(feed.ID)
			if err != nil {
				log.WithError(err).WithField("feed_id", feed.ID).Error("feed update failed")
			} else {
				log.WithField("feed_id", feed.ID).Infof("next update: %s", s.cron.Entry(s.entries[feed.ID]).Next)
			}
		}
	}
}
