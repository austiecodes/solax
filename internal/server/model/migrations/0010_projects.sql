-- Solax schema v10: projects.
-- A project is where agents work: for now a folder on a machine (kind
-- "local" or "managed"); later also a Git repository. An agent has no
-- folder of its own any more; a task works in its project.
CREATE TABLE projects (
  id          TEXT PRIMARY KEY,
  owner_id    TEXT NOT NULL REFERENCES owners(id),
  name        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  kind        TEXT NOT NULL,              -- local | managed
  machine_id  TEXT NOT NULL DEFAULT '',   -- the machine with the folder
  path        TEXT NOT NULL DEFAULT '',   -- local: the folder; managed: its name in the projects folder
  deleted     INTEGER NOT NULL DEFAULT 0,
  created_at  INTEGER NOT NULL
);
CREATE UNIQUE INDEX projects_name ON projects(owner_id, name) WHERE deleted = 0;

ALTER TABLE tasks ADD COLUMN project_id TEXT NOT NULL DEFAULT '';

-- Each folder (machine, dir) of a worker agent becomes a local project,
-- named after the last element of the folder ("home" for "~" and "/"). A second project of the
-- same name gets the machine name after it. The ID takes the time part of
-- the oldest agent ID in the folder, so projects sort by age.
CREATE TEMP TABLE folders AS
SELECT
  a.machine_id AS machine_id,
  rtrim(a.dir, '/') AS dir,
  m.owner_id AS owner_id,
  m.name AS machine_name,
  MIN(a.id) AS first_agent,
  MIN(a.created_at) AS created_at
FROM agents a JOIN machines m ON m.id = a.machine_id
WHERE a.role != 'super' AND rtrim(a.dir, '/') != ''
GROUP BY a.machine_id, rtrim(a.dir, '/');

CREATE TEMP TABLE named AS
SELECT f.machine_id, f.dir, f.owner_id, f.machine_name, f.first_agent, f.created_at,
  CASE WHEN f.last IN ('', '~') THEN 'home' ELSE f.last END AS base
FROM (
  SELECT folders.*, replace(dir, rtrim(dir, replace(dir, '/', '')), '') AS last FROM folders
) f;

INSERT INTO projects(id, owner_id, name, kind, machine_id, path, created_at)
SELECT
  'prj_' || substr(first_agent, 5, 9) || lower(hex(randomblob(8))),
  owner_id,
  CASE WHEN ROW_NUMBER() OVER (PARTITION BY owner_id, base ORDER BY created_at, machine_id) = 1
    THEN base ELSE base || '-' || machine_name END,
  'local', machine_id, dir, created_at
FROM named;

-- Old tasks link to the project of the folder their agent had.
UPDATE tasks SET project_id = COALESCE((
  SELECT p.id FROM agents a JOIN projects p
    ON p.machine_id = a.machine_id AND p.path = rtrim(a.dir, '/') AND p.kind = 'local'
  WHERE a.id = tasks.agent_id
), '');

DROP TABLE folders;
DROP TABLE named;
ALTER TABLE agents DROP COLUMN dir;
