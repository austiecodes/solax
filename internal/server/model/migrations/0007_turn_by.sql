-- Solax schema v7: Fast Path and Slow Path.
-- turn_by is who started the current or queued turn of a task; the result
-- of that turn goes to them. requested_by stays the creator.
ALTER TABLE tasks ADD COLUMN turn_by TEXT NOT NULL DEFAULT '';
UPDATE tasks SET turn_by = requested_by;

-- mode is follow_up, steer (both wake the session) or note (passive
-- context, sent with the next waking input).
ALTER TABLE inputs ADD COLUMN mode TEXT NOT NULL DEFAULT 'follow_up';
CREATE INDEX inputs_waking ON inputs(session_id) WHERE sent = 0 AND mode != 'note';
