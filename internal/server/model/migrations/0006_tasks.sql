-- Solax schema v6: a task is what was a job.
ALTER TABLE jobs RENAME TO tasks;
DROP INDEX jobs_owner;
CREATE INDEX tasks_owner ON tasks(owner_id, created_at);
ALTER TABLE sessions RENAME COLUMN job_id TO task_id;
