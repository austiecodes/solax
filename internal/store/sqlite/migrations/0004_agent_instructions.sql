-- Solax schema v4: the instructions of an agent, the system prompt that
-- its sessions get after the Solax role prompt.
ALTER TABLE agents ADD COLUMN instructions TEXT NOT NULL DEFAULT '';
