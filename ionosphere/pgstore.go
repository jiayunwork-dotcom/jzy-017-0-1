package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	_ "github.com/lib/pq"
)

// schemaSQL creates the persistence table. Every calculation stores its
// four input parameters in queryable columns and the full result document
// (including the profile when one was produced) as JSONB.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS calculations (
    id                 BIGSERIAL PRIMARY KEY,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    kind               TEXT NOT NULL,
    batch_id           TEXT NOT NULL DEFAULT '',
    peak_density       DOUBLE PRECISION NOT NULL,
    peak_height        DOUBLE PRECISION NOT NULL,
    scale_height       DOUBLE PRECISION NOT NULL,
    solar_zenith_angle DOUBLE PRECISION NOT NULL,
    nmax               DOUBLE PRECISION NOT NULL,
    tec                DOUBLE PRECISION NOT NULL,
    tecu               DOUBLE PRECISION NOT NULL,
    result             JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_calculations_created_at ON calculations (created_at);
CREATE INDEX IF NOT EXISTS idx_calculations_kind ON calculations (kind);
`

// selectCols lists the columns mapped onto a Record.
const selectCols = `id, created_at, kind, batch_id,
	peak_density, peak_height, scale_height, solar_zenith_angle,
	nmax, tec, tecu, result`

// PGStore is a Store backed by PostgreSQL.
type PGStore struct {
	db *sql.DB
}

// NewPGStore wraps an open database handle.
func NewPGStore(db *sql.DB) *PGStore {
	return &PGStore{db: db}
}

// Migrate applies the schema (idempotent).
func Migrate(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, schemaSQL)
	return err
}

// Save inserts the record and fills in its ID and creation time.
func (p *PGStore) Save(ctx context.Context, rec *Record) error {
	const q = `INSERT INTO calculations
		(kind, batch_id, peak_density, peak_height, scale_height, solar_zenith_angle, nmax, tec, tecu, result)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at`
	return p.db.QueryRowContext(ctx, q,
		rec.Kind, rec.BatchID,
		rec.Input.PeakDensity, rec.Input.PeakHeight, rec.Input.ScaleHeight, rec.Input.SolarZenithAngle,
		rec.PeakDensity, rec.TEC, rec.TECU, string(rec.Result),
	).Scan(&rec.ID, &rec.CreatedAt)
}

// Get returns the full record (including the stored profile) or ErrNotFound.
func (p *PGStore) Get(ctx context.Context, id int64) (*Record, error) {
	rec, err := scanRecord(p.db.QueryRowContext(ctx,
		`SELECT `+selectCols+` FROM calculations WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// List returns the records matching f, newest pages ordered by id, with the
// profile member stripped from the stored result document.
func (p *PGStore) List(ctx context.Context, f HistoryFilter) ([]*Record, int, error) {
	var conds []string
	var args []any
	add := func(format string, v any) {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf(format, len(args)))
	}
	if f.Kind != "" {
		add("kind = $%d", f.Kind)
	}
	if f.Since != nil {
		add("created_at >= $%d", *f.Since)
	}
	if f.Until != nil {
		add("created_at <= $%d", *f.Until)
	}
	if f.MinTECU != nil {
		add("tecu >= $%d", *f.MinTECU)
	}
	if f.MaxTECU != nil {
		add("tecu <= $%d", *f.MaxTECU)
	}
	clause := ""
	if len(conds) > 0 {
		clause = " WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	if err := p.db.QueryRowContext(ctx,
		`SELECT count(*) FROM calculations`+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, f.Offset)
	q := fmt.Sprintf(`SELECT id, created_at, kind, batch_id,
		peak_density, peak_height, scale_height, solar_zenith_angle,
		nmax, tec, tecu, result - 'profile' AS result
		FROM calculations%s ORDER BY id LIMIT $%d OFFSET $%d`,
		clause, len(args)-1, len(args))
	rows, err := p.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*Record, 0, limit)
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, rec)
	}
	return out, total, rows.Err()
}

// Ping checks database liveness.
func (p *PGStore) Ping(ctx context.Context) error {
	return p.db.PingContext(ctx)
}

// Close closes the underlying handle.
func (p *PGStore) Close() error {
	return p.db.Close()
}

// scanRecord reads one row (sql.Row or sql.Rows) into a Record.
func scanRecord(row interface{ Scan(dest ...any) error }) (*Record, error) {
	var rec Record
	var result []byte
	err := row.Scan(&rec.ID, &rec.CreatedAt, &rec.Kind, &rec.BatchID,
		&rec.Input.PeakDensity, &rec.Input.PeakHeight, &rec.Input.ScaleHeight, &rec.Input.SolarZenithAngle,
		&rec.PeakDensity, &rec.TEC, &rec.TECU, &result)
	if err != nil {
		return nil, err
	}
	rec.Result = json.RawMessage(result)
	return &rec, nil
}
