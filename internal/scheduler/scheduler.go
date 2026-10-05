// Package scheduler runs corridor measurements on a fixed interval.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gofairway/fairway/internal/measure"
	"github.com/gofairway/fairway/internal/store"
	"github.com/gofairway/fairway/internal/webhook"
)

// Scheduler periodically measures all enabled corridors and records results.
type Scheduler struct {
	store      *store.Store
	horizon    *measure.HorizonClient
	dispatcher *webhook.Dispatcher
	logger     *slog.Logger

	sellAmount        float64
	interval          time.Duration
	degradedThreshold float64
	unusableThreshold float64
}

// New creates a Scheduler.
func New(
	st *store.Store,
	horizon *measure.HorizonClient,
	dispatcher *webhook.Dispatcher,
	logger *slog.Logger,
	sellAmount float64,
	interval time.Duration,
	degradedThreshold, unusableThreshold float64,
) *Scheduler {
	return &Scheduler{
		store:             st,
		horizon:           horizon,
		dispatcher:        dispatcher,
		logger:            logger,
		sellAmount:        sellAmount,
		interval:          interval,
		degradedThreshold: degradedThreshold,
		unusableThreshold: unusableThreshold,
	}
}

// Run starts the measurement loop; it blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	s.logger.Info("scheduler starting", "interval", s.interval)
	// Measure immediately on start, then on each tick.
	s.measureAll(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("scheduler stopped")
			return
		case <-ticker.C:
			s.measureAll(ctx)
		}
	}
}

// measureAll iterates over every enabled corridor and measures it concurrently.
func (s *Scheduler) measureAll(ctx context.Context) {
	corridors, err := s.store.ListCorridors(ctx)
	if err != nil {
		s.logger.Error("listing corridors", "err", err)
		return
	}
	if len(corridors) == 0 {
		s.logger.Warn("no enabled corridors found")
		return
	}

	var wg sync.WaitGroup
	for _, c := range corridors {
		c := c
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.measureOne(ctx, c)
		}()
	}
	wg.Wait()
}

// measureOne measures a single corridor, persists the result, and fires webhooks on state change.
func (s *Scheduler) measureOne(ctx context.Context, corridor store.Corridor) {
	log := s.logger.With("corridor", corridor.Name, "corridor_id", corridor.ID)

	result := s.horizon.Measure(ctx, corridor, s.sellAmount)

	newState := measure.ScoreState(result.LossPct, result.PathFound, s.degradedThreshold, s.unusableThreshold)

	m := store.Measurement{
		CorridorID:     corridor.ID,
		MeasuredAt:     result.MeasuredAt,
		SellAmount:     result.SellAmount,
		ReceivedAmount: result.ReceivedAmount,
		LossPct:        result.LossPct,
		IntegrityState: newState,
		PathFound:      result.PathFound,
		RawResponse:    result.RawResponse,
		ErrorMsg:       result.ErrorMsg,
	}

	measurementID, err := s.store.InsertMeasurement(ctx, m)
	if err != nil {
		log.Error("inserting measurement", "err", err)
		return
	}

	lossPctStr := "n/a"
	if result.LossPct != nil {
		lossPctStr = fmt.Sprintf("%.4f%%", *result.LossPct)
	}
	log.Info("measured",
		"state", newState,
		"path_found", result.PathFound,
		"loss_pct", lossPctStr,
	)

	// Detect state change against previous measurement.
	prev, err := s.store.LatestMeasurement(ctx, corridor.ID)
	if err != nil {
		log.Error("fetching previous measurement", "err", err)
		return
	}

	var prevState store.IntegrityState
	if prev == nil || prev.ID == measurementID {
		// First ever measurement — record as a transition from unknown.
		prevState = store.StateUnknown
	} else {
		prevState = prev.IntegrityState
	}

	if prevState != newState {
		log.Info("state changed", "from", prevState, "to", newState)

		event := store.StateChangeEvent{
			CorridorID:    corridor.ID,
			FromState:     prevState,
			ToState:       newState,
			MeasurementID: &measurementID,
			OccurredAt:    result.MeasuredAt,
		}
		eventID, err := s.store.InsertStateChange(ctx, event)
		if err != nil {
			log.Error("inserting state change event", "err", err)
			return
		}
		event.ID = eventID

		// Dispatch webhook (best-effort).
		if dispErr := s.dispatcher.Dispatch(ctx, corridor, event); dispErr != nil {
			log.Warn("webhook dispatch failed", "err", dispErr)
		} else {
			if markErr := s.store.MarkWebhookSent(ctx, eventID); markErr != nil {
				log.Warn("marking webhook sent", "err", markErr)
			}
		}
	}
}
