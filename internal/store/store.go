// Package store provides the Postgres persistence layer for Fairway.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

// IntegrityState represents the health of a corridor at a point in time.
type IntegrityState string

const (
	StateUsable   IntegrityState = "usable"
	StateDegraded IntegrityState = "degraded"
	StateUnusable IntegrityState = "unusable"
	StateUnknown  IntegrityState = "unknown"
)

// Corridor mirrors the corridors table row.
type Corridor struct {
	ID              int
	Name            string
	SellAssetCode   string
	SellAssetIssuer string
	BuyAssetCode    string
	BuyAssetIssuer  string

	// Sell-leg anchor verification
	SellDomain          string
	SellAnchorMetadata  string
	SellVerifiedStatus  string

	// Buy-leg anchor verification
	BuyDomain          string
	BuyAnchorMetadata  string
	BuyVerifiedStatus  string

	// Shared
	VerificationDate *time.Time
	Enabled          bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// EffectiveVerifiedStatus returns the worst-case verified status across both legs.
// A corridor is only as trustworthy as its least-verified asset:
// unverifiable > unknown > pending > live (where unverifiable is worst).
func (c Corridor) EffectiveVerifiedStatus() string {
	rank := map[string]int{
		"unverifiable": 0,
		"unknown":      1,
		"pending":      2,
		"live":         3,
	}
	sellR, ok := rank[c.SellVerifiedStatus]
	if !ok {
		sellR = rank["unknown"]
	}
	buyR, ok := rank[c.BuyVerifiedStatus]
	if !ok {
		buyR = rank["unknown"]
	}
	if sellR <= buyR {
		return c.SellVerifiedStatus
	}
	return c.BuyVerifiedStatus
}

// Measurement mirrors the measurements table row.
type Measurement struct {
	ID             int64
	CorridorID     int
	MeasuredAt     time.Time
	SellAmount     float64
	ReceivedAmount *float64
	LossPct        *float64
	ReferenceRate  *float64
	ReferenceSrc   *string
	IntegrityState IntegrityState
	PathFound      bool
	RawResponse    json.RawMessage
	ErrorMsg       *string
}

// StateChangeEvent mirrors the state_change_events table row.
type StateChangeEvent struct {
	ID            int64
	CorridorID    int
	FromState     IntegrityState
	ToState       IntegrityState
	MeasurementID *int64
	OccurredAt    time.Time
	WebhookSent   bool
	WebhookSentAt *time.Time
}

// Store wraps a *sql.DB and exposes domain-level operations.
type Store struct {
	db *sql.DB
}

// New opens a Postgres connection and returns a Store.
func New(databaseURL string) (*Store, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("opening db: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	return &Store{db: db}, nil
}

// Ping verifies the connection is alive.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// Close closes the underlying db pool.
func (s *Store) Close() error {
	return s.db.Close()
}

// UpsertCorridor inserts or updates a corridor by name, returning the corridor ID.
// "Upsert" here is: insert if no row with that name exists, otherwise update mutable fields.
func (s *Store) UpsertCorridor(ctx context.Context, c Corridor) (int, error) {
	const q = `
INSERT INTO corridors
    (name, sell_asset_code, sell_asset_issuer, buy_asset_code, buy_asset_issuer,
     sell_domain, sell_anchor_metadata, sell_verified_status,
     buy_domain, buy_anchor_metadata, buy_verified_status,
     verification_date, enabled, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NOW())
ON CONFLICT (name) DO UPDATE SET
    sell_asset_code      = EXCLUDED.sell_asset_code,
    sell_asset_issuer    = EXCLUDED.sell_asset_issuer,
    buy_asset_code       = EXCLUDED.buy_asset_code,
    buy_asset_issuer     = EXCLUDED.buy_asset_issuer,
    sell_domain          = EXCLUDED.sell_domain,
    sell_anchor_metadata = EXCLUDED.sell_anchor_metadata,
    sell_verified_status = EXCLUDED.sell_verified_status,
    buy_domain           = EXCLUDED.buy_domain,
    buy_anchor_metadata  = EXCLUDED.buy_anchor_metadata,
    buy_verified_status  = EXCLUDED.buy_verified_status,
    verification_date    = EXCLUDED.verification_date,
    enabled              = EXCLUDED.enabled,
    updated_at           = NOW()
RETURNING id`

	var id int
	err := s.db.QueryRowContext(ctx, q,
		c.Name, c.SellAssetCode, c.SellAssetIssuer,
		c.BuyAssetCode, c.BuyAssetIssuer,
		c.SellDomain, c.SellAnchorMetadata, c.SellVerifiedStatus,
		c.BuyDomain, c.BuyAnchorMetadata, c.BuyVerifiedStatus,
		c.VerificationDate, c.Enabled,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert corridor %q: %w", c.Name, err)
	}
	return id, nil
}

// ListCorridors returns all enabled corridors.
func (s *Store) ListCorridors(ctx context.Context) ([]Corridor, error) {
	const q = `
SELECT id, name, sell_asset_code, sell_asset_issuer, buy_asset_code, buy_asset_issuer,
       sell_domain, sell_anchor_metadata, sell_verified_status,
       buy_domain, buy_anchor_metadata, buy_verified_status,
       verification_date, enabled, created_at, updated_at
FROM corridors
WHERE enabled = TRUE
ORDER BY id`

	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list corridors: %w", err)
	}
	defer rows.Close()

	var out []Corridor
	for rows.Next() {
		var c Corridor
		if err := rows.Scan(
			&c.ID, &c.Name, &c.SellAssetCode, &c.SellAssetIssuer,
			&c.BuyAssetCode, &c.BuyAssetIssuer,
			&c.SellDomain, &c.SellAnchorMetadata, &c.SellVerifiedStatus,
			&c.BuyDomain, &c.BuyAnchorMetadata, &c.BuyVerifiedStatus,
			&c.VerificationDate, &c.Enabled, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning corridor: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCorridor returns a single corridor by ID (enabled or not).
func (s *Store) GetCorridor(ctx context.Context, id int) (*Corridor, error) {
	const q = `
SELECT id, name, sell_asset_code, sell_asset_issuer, buy_asset_code, buy_asset_issuer,
       sell_domain, sell_anchor_metadata, sell_verified_status,
       buy_domain, buy_anchor_metadata, buy_verified_status,
       verification_date, enabled, created_at, updated_at
FROM corridors WHERE id = $1`

	var c Corridor
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&c.ID, &c.Name, &c.SellAssetCode, &c.SellAssetIssuer,
		&c.BuyAssetCode, &c.BuyAssetIssuer,
		&c.SellDomain, &c.SellAnchorMetadata, &c.SellVerifiedStatus,
		&c.BuyDomain, &c.BuyAnchorMetadata, &c.BuyVerifiedStatus,
		&c.VerificationDate, &c.Enabled, &c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get corridor %d: %w", id, err)
	}
	return &c, nil
}

// InsertMeasurement persists a measurement and returns its ID.
func (s *Store) InsertMeasurement(ctx context.Context, m Measurement) (int64, error) {
	const q = `
INSERT INTO measurements
    (corridor_id, measured_at, sell_amount, received_amount, loss_pct,
     reference_rate, reference_src, integrity_state, path_found, raw_response, error_msg)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
RETURNING id`

	var rawJSON interface{}
	if m.RawResponse != nil {
		rawJSON = m.RawResponse
	}

	var id int64
	err := s.db.QueryRowContext(ctx, q,
		m.CorridorID, m.MeasuredAt, m.SellAmount,
		m.ReceivedAmount, m.LossPct,
		m.ReferenceRate, m.ReferenceSrc,
		string(m.IntegrityState), m.PathFound,
		rawJSON, m.ErrorMsg,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert measurement: %w", err)
	}
	return id, nil
}

// LatestMeasurement returns the most recent measurement for a corridor, or nil if none.
func (s *Store) LatestMeasurement(ctx context.Context, corridorID int) (*Measurement, error) {
	const q = `
SELECT id, corridor_id, measured_at, sell_amount, received_amount, loss_pct,
       reference_rate, reference_src, integrity_state, path_found, raw_response, error_msg
FROM measurements
WHERE corridor_id = $1
ORDER BY measured_at DESC
LIMIT 1`

	return s.scanMeasurement(s.db.QueryRowContext(ctx, q, corridorID))
}

// ListMeasurements returns measurements for a corridor, newest first, with optional limit.
func (s *Store) ListMeasurements(ctx context.Context, corridorID, limit, offset int) ([]Measurement, error) {
	const q = `
SELECT id, corridor_id, measured_at, sell_amount, received_amount, loss_pct,
       reference_rate, reference_src, integrity_state, path_found, raw_response, error_msg
FROM measurements
WHERE corridor_id = $1
ORDER BY measured_at DESC
LIMIT $2 OFFSET $3`

	rows, err := s.db.QueryContext(ctx, q, corridorID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list measurements: %w", err)
	}
	defer rows.Close()

	var out []Measurement
	for rows.Next() {
		m, err := s.scanMeasurement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// scanMeasurement scans a single row into a Measurement (works for *sql.Row and *sql.Rows).
type rower interface {
	Scan(dest ...any) error
}

func (s *Store) scanMeasurement(row rower) (*Measurement, error) {
	var m Measurement
	var state string
	err := row.Scan(
		&m.ID, &m.CorridorID, &m.MeasuredAt, &m.SellAmount,
		&m.ReceivedAmount, &m.LossPct,
		&m.ReferenceRate, &m.ReferenceSrc,
		&state, &m.PathFound, &m.RawResponse, &m.ErrorMsg,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scanning measurement: %w", err)
	}
	m.IntegrityState = IntegrityState(state)
	return &m, nil
}

// InsertStateChange records a corridor state transition.
func (s *Store) InsertStateChange(ctx context.Context, e StateChangeEvent) (int64, error) {
	const q = `
INSERT INTO state_change_events
    (corridor_id, from_state, to_state, measurement_id, occurred_at)
VALUES ($1,$2,$3,$4,$5)
RETURNING id`

	var id int64
	err := s.db.QueryRowContext(ctx, q,
		e.CorridorID, string(e.FromState), string(e.ToState),
		e.MeasurementID, e.OccurredAt,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert state change: %w", err)
	}
	return id, nil
}

// PendingWebhooks returns state change events that have not had a webhook sent yet.
func (s *Store) PendingWebhooks(ctx context.Context) ([]StateChangeEvent, error) {
	const q = `
SELECT id, corridor_id, from_state, to_state, measurement_id, occurred_at, webhook_sent, webhook_sent_at
FROM state_change_events
WHERE webhook_sent = FALSE
ORDER BY occurred_at ASC`

	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("pending webhooks: %w", err)
	}
	defer rows.Close()

	var out []StateChangeEvent
	for rows.Next() {
		var e StateChangeEvent
		var from, to string
		if err := rows.Scan(
			&e.ID, &e.CorridorID, &from, &to,
			&e.MeasurementID, &e.OccurredAt, &e.WebhookSent, &e.WebhookSentAt,
		); err != nil {
			return nil, fmt.Errorf("scanning state change event: %w", err)
		}
		e.FromState = IntegrityState(from)
		e.ToState = IntegrityState(to)
		out = append(out, e)
	}
	return out, rows.Err()
}

// MarkWebhookSent marks a state_change_event as having had its webhook dispatched.
func (s *Store) MarkWebhookSent(ctx context.Context, eventID int64) error {
	const q = `
UPDATE state_change_events
SET webhook_sent = TRUE, webhook_sent_at = NOW()
WHERE id = $1`
	_, err := s.db.ExecContext(ctx, q, eventID)
	return err
}
