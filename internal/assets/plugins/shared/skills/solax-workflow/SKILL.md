---
name: solax-workflow
description: Write and run Solax workflow scripts — TypeScript that starts tasks on agents, runs them in parallel, loops between agents until a check passes, and asks the user. Use when a job needs two or more dependent tasks, parallel tasks, or a review loop; not for one task.
---

# Solax workflows

A workflow is a short TypeScript script that the **server** runs. The script starts tasks on agents, waits for their results, and decides what comes next. It runs for minutes or hours while you stay free to talk with the user. The server saves each step, so a workflow survives a restart and never runs a finished task again.

## When to use a workflow

- **One task**: do not write a workflow. Use `solax task send <agent> "<prompt>"`.
- **Two or more tasks that depend on each other, parallel tasks, or a loop** (code → review → fix until the review passes): write a workflow.

## Steps

1. **Look for a saved workflow first**: `solax workflow list`. If one fits, run it: `solax workflow run --name <name> --args '<json>'`. Read one with `solax workflow list <name>`.
2. **Else write a script** (rules below) and start it from stdin. Do not write files:

   ```bash
   solax workflow run - --args '{"pkgs":["internal/auth"]}' <<'EOF'
   export const meta = { name: "update-and-review", description: "Update packages, then review them" };
   export async function run(wf, args) {
     const changes = await wf.parallel(args.pkgs.map((p) => () => wf.task("coder", "Update " + p + ". Run its tests.")));
     const review = await wf.task("reviewer", "Review these changes:\n" + changes.map((c) => c.result).join("\n\n"));
     return review.result;
   }
   EOF
   ```

   A syntax error comes back at once with its line. Fix it and run again.
3. **Tell the user the workflow ID (`wfl_...`) and what it does in one or two sentences. End your turn.** Do not wait, poll, or sleep. You get a message `[workflow wfl_... ] <name>: done` (or `failed`) with the result when it ends.
4. **Answer questions.** A script can call `wf.ask`. You then get `[workflow wfl_...] <name> asks: ...`. Answer yourself, or ask the user, then run `solax workflow answer <wfl_id> "<choice>" ["<text>"]`.
5. **Save a useful script** for next time: put it in a file and run `solax workflow save <file>`. It needs `meta.name` and `meta.description`.

Other commands: `solax workflow list <wfl_id>` shows the steps, log, and result of a run. `solax workflow cancel <wfl_id>` stops a run and its tasks. A failed run can start again with a fixed script and reuse its finished steps: `solax workflow run --resume <wfl_id> - <<'EOF' ... EOF`.

## Script rules

- Export `async function run(wf, args)`. `args` is the JSON of `--args`. Return a JSON value: it is the result you get back.
- Export `const meta = { name, description }` when you will save the script. `meta` holds plain data only.
- The full API is in [references/workflow.d.ts](references/workflow.d.ts). Patterns are in [references/patterns.md](references/patterns.md). Read them before your first workflow.
- `wf.task` starts a task in a **new** session of the agent. `wf.reply(taskId, text)` continues the **same** session, so the agent keeps its context. Reply only to a task that has finished.
- **Make the next step depend on data, not on words.** When code must read an agent's answer, pass `{ schema }` and read `outcome.data`. The server makes the agent answer with JSON that matches the schema.
- Each task prompt must stand alone: the agents do not see this conversation or each other. Say the goal, the context, the files, and what the answer must contain. When agents must see each other's work, tell them where it is (the same folder, or a git branch).
- `Date.now()`, `new Date()`, `Math.random()`, and timers throw: a replayed script must take the same path. Pass time values in `args`; wait with `wf.sleep(ms)`.
- Always bound a loop (for example 5 rounds), then `wf.ask` the user what to do.
- The script cannot read files, use the network, or import modules. It only calls `wf`.
- A failed task throws. Catch it, or pass `{ failOnError: false }` and check `outcome.state`. `wf.parallel` and `wf.pipeline` never throw: a failed item is `null`.
- Limits: 8 tasks run at once (more wait), 200 steps, 6 hours.
