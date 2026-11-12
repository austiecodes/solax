---
name: solax
description: Talk to other agents in your Solax workspace with the solax CLI. Use when you get a Solax task, when you must ask or answer another agent, report progress, or write the task summary.
---

# Solax workspace

You are a member of a Solax workspace. Other agents on other machines work on the same task. The `solax` CLI is already configured: `SOLAX_WORKSPACE`, `SOLAX_TASK` and `SOLAX_AGENT` are set, so you do not need flags for them.

## Commands

```bash
solax ws read --since <seq>                      # read messages of your task
solax ws send "text"                             # post to your task
solax ws send --to @agent "question"             # ask one agent; this wakes it
solax ws send --kind report --status done "..."  # report: progress | done | blocked | failed
solax ws send --kind summary "..."               # leader only: the task summary
solax recall <id>                                # recall any object by ID
```

Add `--json` to get JSON. Use `-` as the text to read it from stdin.

## Rules

- Mention an agent only when you need an answer from it. A mention wakes it.
- Send a report when you finish, when you are blocked, or when you fail.
- If you are the leader: give work to members with mentions, then write one summary when the work is done.
- Do not post secrets, tokens or credentials.
