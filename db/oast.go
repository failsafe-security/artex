package db

import (
	"database/sql"
	"errors"
	"time"
)

// OAST (out-of-band application security testing) persistence.
//
// Tokens are minted per engagement and embedded in payloads. Any callback that
// reaches the listener is appended to oast_interactions; tokens and interactions
// are never updated or deleted by the runtime, so the evidence trail is
// append-only. Retention is an operator concern.

const oastSchema = `
CREATE TABLE IF NOT EXISTS oast_tokens (
    token       TEXT PRIMARY KEY,
    task_id     BIGINT NOT NULL DEFAULT 0,
    label       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_oast_tokens_task ON oast_tokens(task_id);

CREATE TABLE IF NOT EXISTS oast_interactions (
    id          BIGSERIAL PRIMARY KEY,
    token       TEXT NOT NULL,
    task_id     BIGINT NOT NULL DEFAULT 0,
    proto       TEXT NOT NULL DEFAULT 'http',
    method      TEXT NOT NULL DEFAULT '',
    host        TEXT NOT NULL DEFAULT '',
    path        TEXT NOT NULL DEFAULT '',
    peer        TEXT NOT NULL DEFAULT '',
    user_agent  TEXT NOT NULL DEFAULT '',
    body        TEXT NOT NULL DEFAULT '',
    received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_oast_interactions_token ON oast_interactions(token, id);
CREATE INDEX IF NOT EXISTS idx_oast_interactions_task ON oast_interactions(task_id, id);
`

// OASTToken is one minted callback token.
type OASTToken struct {
	Token     string    `json:"token"`
	TaskID    int64     `json:"task_id"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// OASTInteraction is one callback received by the listener. Body is stored as
// text and may be a truncated preview; Limit is applied before the insert.
type OASTInteraction struct {
	ID        int64     `json:"id"`
	Token     string    `json:"token"`
	TaskID    int64     `json:"task_id"`
	Proto     string    `json:"proto"`
	Method    string    `json:"method"`
	Host      string    `json:"host"`
	Path      string    `json:"path"`
	Peer      string    `json:"peer"`
	UserAgent string    `json:"user_agent"`
	Body      string    `json:"body"`
	Received  time.Time `json:"received_at"`
}

// EnsureOASTTables creates the OAST tables if they do not exist. The OAST
// feature is off by default, so this runs only when the operator enables it.
func (d *DB) EnsureOASTTables() error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if err := coordinateWithSchemaMigration(tx); err != nil {
		return err
	}
	if _, err := tx.Exec(oastSchema); err != nil {
		return err
	}
	return tx.Commit()
}

// InsertOASTToken records a freshly minted token. Tokens are random and primary
// keyed, so a collision fails the insert instead of replacing another token.
func (d *DB) InsertOASTToken(token string, taskID int64, label string, expiresAt time.Time) error {
	_, err := d.Exec(`
INSERT INTO oast_tokens(token, task_id, label, expires_at)
VALUES($1,$2,$3,$4)`, token, taskID, label, expiresAt)
	return err
}

// LookupOASTToken returns the token row, or ok=false when the token is unknown.
// Expiry is returned so the caller decides whether to accept a callback; an
// expired token keeps its row for the audit trail.
func (d *DB) LookupOASTToken(token string) (OASTToken, bool, error) {
	var t OASTToken
	err := d.QueryRow(`
SELECT token, task_id, label, created_at, expires_at
FROM oast_tokens WHERE token=$1`, token).
		Scan(&t.Token, &t.TaskID, &t.Label, &t.CreatedAt, &t.ExpiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return OASTToken{}, false, nil
		}
		return OASTToken{}, false, err
	}
	return t, true, nil
}

// InsertOASTInteraction appends one callback and returns its id.
func (d *DB) InsertOASTInteraction(in OASTInteraction) (int64, error) {
	var id int64
	err := d.QueryRow(`
INSERT INTO oast_interactions(token, task_id, proto, method, host, path, peer, user_agent, body)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING id`,
		in.Token, in.TaskID, in.Proto, in.Method, in.Host, in.Path, in.Peer, in.UserAgent, in.Body).Scan(&id)
	return id, err
}

// ListOASTInteractions returns callbacks for one token, oldest first, strictly
// after afterID. limit is clamped to 1..100.
func (d *DB) ListOASTInteractions(token string, afterID int64, limit int) ([]OASTInteraction, error) {
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := d.Query(`
SELECT id, token, task_id, proto, method, host, path, peer, user_agent, body, received_at
FROM oast_interactions
WHERE token=$1 AND id>$2
ORDER BY id
LIMIT $3`, token, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []OASTInteraction{}
	for rows.Next() {
		var in OASTInteraction
		if err := rows.Scan(&in.ID, &in.Token, &in.TaskID, &in.Proto, &in.Method, &in.Host,
			&in.Path, &in.Peer, &in.UserAgent, &in.Body, &in.Received); err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}
