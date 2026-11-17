-- Solax schema v5: a runtime (claude, codex, pi) is what was a driver.
ALTER TABLE agents RENAME COLUMN driver TO runtime;
ALTER TABLE machines RENAME COLUMN drivers TO runtimes;
