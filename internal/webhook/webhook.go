// Package webhook dispatches plain JSON POST notifications on corridor state changes.
// Formatting for specific platforms (Discord, Slack, etc.) is intentionally left
// to future contributors — this package just sends raw JSON.
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gofairway/fairway/internal/store"
)

// Payload is the JSON body posted to the webhook URL.
type Payload struct {
	Event       string    `json:"event"`        // always "corridor.state_change"
	OccurredAt  time.Time `json:"occurred_at"`
	CorridorID  int       `json:"corridor_id"`
	CorridorName string   `json:"corridor_name"`
	FromState   string    `json:"from_state"`
	ToState     string    `json:"to_state"`
	MeasurementID *int64  `json:"measurement_id,omitempty"`
}

// Dispatcher sends webhook payloads to a configured URL.
type Dispatcher struct {
	url        string
	httpClient *http.Client
	logger     *slog.Logger
}

// NewDispatcher creates a Dispatcher. If webhookURL is empty, Dispatch is a no-op.
func NewDispatcher(webhookURL string, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		url:    webhookURL,
		logger: logger,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Dispatch sends the state-change event as a JSON POST.
// Returns nil if the webhook URL is not configured.
func (d *Dispatcher) Dispatch(ctx context.Context, corridor store.Corridor, event store.StateChangeEvent) error {
	if d.url == "" {
		return nil
	}

	payload := Payload{
		Event:         "corridor.state_change",
		OccurredAt:    event.OccurredAt,
		CorridorID:    corridor.ID,
		CorridorName:  corridor.Name,
		FromState:     string(event.FromState),
		ToState:       string(event.ToState),
		MeasurementID: event.MeasurementID,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("posting webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook target returned HTTP %d", resp.StatusCode)
	}

	d.logger.Info("webhook dispatched",
		"corridor_id", corridor.ID,
		"from", event.FromState,
		"to", event.ToState,
		"http_status", resp.StatusCode,
	)
	return nil
}
