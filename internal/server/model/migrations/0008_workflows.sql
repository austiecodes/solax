-- Solax schema v8: dynamic workflows. A workflow is a TypeScript script
-- that the server runs; each wait of the script is a step, saved so that
-- the script can run again after a restart without repeating work.
CREATE TABLE workflows (
  id            TEXT PRIMARY KEY,
  owner_id      TEXT NOT NULL REFERENCES owners(id),
  requested_by  TEXT NOT NULL,             -- author kind: human | super
  name          TEXT NOT NULL DEFAULT '',  -- meta.name, or the saved name it ran from
  title         TEXT NOT NULL DEFAULT '',
  source        TEXT NOT NULL,             -- TypeScript source
  args_json     TEXT NOT NULL DEFAULT 'null',
  resume_from   TEXT NOT NULL DEFAULT '',  -- wfl_ ID whose steps it may reuse
  state         TEXT NOT NULL,             -- running | waiting | done | failed | cancelled
  result        TEXT NOT NULL DEFAULT '',  -- JSON
  error         TEXT NOT NULL DEFAULT '',
  logs          TEXT NOT NULL DEFAULT '',  -- wf.log lines, newest last, bounded
  log_seq       INTEGER NOT NULL DEFAULT 0,-- seq of the last stored log line; replay skips older ones
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);
CREATE INDEX workflows_owner ON workflows(owner_id, created_at);
CREATE INDEX workflows_active ON workflows(state) WHERE state IN ('running', 'waiting');

CREATE TABLE workflow_steps (
  workflow_id   TEXT NOT NULL REFERENCES workflows(id),
  step_key      TEXT NOT NULL,             -- opts.stepKey, or "step_<n>" in call order
  seq           INTEGER NOT NULL,
  kind          TEXT NOT NULL,             -- agents | recall | task | reply | ask | sleep
  fingerprint   TEXT NOT NULL,             -- sha256 of kind and arguments
  target_id     TEXT NOT NULL DEFAULT '',  -- tsk_ ID, or the wake time of a sleep
  phase         TEXT NOT NULL DEFAULT '',
  label         TEXT NOT NULL DEFAULT '',
  state         TEXT NOT NULL,             -- pending | done | failed
  output_json   TEXT NOT NULL DEFAULT '',
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL,
  PRIMARY KEY (workflow_id, step_key)
) WITHOUT ROWID;

-- Saved workflow patterns: solax workflow save / run --name.
CREATE TABLE saved_workflows (
  owner_id      TEXT NOT NULL REFERENCES owners(id),
  name          TEXT NOT NULL,             -- meta.name; never starts with "wfl_"
  description   TEXT NOT NULL DEFAULT '',
  meta_json     TEXT NOT NULL DEFAULT '{}',
  source        TEXT NOT NULL,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL,
  PRIMARY KEY (owner_id, name)
) WITHOUT ROWID;

ALTER TABLE tasks ADD COLUMN workflow_id TEXT NOT NULL DEFAULT '';
