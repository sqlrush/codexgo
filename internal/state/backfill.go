package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// GetBackfillState returns the persisted backfill lifecycle state, ensuring the
// singleton row exists first, mirroring the Rust `get_backfill_state`.
func (r *StateRuntime) GetBackfillState(ctx context.Context) (BackfillState, error) {
	if err := r.ensureBackfillStateRow(ctx); err != nil {
		return BackfillState{}, err
	}
	row := r.pool.QueryRowContext(ctx, `
SELECT status, last_watermark, last_success_at
FROM backfill_state
WHERE id = 1
`)
	var (
		status        string
		watermark     sql.NullString
		lastSuccessAt sql.NullInt64
	)
	if err := row.Scan(&status, &watermark, &lastSuccessAt); err != nil {
		return BackfillState{}, fmt.Errorf("get backfill state: %w", err)
	}
	parsed, err := ParseBackfillStatus(status)
	if err != nil {
		return BackfillState{}, fmt.Errorf("get backfill state: %w", err)
	}
	state := BackfillState{Status: parsed, LastWatermark: nullStringToPtr(watermark)}
	if lastSuccessAt.Valid {
		ts, terr := epochSecondsToDatetime(lastSuccessAt.Int64)
		if terr != nil {
			return BackfillState{}, fmt.Errorf("get backfill state: %w", terr)
		}
		state.LastSuccessAt = &ts
	}
	return state, nil
}

// TryClaimBackfill attempts to claim the singleton backfill worker slot,
// returning true on success, mirroring the Rust `try_claim_backfill`. A claim
// succeeds when backfill is not complete and either not running or its lease has
// expired (older than leaseSeconds).
func (r *StateRuntime) TryClaimBackfill(ctx context.Context, leaseSeconds int64) (bool, error) {
	if err := r.ensureBackfillStateRow(ctx); err != nil {
		return false, err
	}
	now := time.Now().UTC().Unix()
	lease := leaseSeconds
	if lease < 0 {
		lease = 0
	}
	leaseCutoff := saturatingSub(now, lease)
	res, err := r.pool.ExecContext(ctx, `
UPDATE backfill_state
SET status = ?, updated_at = ?
WHERE id = 1
  AND status != ?
  AND (status != ? OR updated_at <= ?)
`,
		BackfillStatusRunning.String(), now,
		BackfillStatusComplete.String(),
		BackfillStatusRunning.String(), leaseCutoff,
	)
	if err != nil {
		return false, fmt.Errorf("try claim backfill: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return affected == 1, nil
}

// MarkBackfillRunning marks rollout metadata backfill as running.
func (r *StateRuntime) MarkBackfillRunning(ctx context.Context) error {
	if err := r.ensureBackfillStateRow(ctx); err != nil {
		return err
	}
	_, err := r.pool.ExecContext(ctx, `
UPDATE backfill_state
SET status = ?, updated_at = ?
WHERE id = 1
`, BackfillStatusRunning.String(), time.Now().UTC().Unix())
	if err != nil {
		return fmt.Errorf("mark backfill running: %w", err)
	}
	return nil
}

// CheckpointBackfill persists backfill progress at the given watermark.
func (r *StateRuntime) CheckpointBackfill(ctx context.Context, watermark string) error {
	if err := r.ensureBackfillStateRow(ctx); err != nil {
		return err
	}
	_, err := r.pool.ExecContext(ctx, `
UPDATE backfill_state
SET status = ?, last_watermark = ?, updated_at = ?
WHERE id = 1
`, BackfillStatusRunning.String(), watermark, time.Now().UTC().Unix())
	if err != nil {
		return fmt.Errorf("checkpoint backfill: %w", err)
	}
	return nil
}

// MarkBackfillComplete marks backfill complete, optionally advancing the
// watermark, mirroring the Rust `mark_backfill_complete`.
func (r *StateRuntime) MarkBackfillComplete(ctx context.Context, lastWatermark *string) error {
	if err := r.ensureBackfillStateRow(ctx); err != nil {
		return err
	}
	now := time.Now().UTC().Unix()
	_, err := r.pool.ExecContext(ctx, `
UPDATE backfill_state
SET
    status = ?,
    last_watermark = COALESCE(?, last_watermark),
    last_success_at = ?,
    updated_at = ?
WHERE id = 1
`, BackfillStatusComplete.String(), nullableString(lastWatermark), now, now)
	if err != nil {
		return fmt.Errorf("mark backfill complete: %w", err)
	}
	return nil
}

func (r *StateRuntime) ensureBackfillStateRow(ctx context.Context) error {
	return ensureBackfillStateRow(ctx, r.pool)
}

func saturatingSub(a, b int64) int64 {
	const maxI64 = int64(^uint64(0) >> 1)
	const minI64 = -maxI64 - 1
	diff := a - b
	if b < 0 && diff < a {
		return maxI64
	}
	if b > 0 && diff > a {
		return minI64
	}
	return diff
}
