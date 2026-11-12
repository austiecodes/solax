package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
)

// Tasks.

type taskRepo struct{ t *txn }

const taskCols = `id, workspace_id, parent_id, parent_node, depth, target_agent_id, title, brief, general,
	state, blocked_reason, max_agent_turns, max_wall_ms, max_hops, spent_turns, spent_hops,
	orchestrator, orch_hash, orch_state, orch_last_seq, rev, created_at, updated_at`

func (r taskRepo) Get(ctx context.Context, id core.ID) (core.Task, error) {
	return scanTask(r.t.q.QueryRowContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE id = ?`, id))
}

func (r taskRepo) General(ctx context.Context, workspaceID core.ID) (core.Task, error) {
	return scanTask(r.t.q.QueryRowContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE workspace_id = ? AND general = 1`, workspaceID))
}

func (r taskRepo) Create(ctx context.Context, t core.Task) error {
	_, err := r.t.exec(ctx, `INSERT INTO tasks(`+taskCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.WorkspaceID, t.ParentID, t.ParentNode, t.Depth, t.TargetAgentID, t.Title, t.Brief, b2i(t.General),
		t.State, t.BlockedReason, t.Budget.MaxAgentTurns, t.Budget.MaxWallTime.Milliseconds(), t.Budget.MaxHopsWithoutHuman,
		t.Spent.AgentTurns, t.Spent.HopsWithoutHuman,
		t.Orchestrator, t.OrchHash, string(t.OrchState), t.OrchLastSeq, t.Rev, ms(t.CreatedAt), ms(t.UpdatedAt))
	return err
}

func (r taskRepo) Update(ctx context.Context, t core.Task, prevRev int64) error {
	res, err := r.t.exec(ctx, `UPDATE tasks SET title = ?, brief = ?, state = ?, blocked_reason = ?,
			max_agent_turns = ?, max_wall_ms = ?, max_hops = ?, target_agent_id = ?,
			orchestrator = ?, orch_hash = ?, orch_state = ?, orch_last_seq = ?, rev = ?, updated_at = ?
		WHERE id = ? AND rev = ?`,
		t.Title, t.Brief, t.State, t.BlockedReason,
		t.Budget.MaxAgentTurns, t.Budget.MaxWallTime.Milliseconds(), t.Budget.MaxHopsWithoutHuman, t.TargetAgentID,
		t.Orchestrator, t.OrchHash, string(t.OrchState), t.OrchLastSeq, t.Rev, ms(t.UpdatedAt),
		t.ID, prevRev)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// Either the task is gone or someone else moved it on.
		if _, err := r.Get(ctx, t.ID); err != nil {
			return err
		}
		return errs.ErrConflict
	}
	return nil
}

func (r taskRepo) SetSpent(ctx context.Context, id core.ID, s core.Spent) error {
	return mustAffect(r.t.exec(ctx, `UPDATE tasks SET spent_turns = ?, spent_hops = ? WHERE id = ?`,
		s.AgentTurns, s.HopsWithoutHuman, id))
}

func (r taskRepo) List(ctx context.Context, workspaceID core.ID) ([]core.Task, error) {
	rows, err := r.t.q.QueryContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE workspace_id = ? ORDER BY created_at, id`, workspaceID)
	return collect(rows, err, scanTask)
}

func (r taskRepo) Open(ctx context.Context, workspaceID core.ID) ([]core.Task, error) {
	rows, err := r.t.q.QueryContext(ctx, `SELECT `+taskCols+` FROM tasks
		WHERE workspace_id = ? AND state NOT IN (?, ?, ?) ORDER BY created_at, id`,
		workspaceID, core.TaskDone, core.TaskFailed, core.TaskCancelled)
	return collect(rows, err, scanTask)
}

func (r taskRepo) Children(ctx context.Context, parentID core.ID) ([]core.Task, error) {
	rows, err := r.t.q.QueryContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE parent_id = ? AND parent_id <> '' ORDER BY created_at, id`, parentID)
	return collect(rows, err, scanTask)
}

func scanTask(s scanner) (core.Task, error) {
	var t core.Task
	var general int
	var state, blocked, orchState string
	var wallMs, created, updated int64
	if err := s.Scan(&t.ID, &t.WorkspaceID, &t.ParentID, &t.ParentNode, &t.Depth, &t.TargetAgentID, &t.Title, &t.Brief, &general,
		&state, &blocked, &t.Budget.MaxAgentTurns, &wallMs, &t.Budget.MaxHopsWithoutHuman, &t.Spent.AgentTurns, &t.Spent.HopsWithoutHuman,
		&t.Orchestrator, &t.OrchHash, &orchState, &t.OrchLastSeq, &t.Rev, &created, &updated); err != nil {
		return t, mapErr(err)
	}
	t.General = general != 0
	t.State, t.BlockedReason = core.TaskState(state), core.BlockedReason(blocked)
	t.Budget.MaxWallTime = time.Duration(wallMs) * time.Millisecond
	if orchState != "" {
		t.OrchState = json.RawMessage(orchState)
	}
	t.CreatedAt, t.UpdatedAt = tm(created), tm(updated)
	return t, nil
}

