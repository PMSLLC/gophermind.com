package blackboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gophermind/gophermind-lib/briefv2/db"
)

// SQLite is the blackboard over the shared database. Close does nothing: the
// *sql.DB belongs to the caller because the ledger uses it too.
type SQLite struct {
	db   *sql.DB
	now  func() time.Time
	poll time.Duration
}

var _ Blackboard = (*SQLite)(nil)

func NewSQLite(d *sql.DB) *SQLite {
	return &SQLite{db: d, now: func() time.Time { return time.Now().UTC() }, poll: 250 * time.Millisecond}
}

var allowed = map[Status][]Status{
	StatusPending:       {StatusReady, StatusNeedsRevision},
	StatusReady:         {StatusClaimed, StatusNeedsRevision},
	StatusClaimed:       {StatusInProgress, StatusReady},
	StatusInProgress:    {StatusVerified, StatusFailed, StatusNeedsRevision, StatusReady},
	StatusNeedsRevision: {StatusReady, StatusEscalated},
	StatusEscalated:     {StatusReady, StatusFailed},
	StatusVerified:      {StatusNeedsRevision},
}

func transitionAllowed(from, to Status) bool {
	for _, s := range allowed[from] {
		if s == to {
			return true
		}
	}
	return false
}

const rowCols = `run_id, node_id, status, revision, claim_worker, claim_at, heartbeat_at, attempts, result, updated_at`

type scanner interface{ Scan(dest ...any) error }

func scanRow(sc scanner) (Row, error) {
	var (
		r                           Row
		status, worker, claimAt, hb string
		attempts, result, updated   string
	)
	if err := sc.Scan(&r.RunID, &r.NodeID, &status, &r.Revision, &worker, &claimAt, &hb, &attempts, &result, &updated); err != nil {
		return Row{}, err
	}
	r.Status = Status(status)
	r.HeartbeatAt, _ = db.ParseTS(hb)
	r.UpdatedAt, _ = db.ParseTS(updated)
	if worker != "" {
		at, _ := db.ParseTS(claimAt)
		r.Claim = &Claim{Worker: worker, ClaimedAt: at}
	}
	if attempts != "" && attempts != "[]" {
		if err := json.Unmarshal([]byte(attempts), &r.Attempts); err != nil {
			return Row{}, err
		}
	}
	if result != "" {
		r.Result = &Result{}
		if err := json.Unmarshal([]byte(result), r.Result); err != nil {
			return Row{}, err
		}
	}
	return r, nil
}

func (s *SQLite) ts() string { return db.TS(s.now()) }

func (s *SQLite) emit(ctx context.Context, kind EventKind, runID, nodeID string) {
	// A lost change notification must never fail the write it describes.
	_, _ = s.db.ExecContext(ctx, `INSERT INTO events (run_id, node_id, kind, at) VALUES (?, ?, ?, ?)`, runID, nodeID, string(kind), s.ts())
}

