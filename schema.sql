-- Fairway database schema
-- Run once against a fresh Postgres database to initialise all tables.

CREATE TABLE IF NOT EXISTS corridors (
    id                      SERIAL PRIMARY KEY,
    name                    TEXT        NOT NULL UNIQUE,   -- e.g. "NGNC/USDC"
    sell_asset_code         TEXT        NOT NULL,
    sell_asset_issuer       TEXT        NOT NULL,
    buy_asset_code          TEXT        NOT NULL,
    buy_asset_issuer        TEXT        NOT NULL,          -- empty string for native XLM

    -- Sell-leg anchor verification (independently assessed per asset)
    sell_domain             TEXT        NOT NULL DEFAULT '',
    sell_anchor_metadata    TEXT        NOT NULL DEFAULT 'none',   -- 'full' | 'none'
    sell_verified_status    TEXT        NOT NULL DEFAULT 'unknown', -- 'live' | 'pending' | 'unverifiable' | 'unknown'

    -- Buy-leg anchor verification (independently assessed per asset)
    buy_domain              TEXT        NOT NULL DEFAULT '',
    buy_anchor_metadata     TEXT        NOT NULL DEFAULT 'none',   -- 'full' | 'none'
    buy_verified_status     TEXT        NOT NULL DEFAULT 'unknown', -- 'live' | 'pending' | 'unverifiable' | 'unknown'

    -- Shared: all seed assets verified on the same date (3rd Oct 2026)
    verification_date       DATE,

    enabled                 BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- integrity_state values: 'usable' | 'degraded' | 'unusable' | 'unknown'
CREATE TABLE IF NOT EXISTS measurements (
    id              BIGSERIAL   PRIMARY KEY,
    corridor_id     INT         NOT NULL REFERENCES corridors(id) ON DELETE CASCADE,
    measured_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sell_amount     NUMERIC(28, 7) NOT NULL,
    received_amount NUMERIC(28, 7),                   -- NULL when no path found
    loss_pct        NUMERIC(10, 4),                   -- NULL when no path found
    reference_rate  NUMERIC(28, 10),                  -- independent FX reference
    reference_src   TEXT,                             -- e.g. "exchangerate.host"
    integrity_state TEXT        NOT NULL DEFAULT 'unknown',
    path_found      BOOLEAN     NOT NULL DEFAULT FALSE,
    raw_response    JSONB,                            -- full Horizon pathfinding response
    error_msg       TEXT                              -- set when measurement itself errored
);

CREATE INDEX IF NOT EXISTS idx_measurements_corridor_time
    ON measurements (corridor_id, measured_at DESC);

CREATE TABLE IF NOT EXISTS state_change_events (
    id              BIGSERIAL   PRIMARY KEY,
    corridor_id     INT         NOT NULL REFERENCES corridors(id) ON DELETE CASCADE,
    from_state      TEXT        NOT NULL,
    to_state        TEXT        NOT NULL,
    measurement_id  BIGINT      REFERENCES measurements(id),
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    webhook_sent    BOOLEAN     NOT NULL DEFAULT FALSE,
    webhook_sent_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_state_changes_corridor
    ON state_change_events (corridor_id, occurred_at DESC);
