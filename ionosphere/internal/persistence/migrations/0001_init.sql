CREATE TABLE IF NOT EXISTS computations (
    id                  BIGSERIAL PRIMARY KEY,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    mode                TEXT        NOT NULL,
    batch_id            TEXT        NOT NULL DEFAULT '',
    item_index          INTEGER     NOT NULL DEFAULT 0,
    status              TEXT        NOT NULL,
    peak_density        DOUBLE PRECISION,
    peak_height         DOUBLE PRECISION,
    scale_height        DOUBLE PRECISION,
    zenith_angle        DOUBLE PRECISION,
    tec_u               DOUBLE PRECISION,
    result_peak_density DOUBLE PRECISION,
    panels              INTEGER,
    error_field         TEXT        NOT NULL DEFAULT '',
    error_message       TEXT        NOT NULL DEFAULT '',
    raw                 JSONB       NOT NULL,
    profile             JSONB
);

CREATE INDEX IF NOT EXISTS idx_computations_created_at ON computations (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_computations_status     ON computations (status);
CREATE INDEX IF NOT EXISTS idx_computations_batch      ON computations (batch_id);
CREATE INDEX IF NOT EXISTS idx_computations_tec        ON computations (tec_u);
