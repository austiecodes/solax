-- Solax schema v1. Times are INTEGER Unix milliseconds (UTC); 0 means unset.
-- JSON columns hold TEXT. Booleans are INTEGER 0/1.

CREATE TABLE owners (
  id          TEXT PRIMARY KEY,
  super_seq   INTEGER NOT NULL DEFAULT 0,  -- last seq of the Super Agent conversation
  created_at  INTEGER NOT NULL
);

CREATE TABLE client_tokens (
  id          TEXT PRIMARY KEY,
  owner_id    TEXT NOT NULL REFERENCES owners(id),
  name        TEXT NOT NULL,
  hash        TEXT NOT NULL UNIQUE,
  created_at  INTEGER NOT NULL,
  last_used   INTEGER NOT NULL DEFAULT 0,
  revoked     INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE machines (
  id          TEXT PRIMARY KEY,
  owner_id    TEXT NOT NULL REFERENCES owners(id),
  name        TEXT NOT NULL,
  state       TEXT NOT NULL,
  token_hash  TEXT NOT NULL UNIQUE,
  pair_code   TEXT NOT NULL DEFAULT '',
  pair_until  INTEGER NOT NULL DEFAULT 0,
  last_seen   INTEGER NOT NULL DEFAULT 0,
  online      INTEGER NOT NULL DEFAULT 0,
  slots       INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX machines_pair_code ON machines(pair_code) WHERE pair_code <> '';

CREATE TABLE agents (
  id          TEXT PRIMARY KEY,
  machine_id  TEXT NOT NULL REFERENCES machines(id),
  name        TEXT NOT NULL,
  driver      TEXT NOT NULL,
  role        TEXT NOT NULL DEFAULT '',
  dir         TEXT NOT NULL DEFAULT '',
  model       TEXT NOT NULL DEFAULT '',
  UNIQUE (machine_id, name)
);

CREATE TABLE workspaces (
  id            TEXT PRIMARY KEY,
  owner_id      TEXT NOT NULL REFERENCES owners(id),
  name          TEXT NOT NULL,
  leader_id     TEXT NOT NULL DEFAULT '',
  orchestrator  TEXT NOT NULL DEFAULT '',
  next_seq      INTEGER NOT NULL DEFAULT 0,  -- last message seq of the workspace
  created_at    INTEGER NOT NULL,
  UNIQUE (owner_id, name)
);

CREATE TABLE members (
  workspace_id  TEXT NOT NULL REFERENCES workspaces(id),
  agent_id      TEXT NOT NULL REFERENCES agents(id),
  role          TEXT NOT NULL,
  PRIMARY KEY (workspace_id, agent_id)
);

CREATE TABLE tasks (
  id               TEXT PRIMARY KEY,
  workspace_id     TEXT NOT NULL REFERENCES workspaces(id),
  parent_id        TEXT NOT NULL DEFAULT '',
  parent_node      TEXT NOT NULL DEFAULT '',
  depth            INTEGER NOT NULL DEFAULT 0,
  target_agent_id  TEXT NOT NULL DEFAULT '',
  title            TEXT NOT NULL DEFAULT '',
  brief            TEXT NOT NULL DEFAULT '',
  general          INTEGER NOT NULL DEFAULT 0,
  state            TEXT NOT NULL,
  blocked_reason   TEXT NOT NULL DEFAULT '',
  max_agent_turns  INTEGER NOT NULL DEFAULT 0,
  max_wall_ms      INTEGER NOT NULL DEFAULT 0,
  max_hops         INTEGER NOT NULL DEFAULT 0,
  spent_turns      INTEGER NOT NULL DEFAULT 0,
  spent_hops       INTEGER NOT NULL DEFAULT 0,
  orchestrator     TEXT NOT NULL DEFAULT '',
  orch_hash        TEXT NOT NULL DEFAULT '',
  orch_state       TEXT NOT NULL DEFAULT '',
  orch_last_seq    INTEGER NOT NULL DEFAULT 0,
  rev              INTEGER NOT NULL DEFAULT 0,
  created_at       INTEGER NOT NULL,
  updated_at       INTEGER NOT NULL
);
CREATE INDEX tasks_workspace ON tasks(workspace_id, created_at);
CREATE INDEX tasks_parent ON tasks(parent_id) WHERE parent_id <> '';
CREATE UNIQUE INDEX tasks_general ON tasks(workspace_id) WHERE general = 1;

CREATE TABLE messages (
  id            TEXT PRIMARY KEY,
  owner_id      TEXT NOT NULL,
  workspace_id  TEXT NOT NULL DEFAULT '',  -- '' for the Super Agent conversation
  task_id       TEXT NOT NULL DEFAULT '',
  seq           INTEGER NOT NULL,
  author_kind   TEXT NOT NULL,
  author_id     TEXT NOT NULL DEFAULT '',
  kind          TEXT NOT NULL,
  status        TEXT NOT NULL DEFAULT '',
  checkpoint    INTEGER NOT NULL DEFAULT 0,
  mentions      TEXT NOT NULL DEFAULT '[]',
  text          TEXT NOT NULL,
  created_at    INTEGER NOT NULL,
  UNIQUE (owner_id, workspace_id, seq)
);
CREATE INDEX messages_task ON messages(task_id, seq);

CREATE TABLE task_events (
  task_id     TEXT NOT NULL,
  event_seq   INTEGER NOT NULL,
  kind        TEXT NOT NULL,
  payload     TEXT NOT NULL,
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (task_id, event_seq)
) WITHOUT ROWID;

CREATE TABLE sessions (
  id          TEXT PRIMARY KEY,
  agent_id    TEXT NOT NULL REFERENCES agents(id),
  task_id     TEXT NOT NULL DEFAULT '',  -- '' for the Super Agent session
  state       TEXT NOT NULL,
  ref         TEXT NOT NULL DEFAULT '',  -- JSON ir.SessionRef
  updated_at  INTEGER NOT NULL,
  UNIQUE (agent_id, task_id)
);

CREATE TABLE cursors (
  session_id     TEXT NOT NULL,
  task_id        TEXT NOT NULL,
  delivered_seq  INTEGER NOT NULL DEFAULT 0,
  read_seq       INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (session_id, task_id)
) WITHOUT ROWID;

CREATE TABLE effects (
  kind           TEXT NOT NULL,
  key            TEXT NOT NULL,
  payload        TEXT NOT NULL DEFAULT 'null',
  generation     INTEGER NOT NULL DEFAULT 1,
  attempts       INTEGER NOT NULL DEFAULT 0,
  run_at         INTEGER NOT NULL,
  claimed_until  INTEGER NOT NULL DEFAULT 0,
  claim_token    TEXT NOT NULL DEFAULT '',
  last_error     TEXT NOT NULL DEFAULT '',
  created_at     INTEGER NOT NULL,
  PRIMARY KEY (kind, key)
) WITHOUT ROWID;
CREATE INDEX effects_due ON effects(kind, run_at);
