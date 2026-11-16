-- Solax schema v3: drop the workspace and task model. Work is jobs now.

-- Children first: foreign keys are on.
DROP TABLE members;
DROP TABLE tasks;
DROP TABLE workspaces;
DROP TABLE task_events;
DROP TABLE cursors;
DROP TABLE effects;

-- Messages are only the Super Agent conversation now. Keep its rows and
-- drop the task columns.
CREATE TABLE messages_v3 (
  id           TEXT PRIMARY KEY,
  owner_id     TEXT NOT NULL,
  seq          INTEGER NOT NULL,  -- per owner
  author_kind  TEXT NOT NULL,
  author_id    TEXT NOT NULL DEFAULT '',
  kind         TEXT NOT NULL,     -- chat | system
  text         TEXT NOT NULL,
  created_at   INTEGER NOT NULL,
  UNIQUE (owner_id, seq)
);
INSERT INTO messages_v3 (id, owner_id, seq, author_kind, author_id, kind, text, created_at)
  SELECT id, owner_id, seq, author_kind, author_id, kind, text, created_at
  FROM messages WHERE workspace_id = '';
DROP TABLE messages;
ALTER TABLE messages_v3 RENAME TO messages;

-- A worker session belongs to a job; the Super Agent session has ''.
ALTER TABLE sessions RENAME COLUMN task_id TO job_id;
