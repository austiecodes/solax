-- Solax schema v2: server-defined agents, async jobs, session inboxes and
-- the stored IR stream of each session.

-- Agents are defined on the server now. A person creates them; the daemon
-- runs them. Names are unique, because the Super Agent routes by name.
ALTER TABLE agents ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN options     TEXT NOT NULL DEFAULT '{}';  -- JSON, driver-specific
ALTER TABLE agents ADD COLUMN deleted     INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agents ADD COLUMN created_at  INTEGER NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX agents_live_name ON agents(name) WHERE deleted = 0;

-- The drivers that the daemon of a machine detected (JSON []DriverProbe).
ALTER TABLE machines ADD COLUMN drivers TEXT NOT NULL DEFAULT '[]';

-- live = 1 while the daemon reports the session process as running.
ALTER TABLE sessions ADD COLUMN live INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN last_seq INTEGER NOT NULL DEFAULT 0;  -- last stored IR event

-- A job is one piece of work that the Super Agent (or a person) gives to
-- one agent. It runs in its own session; the call does not wait for it.
CREATE TABLE jobs (
  id            TEXT PRIMARY KEY,
  owner_id      TEXT NOT NULL REFERENCES owners(id),
  agent_id      TEXT NOT NULL REFERENCES agents(id),
  session_id    TEXT NOT NULL,
  requested_by  TEXT NOT NULL DEFAULT '',  -- author kind: human | super
  title         TEXT NOT NULL DEFAULT '',
  prompt        TEXT NOT NULL,
  state         TEXT NOT NULL,             -- queued | running | done | failed | cancelled
  result        TEXT NOT NULL DEFAULT '',
  error         TEXT NOT NULL DEFAULT '',
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);
CREATE INDEX jobs_owner ON jobs(owner_id, created_at);

-- The inbox of each session: inputs in order, delivered at least once.
-- The input ID is the ir.Input ref, so the daemon drops a redelivery.
CREATE TABLE inputs (
  id          TEXT PRIMARY KEY,
  session_id  TEXT NOT NULL,
  text        TEXT NOT NULL,
  sent        INTEGER NOT NULL DEFAULT 0,
  created_at  INTEGER NOT NULL
);
CREATE INDEX inputs_pending ON inputs(session_id, created_at) WHERE sent = 0;

-- The reliable IR events of each session (no deltas), for replay.
CREATE TABLE session_events (
  session_id  TEXT NOT NULL,
  seq         INTEGER NOT NULL,
  type        TEXT NOT NULL,
  payload     TEXT NOT NULL,  -- JSON ir.Event
  PRIMARY KEY (session_id, seq)
) WITHOUT ROWID;
