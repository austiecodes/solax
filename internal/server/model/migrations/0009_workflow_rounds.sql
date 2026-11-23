-- Solax schema v9: loop rounds of workflow steps.
-- wf.round(loop, round, maxRounds) in a script puts its later steps in a
-- round of a named loop, so a client can show the loop and its rounds.
ALTER TABLE workflow_steps ADD COLUMN loop TEXT NOT NULL DEFAULT '';
ALTER TABLE workflow_steps ADD COLUMN round INTEGER NOT NULL DEFAULT 0;
ALTER TABLE workflow_steps ADD COLUMN max_rounds INTEGER NOT NULL DEFAULT 0;