// Messages.

type messageRepo struct{ t *txn }

const messageCols = `id, owner_id, workspace_id, task_id, seq, author_kind, author_id, kind, status, checkpoint, mentions, text, created_at`

func (r messageRepo) Get(ctx context.Context, id core.ID) (core.Message, error) {
	return scanMessage(r.t.q.QueryRowContext(ctx, `SELECT `+messageCols+` FROM messages WHERE id = ?`, id))
}

// Append assigns the next seq of the stream. Write transactions are
// serialised, so read-increment-write inside one is safe.
func (r messageRepo) Append(ctx context.Context, m core.Message) (core.Message, error) {
	if m.OwnerID == "" {
		return m, errs.ErrInvalid
	}
	var err error
	if m.WorkspaceID != "" {
		err = r.t.q.QueryRowContext(ctx, `UPDATE workspaces SET next_seq = next_seq + 1 WHERE id = ? AND owner_id = ? RETURNING next_seq`,
			m.WorkspaceID, m.OwnerID).Scan(&m.Seq)
	} else {
		err = r.t.q.QueryRowContext(ctx, `UPDATE owners SET super_seq = super_seq + 1 WHERE id = ? RETURNING super_seq`,
			m.OwnerID).Scan(&m.Seq)
	}
	if err != nil {
		return m, mapErr(err)
	}
	mentions, err := json.Marshal(nonNil(m.Mentions))
	if err != nil {
		return m, err
	}
	_, err = r.t.exec(ctx, `INSERT INTO messages(`+messageCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.OwnerID, m.WorkspaceID, m.TaskID, m.Seq, m.Author.Kind, m.Author.ID, m.Kind, m.Status, b2i(m.Checkpoint),
		string(mentions), m.Text, ms(m.CreatedAt))
	return m, err
}

func (r messageRepo) Since(ctx context.Context, taskID core.ID, since int64, limit int) ([]core.Message, error) {
	rows, err := r.t.q.QueryContext(ctx, `SELECT `+messageCols+` FROM messages WHERE task_id = ? AND task_id <> '' AND seq > ?
		ORDER BY seq LIMIT ?`, taskID, since, limitOr(limit))
	return collect(rows, err, scanMessage)
}

func (r messageRepo) SuperSince(ctx context.Context, ownerID core.ID, since int64, limit int) ([]core.Message, error) {
	rows, err := r.t.q.QueryContext(ctx, `SELECT `+messageCols+` FROM messages WHERE owner_id = ? AND workspace_id = '' AND seq > ?
		ORDER BY seq LIMIT ?`, ownerID, since, limitOr(limit))
	return collect(rows, err, scanMessage)
}

func scanMessage(s scanner) (core.Message, error) {
	var m core.Message
	var authorKind, kind, status, mentions string
	var checkpoint int
	var created int64
	if err := s.Scan(&m.ID, &m.OwnerID, &m.WorkspaceID, &m.TaskID, &m.Seq, &authorKind, &m.Author.ID, &kind, &status,
		&checkpoint, &mentions, &m.Text, &created); err != nil {
		return m, mapErr(err)
	}
	m.Author.Kind, m.Kind, m.Status = core.AuthorKind(authorKind), core.MessageKind(kind), core.ReportStatus(status)
	m.Checkpoint, m.CreatedAt = checkpoint != 0, tm(created)
	if err := json.Unmarshal([]byte(mentions), &m.Mentions); err != nil {
		return m, err
	}
	if len(m.Mentions) == 0 {
		m.Mentions = nil
	}
	return m, nil
}

// Task events.

type eventRepo struct{ t *txn }

func (r eventRepo) Append(ctx context.Context, e core.TaskEvent) (core.TaskEvent, error) {
	if e.TaskID == "" || e.Kind == "" {
		return e, errs.ErrInvalid
	}
	if len(e.Payload) == 0 {
		e.Payload = json.RawMessage(`{}`)
	}
	if !json.Valid(e.Payload) {
		return e, errs.ErrInvalid
	}
	err := r.t.q.QueryRowContext(ctx, `INSERT INTO task_events(task_id, event_seq, kind, payload, created_at)
		VALUES (?, (SELECT COALESCE(MAX(event_seq), 0) + 1 FROM task_events WHERE task_id = ?), ?, ?, ?)
		RETURNING event_seq`, e.TaskID, e.TaskID, e.Kind, string(e.Payload), ms(e.At)).Scan(&e.Seq)
	return e, mapErr(err)
}

