package webhook

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofairway/fairway/internal/store"
)

func TestDispatcher_NoURL(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := NewDispatcher("", logger)

	corridor := store.Corridor{ID: 1, Name: "NGNC/USDC"}
	event := store.StateChangeEvent{
		ID:         1,
		CorridorID: 1,
		FromState:  store.StateUsable,
		ToState:    store.StateDegraded,
		OccurredAt: time.Now(),
	}

	err := d.Dispatch(context.Background(), corridor, event)
	if err != nil {
		t.Fatalf("expected nil error when webhook URL is empty, got: %v", err)
	}
}

func TestDispatcher_Success(t *testing.T) {
	var receivedPayload Payload
	var receivedContentType string
	var receivedMethod string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedContentType = r.Header.Get("Content-Type")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if err := json.Unmarshal(body, &receivedPayload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := NewDispatcher(ts.URL, logger)

	measurementID := int64(42)
	now := time.Now().Truncate(time.Millisecond)
	corridor := store.Corridor{ID: 10, Name: "USDC/EURC"}
	event := store.StateChangeEvent{
		ID:            5,
		CorridorID:    10,
		FromState:     store.StateDegraded,
		ToState:       store.StateUsable,
		MeasurementID: &measurementID,
		OccurredAt:    now,
	}

	err := d.Dispatch(context.Background(), corridor, event)
	if err != nil {
		t.Fatalf("unexpected error from Dispatch: %v", err)
	}

	if receivedMethod != http.MethodPost {
		t.Errorf("expected HTTP POST, got %s", receivedMethod)
	}
	if receivedContentType != "application/json" {
		t.Errorf("expected application/json Content-Type, got %s", receivedContentType)
	}
	if receivedPayload.Event != "corridor.state_change" {
		t.Errorf("expected event 'corridor.state_change', got %s", receivedPayload.Event)
	}
	if receivedPayload.CorridorID != 10 {
		t.Errorf("expected corridor_id 10, got %d", receivedPayload.CorridorID)
	}
	if receivedPayload.CorridorName != "USDC/EURC" {
		t.Errorf("expected corridor_name 'USDC/EURC', got %s", receivedPayload.CorridorName)
	}
	if receivedPayload.FromState != "degraded" || receivedPayload.ToState != "usable" {
		t.Errorf("expected from=degraded, to=usable, got from=%s, to=%s", receivedPayload.FromState, receivedPayload.ToState)
	}
	if receivedPayload.MeasurementID == nil || *receivedPayload.MeasurementID != 42 {
		t.Errorf("expected measurement_id 42, got %v", receivedPayload.MeasurementID)
	}
}

func TestDispatcher_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := NewDispatcher(ts.URL, logger)

	corridor := store.Corridor{ID: 1, Name: "NGNC/USDC"}
	event := store.StateChangeEvent{
		CorridorID: 1,
		FromState:  store.StateUnknown,
		ToState:    store.StateUsable,
		OccurredAt: time.Now(),
	}

	err := d.Dispatch(context.Background(), corridor, event)
	if err == nil {
		t.Fatal("expected error on HTTP 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("expected error message to mention 'HTTP 500', got: %v", err)
	}
}

func TestDispatcher_NetworkError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ts.Close() // Close immediately to force connection failure

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := NewDispatcher(ts.URL, logger)

	corridor := store.Corridor{ID: 1, Name: "NGNC/USDC"}
	event := store.StateChangeEvent{
		CorridorID: 1,
		FromState:  store.StateUsable,
		ToState:    store.StateUnusable,
		OccurredAt: time.Now(),
	}

	err := d.Dispatch(context.Background(), corridor, event)
	if err == nil {
		t.Fatal("expected network error on unreachable server, got nil")
	}
}

func TestDispatcher_ContextCanceled(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := NewDispatcher(ts.URL, logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	corridor := store.Corridor{ID: 1, Name: "NGNC/USDC"}
	event := store.StateChangeEvent{
		CorridorID: 1,
		FromState:  store.StateUsable,
		ToState:    store.StateUnusable,
		OccurredAt: time.Now(),
	}

	err := d.Dispatch(ctx, corridor, event)
	if err == nil {
		t.Fatal("expected context canceled error, got nil")
	}
}