func (s *SQLite) exists(ctx context.Context, runID, nodeID string) bool {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM rows WHERE run_id = ? AND node_id = ?`, runID, nodeID).Scan(&n)
	return n > 0
}

func (s *SQLite) InitRun(ctx context.Context, runID string, nodeIDs []string, waves map[string]int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range nodeIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO rows (run_id, node_id, status, wave, updated_at) VALUES (?, ?, 'pending', ?, ?)`,
			runID, id, waves[id], s.ts()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) Get(ctx context.Context, runID, nodeID string) (Row, error) {
	r, err := scanRow(s.db.QueryRowContext(ctx, `SELECT `+rowCols+` FROM rows WHERE run_id = ? AND node_id = ?`, runID, nodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return Row{}, ErrNotFound
	}
	return r, err
}

func (s *SQLite) List(ctx context.Context, runID string, f Filter) ([]Row, error) {
	q := `SELECT ` + rowCols + ` FROM rows WHERE run_id = ?`
	args := []any{runID}
	if len(f.Statuses) > 0 {
		q += ` AND status IN (` + strings.TrimSuffix(strings.Repeat("?,", len(f.Statuses)), ",") + `)`
		for _, st := range f.Statuses {
			args = append(args, string(st))
		}
	}
	if f.Wave != nil {
		q += ` AND wave = ?`
		args = append(args, *f.Wave)
	}
	q += ` ORDER BY node_id`
	rs, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []Row
	for rs.Next() {
		r, err := scanRow(rs)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rs.Err()
}

func (s *SQLite) Claim(ctx context.Context, runID, nodeID, worker string) (bool, error) {
	now := s.ts()
	res, err := s.db.ExecContext(ctx,
		`UPDATE rows SET status = 'claimed', claim_worker = ?, claim_at = ?, heartbeat_at = ?, updated_at = ?
		 WHERE run_id = ? AND node_id = ? AND status = 'ready'`,
		worker, now, now, now, runID, nodeID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 1 {
		s.emit(ctx, EventStatus, runID, nodeID)
	}
	return n == 1, nil
}

func (s *SQLite) Heartbeat(ctx context.Context, runID, nodeID, worker string) error {
	now := s.ts()
	res, err := s.db.ExecContext(ctx,
		`UPDATE rows SET heartbeat_at = ?, updated_at = ?
		 WHERE run_id = ? AND node_id = ? AND claim_worker = ? AND status IN ('claimed', 'in_progress')`,
		now, now, runID, nodeID, worker)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if !s.exists(ctx, runID, nodeID) {
			return ErrNotFound
		}
		return ErrNotOwner
	}
	return nil
}

func (s *SQLite) Release(ctx context.Context, runID, nodeID, worker string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE rows SET status = 'ready', claim_worker = '', claim_at = '', heartbeat_at = '', updated_at = ?
		 WHERE run_id = ? AND node_id = ? AND claim_worker = ? AND status IN ('claimed', 'in_progress')`,
		s.ts(), runID, nodeID, worker)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if !s.exists(ctx, runID, nodeID) {
			return ErrNotFound
		}
		return ErrNotOwner
	}
	s.emit(ctx, EventStatus, runID, nodeID)
	return nil
}

func (s *SQLite) SetStatus(ctx context.Context, runID, nodeID string, from, to Status) error {
	if !transitionAllowed(from, to) {
		return ErrBadTransition
	}
	holds := to == StatusClaimed || to == StatusInProgress
	res, err := s.db.ExecContext(ctx,
		`UPDATE rows SET status = ?, updated_at = ?,
		   claim_worker = CASE WHEN ? THEN claim_worker ELSE '' END,
		   claim_at = CASE WHEN ? THEN claim_at ELSE '' END,
		   heartbeat_at = CASE WHEN ? THEN heartbeat_at ELSE '' END
		 WHERE run_id = ? AND node_id = ? AND status = ?`,
		string(to), s.ts(), holds, holds, holds, runID, nodeID, string(from))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if !s.exists(ctx, runID, nodeID) {
			return ErrNotFound
		}
		return ErrBadTransition
	}
	s.emit(ctx, EventStatus, runID, nodeID)
	return nil
}

func (s *SQLite) SetRevision(ctx context.Context, runID, nodeID string, revision int) error {
	res, err := s.db.ExecContext(ctx, `UPDATE rows SET revision = ?, updated_at = ? WHERE run_id = ? AND node_id = ?`, revision, s.ts(), runID, nodeID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) AppendAttempt(ctx context.Context, runID, nodeID string, a Attempt) error {
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	// One statement, so concurrent appends cannot lose each other's writes.
	res, err := s.db.ExecContext(ctx,
		`UPDATE rows SET attempts = json_insert(attempts, '$[#]', json(?)), updated_at = ? WHERE run_id = ? AND node_id = ?`,
		string(b), s.ts(), runID, nodeID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.emit(ctx, EventAttempt, runID, nodeID)
	return nil
}

func (s *SQLite) SetResult(ctx context.Context, runID, nodeID string, r Result) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE rows SET result = ?, updated_at = ? WHERE run_id = ? AND node_id = ?`, string(b), s.ts(), runID, nodeID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.emit(ctx, EventResult, runID, nodeID)
	return nil
}

func (s *SQLite) ReleaseStale(ctx context.Context, runID string, olderThan time.Duration) ([]string, error) {
	cutoff := db.TS(s.now().Add(-olderThan))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rs, err := tx.QueryContext(ctx,
		`SELECT node_id FROM rows WHERE run_id = ? AND status IN ('claimed', 'in_progress') AND heartbeat_at < ? ORDER BY node_id`,
		runID, cutoff)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rs.Next() {
		var id string
		if err := rs.Scan(&id); err != nil {
			rs.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rs.Close()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE rows SET status = 'ready', claim_worker = '', claim_at = '', heartbeat_at = '', updated_at = ?
			 WHERE run_id = ? AND node_id = ?`, s.ts(), runID, id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		s.emit(ctx, EventStatus, runID, id)
	}
	return ids, nil
}

func (s *SQLite) Watch(ctx context.Context, runID string) (<-chan Event, error) {
	var last int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM events WHERE run_id = ?`, runID).Scan(&last); err != nil {
		return nil, err
	}
	ch := make(chan Event)
	go func() {
		defer close(ch)
		tick := time.NewTicker(s.poll)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			rs, err := s.db.QueryContext(ctx, `SELECT id, node_id, kind, at FROM events WHERE run_id = ? AND id > ? ORDER BY id`, runID, last)
			if err != nil {
				continue // transient; try again on the next tick
			}
			type change struct {
				id               int64
				node, kind, when string
			}
			var changes []change
			for rs.Next() {
				var c change
				if rs.Scan(&c.id, &c.node, &c.kind, &c.when) == nil {
					changes = append(changes, c)
				}
			}
			rs.Close()
			for _, c := range changes {
				last = c.id
				row, err := s.Get(ctx, runID, c.node)
				if err != nil {
					continue
				}
				at, _ := db.ParseTS(c.when)
				select {
				case ch <- Event{Kind: EventKind(c.kind), Row: row, At: at}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch, nil
}

func (s *SQLite) Close() error { return nil }
