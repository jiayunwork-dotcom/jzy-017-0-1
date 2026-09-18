package persistence

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/0001_init.sql
var migrationSQL string

// PostgresStore persists computations in PostgreSQL 16.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore connects, verifies connectivity and applies migrations.
func NewPostgresStore(ctx context.Context, url string) (*PostgresStore, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if _, err := pool.Exec(ctx, migrationSQL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("apply migrations: %w", echoError(err))
	}
	return &PostgresStore{pool: pool}, nil
}

// EnsureAtLeast attempts to wait until the database is reachable, retrying
// until the context expires.
func EnsureAtLeast(ctx context.Context, url string, wait time.Duration) (*PostgresStore, error) {
	deadline := time.Now().Add(wait)
	var lastErr error
	for {
		store, err := NewPostgresStore(ctx, url)
		if err == nil {
			return store, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return nil, lastErr
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// Save inserts all results in a single transaction.
func (s *PostgresStore) Save(ctx context.Context, results []Result) error {
	if len(results) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	const q = `
INSERT INTO computations (
	mode, batch_id, item_index, status,
	peak_density, peak_height, scale_height, zenith_angle,
	tec_u, result_peak_density, panels,
	error_field, error_message, raw, profile
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
RETURNING id, created_at`

	for i := range results {
		r := &results[i]
		raw := r.Raw
		if len(raw) == 0 {
			raw = []byte("{}")
		}
		err := tx.QueryRow(ctx, q,
			r.Mode, r.BatchID, r.ItemIndex, r.Status,
			floatArg(r.PeakDensity), floatArg(r.PeakHeight), floatArg(r.ScaleHeight), floatArg(r.ZenithAngle),
			floatArg(r.TECU), floatArg(r.ResultPeakDensity), intArg(r.Panels),
			r.ErrorField, r.ErrorMessage, string(raw), jsonbArg(r.ProfileJSON),
		).Scan(&r.ID, &r.CreatedAt)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Query retrieves computations filtered by f.
func (s *PostgresStore) Query(ctx context.Context, f Filter) ([]Result, error) {
	q := `SELECT id, created_at, mode, batch_id, item_index, status,
		peak_density, peak_height, scale_height, zenith_angle,
		tec_u, result_peak_density, panels,
		error_field, error_message, raw, profile
	FROM computations WHERE true`
	args := []any{}
	add := func(cond string, val any) {
		args = append(args, val)
		q += fmt.Sprintf(" AND %s $%d", cond, len(args))
	}
	if f.Status != "" {
		add("status =", f.Status)
	}
	if f.Mode != "" {
		add("mode =", f.Mode)
	}
	if f.BatchID != "" {
		add("batch_id =", f.BatchID)
	}
	if f.MinTECU != nil {
		add("tec_u >=", *f.MinTECU)
	}
	if f.MaxTECU != nil {
		add("tec_u <=", *f.MaxTECU)
	}
	if f.Since != nil {
		add("created_at >=", *f.Since)
	}
	if f.Until != nil {
		add("created_at <=", f.Until)
	}
	q += " ORDER BY created_at DESC, id DESC"
	if f.Limit > 0 {
		args = append(args, f.Limit)
		q += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if f.Offset > 0 {
		args = append(args, f.Offset)
		q += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Result
	for rows.Next() {
		var (
			r                                        Result
			pd, ph, sh, chi, tecu, resultPD         *float64
			panels                                   *int
			errField, errMsg, batchID, raw           string
			profile                                  []byte
		)
		if err := rows.Scan(
			&r.ID, &r.CreatedAt, &r.Mode, &batchID, &r.ItemIndex, &r.Status,
			&pd, &ph, &sh, &chi,
			&tecu, &resultPD, &panels,
			&errField, &errMsg, &raw, &profile,
		); err != nil {
			return nil, err
		}
		r.BatchID = batchID
		r.PeakDensity, r.PeakHeight, r.ScaleHeight, r.ZenithAngle = pd, ph, sh, chi
		r.TECU, r.ResultPeakDensity, r.Panels = tecu, resultPD, panels
		r.ErrorField, r.ErrorMessage = errField, errMsg
		r.Raw = []byte(raw)
		if profile != nil {
			r.ProfileJSON = profile
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Result{}
	}
	return out, nil
}

// Ping verifies connectivity.
func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Close releases the pool.
func (s *PostgresStore) Close() error {
	s.pool.Close()
	return nil
}

func floatArg(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func intArg(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func jsonbArg(v []byte) any {
	if len(v) == 0 {
		return nil
	}
	return string(v)
}

func echoError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return err
}
