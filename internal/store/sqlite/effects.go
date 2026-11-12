package sqlite

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/errs"
)

// effectRepo implements the outbox table. run_at 0 means "due now", so
// enqueueing needs no clock. A row is claimed while claim_token is set and
// claimed_until is in the future.
type effectRepo struct{ t *txn }

func (r effectRepo) Enqueue(ctx context.Context, kind core.EffectKind, key string, payload json.RawMessage) error {
	return r.enqueue(ctx, kind, key, payload, 0)
}

func (r effectRepo) EnqueueAt(ctx context.Context, kind core.EffectKind, key string, payload json.RawMessage, runAt time.Time) error {
	return r.enqueue(ctx, kind, key, payload, ms(runAt))
}

func (r effectRepo) enqueue(ctx context.Context, kind core.EffectKind, key string, payload json.RawMessage, runAt int64) error {
	if kind == "" || key == "" {
		return errs.ErrInvalid
	}
	if len(payload) == 0 {
		payload = json.RawMessage(`null`)
	}
	_, err := r.t.exec(ctx, `INSERT INTO effects(kind, key, payload, generation, attempts, run_at, created_at)
		VALUES (?, ?, ?, 1, 0, ?, ?)
		ON CONFLICT(kind, key) DO UPDATE SET payload = excluded.payload, generation = generation + 1, run_at = excluded.run_at`,
		kind, key, string(payload), runAt, time.Now().UnixMilli())
	if err == nil {
		r.t.enqueued = true
	}
	return err
}

func (r effectRepo) Cancel(ctx context.Context, kind core.EffectKind, key string) error {
	_, err := r.t.exec(ctx, `DELETE FROM effects WHERE kind = ? AND key = ?`, kind, key)
	return err
}

func (r effectRepo) ClaimDue(ctx context.Context, kind core.EffectKind, now time.Time, limit int) ([]core.Effect, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := r.t.q.QueryContext(ctx, `SELECT kind, key, payload, generation, attempts, run_at FROM effects
		WHERE kind = ? AND run_at <= ? AND (claim_token = '' OR claimed_until <= ?)
		ORDER BY run_at, created_at, key LIMIT ?`, kind, now.UnixMilli(), now.UnixMilli(), limit)
	due, err := collect(rows, err, func(s scanner) (core.Effect, error) {
		var e core.Effect
		var k, payload string
		var runAt int64
		if err := s.Scan(&k, &e.Key, &payload, &e.Generation, &e.Attempts, &runAt); err != nil {
			return e, mapErr(err)
		}
		e.Kind, e.Payload, e.RunAt = core.EffectKind(k), json.RawMessage(payload), tm(runAt)
		return e, nil
	})
	if err != nil {
		return nil, err
	}
	until := now.Add(core.ClaimTTL).UnixMilli()
	for i := range due {
		due[i].ClaimToken = newToken()
		if _, err := r.t.exec(ctx, `UPDATE effects SET claim_token = ?, claimed_until = ? WHERE kind = ? AND key = ?`,
			due[i].ClaimToken, until, kind, due[i].Key); err != nil {
			return nil, err
		}
	}
	return due, nil
}

func (r effectRepo) Holds(ctx context.Context, e core.Effect) (bool, error) {
	if e.ClaimToken == "" {
		return false, nil
	}
	var n int
	err := r.t.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM effects WHERE kind = ? AND key = ? AND claim_token = ?`,
		e.Kind, e.Key, e.ClaimToken).Scan(&n)
	return n > 0, mapErr(err)
}

func (r effectRepo) Complete(ctx context.Context, e core.Effect) (bool, error) {
	res, err := r.t.exec(ctx, `DELETE FROM effects WHERE kind = ? AND key = ? AND claim_token = ? AND generation = ?`,
		e.Kind, e.Key, e.ClaimToken, e.Generation)
	if err != nil {
		return false, err
	}
	if n, err := res.RowsAffected(); err != nil || n > 0 {
		return n > 0, err
	}
	// Signalled again during the run: release the claim, due now. If the
	// claim was lost to another token, this changes nothing.
	_, err = r.t.exec(ctx, `UPDATE effects SET claim_token = '', claimed_until = 0, run_at = 0, attempts = 0, last_error = ''
		WHERE kind = ? AND key = ? AND claim_token = ?`, e.Kind, e.Key, e.ClaimToken)
	return false, err
}

func (r effectRepo) Reschedule(ctx context.Context, e core.Effect, runAt time.Time, attempts int, lastErr string) error {
	_, err := r.t.exec(ctx, `UPDATE effects SET claim_token = '', claimed_until = 0, run_at = ?, attempts = ?, last_error = ?
		WHERE kind = ? AND key = ? AND claim_token = ?`, ms(runAt), attempts, lastErr, e.Kind, e.Key, e.ClaimToken)
	return err
}

func newToken() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