func (r eventRepo) Since(ctx context.Context, taskID core.ID, after int64, limit, maxBytes int) ([]core.TaskEvent, error) {
	rows, err := r.t.q.QueryContext(ctx, `SELECT task_id, event_seq, kind, payload, created_at FROM task_events
		WHERE task_id = ? AND event_seq > ? ORDER BY event_seq LIMIT ?`, taskID, after, limitOr(limit))
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []core.TaskEvent
	size := 0
	for rows.Next() {
		var e core.TaskEvent
		var kind, payload string
		var at int64
		if err := rows.Scan(&e.TaskID, &e.Seq, &kind, &payload, &at); err != nil {
			return nil, mapErr(err)
		}
		size += len(payload)
		if maxBytes > 0 && size > maxBytes && len(out) > 0 {
			break
		}
		e.Kind, e.Payload, e.At = core.TaskEventKind(kind), json.RawMessage(payload), tm(at)
		out = append(out, e)
	}
	return out, mapErr(rows.Err())
}

func (r eventRepo) Last(ctx context.Context, taskID core.ID) (int64, error) {
	var n int64
	err := r.t.q.QueryRowContext(ctx, `SELECT COALESCE(MAX(event_seq), 0) FROM task_events WHERE task_id = ?`, taskID).Scan(&n)
	return n, mapErr(err)
}

// Sessions and cursors.

type sessionRepo struct{ t *txn }

const sessionCols = `s.id, s.agent_id, s.task_id, s.state, s.ref, s.live, s.last_seq, s.updated_at`

func (r sessionRepo) Get(ctx context.Context, id core.ID) (core.Session, error) {
	return scanSession(r.t.q.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM sessions s WHERE s.id = ?`, id))
}

func (r sessionRepo) For(ctx context.Context, agentID, taskID core.ID) (core.Session, error) {
	return scanSession(r.t.q.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM sessions s WHERE s.agent_id = ? AND s.task_id = ?`, agentID, taskID))
}

func (r sessionRepo) Save(ctx context.Context, s core.Session) error {
	ref := ""
	if s.Ref != nil {
		b, err := json.Marshal(s.Ref)
		if err != nil {
			return err
		}
		ref = string(b)
	}
	_, err := r.t.exec(ctx, `INSERT INTO sessions(id, agent_id, task_id, state, ref, live, last_seq, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET state = excluded.state, ref = excluded.ref, live = excluded.live,
			last_seq = excluded.last_seq, updated_at = excluded.updated_at`,
		s.ID, s.AgentID, s.JobID, s.State, ref, b2i(s.Live), s.LastSeq, ms(s.UpdatedAt))
	return err
}

func (r sessionRepo) OnMachine(ctx context.Context, machineID core.ID) ([]core.Session, error) {
	rows, err := r.t.q.QueryContext(ctx, `SELECT `+sessionCols+` FROM sessions s JOIN agents a ON a.id = s.agent_id
		WHERE a.machine_id = ? ORDER BY s.id`, machineID)
	return collect(rows, err, scanSession)
}

func scanSession(sc scanner) (core.Session, error) {
	var s core.Session
	var state, ref string
	var updated int64
	var live int
	if err := sc.Scan(&s.ID, &s.AgentID, &s.JobID, &state, &ref, &live, &s.LastSeq, &updated); err != nil {
		return s, mapErr(err)
	}
	s.State, s.Live, s.UpdatedAt = ir.SessionState(state), live != 0, tm(updated)
	if ref != "" {
		s.Ref = new(ir.SessionRef)
		if err := json.Unmarshal([]byte(ref), s.Ref); err != nil {
			return s, err
		}
	}
	return s, nil
}

type cursorRepo struct{ t *txn }

// Get returns the cursor, or a zero cursor for the pair if there is none.
func (r cursorRepo) Get(ctx context.Context, sessionID, taskID core.ID) (core.Cursor, error) {
	c := core.Cursor{SessionID: sessionID, TaskID: taskID}
	err := r.t.q.QueryRowContext(ctx, `SELECT delivered_seq, read_seq FROM cursors WHERE session_id = ? AND task_id = ?`,
		sessionID, taskID).Scan(&c.DeliveredSeq, &c.ReadSeq)
	if err := mapErr(err); err != nil && err != errs.ErrNotFound {
		return c, err
	}
	return c, nil
}

// Save stores the cursor. Cursors only move forward: a lower value never
// replaces a higher one, so a late ack cannot rewind delivery.
func (r cursorRepo) Save(ctx context.Context, c core.Cursor) error {
	_, err := r.t.exec(ctx, `INSERT INTO cursors(session_id, task_id, delivered_seq, read_seq) VALUES (?, ?, ?, ?)
		ON CONFLICT(session_id, task_id) DO UPDATE SET
			delivered_seq = MAX(delivered_seq, excluded.delivered_seq),
			read_seq = MAX(read_seq, excluded.read_seq)`,
		c.SessionID, c.TaskID, c.DeliveredSeq, c.ReadSeq)
	return err
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// limitOr bounds a query limit: <= 0 means the default of 500, and no
// query returns more than 5000 rows.
func limitOr(n int) int {
	switch {
	case n <= 0:
		return 500
	case n > 5000:
		return 5000
	default:
		return n
	}
}
