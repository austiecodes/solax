# Workflow patterns

Short, complete scripts. Change the agent names to the agents of `solax agent list`.

## Fan out, then review (pipeline)

Each package goes through code and then review, with no wait between stages.

```ts
export const meta = { name: "update-and-review", description: "Update each package, review each change" };

const VERDICT = {
  type: "object",
  properties: { pass: { type: "boolean" }, issues: { type: "array", items: { type: "string" } } },
  required: ["pass", "issues"],
};

export async function run(wf: Workflow, args: { pkgs: string[]; goal: string }) {
  const results = await wf.pipeline(
    args.pkgs,
    (pkg: string) => wf.task("coder", `In ${pkg}: ${args.goal}. Run its tests. Say which files you changed.`, { phase: "Code", label: pkg }),
    (change: TaskOutcome, pkg: string) =>
      wf.task("reviewer", `Review the change in ${pkg}:\n${change.result}`, { schema: VERDICT, phase: "Review", label: pkg }),
  );
  return results.map((r, i) => ({ pkg: args.pkgs[i], pass: r?.data.pass ?? false, issues: r?.data.issues ?? ["failed"] }));
}
```

Use `wf.parallel` instead when the next step needs **all** results at once (for example, to remove duplicates).

## Loop between two agents until a check passes

The coder and the reviewer each keep one session (`wf.reply`), so they remember the earlier rounds. The loop is bounded; then the user decides. `wf.round` marks each round, so the user sees "round 2 of 5" while it runs; `wf.round(null)` ends the loop before the question.

```ts
export const meta = { name: "fix-until-pass", description: "Coder fixes, reviewer checks, until the review passes" };

const VERDICT = {
  type: "object",
  properties: { pass: { type: "boolean" }, issues: { type: "array", items: { type: "string" } } },
  required: ["pass", "issues"],
};

export async function run(wf: Workflow, args: { goal: string; branch: string; maxRounds?: number }) {
  const max = args.maxRounds ?? 5;
  wf.round("fix", 1, max);
  let code = await wf.task("coder", `${args.goal}\nCommit to the branch ${args.branch} and push it.`);
  let review = await wf.task("reviewer", `Review the branch ${args.branch} for: ${args.goal}`, { schema: VERDICT });

  for (let round = 2; !review.data.pass && round <= max; round++) {
    wf.round("fix", round, max);
    wf.log(`round ${round}: ${review.data.issues.length} issues`);
    code = await wf.reply(code.id, "Fix these review issues:\n- " + review.data.issues.join("\n- "));
    review = await wf.reply(review.id, "The coder pushed fixes. Review the branch again.", { schema: VERDICT });
  }
  wf.round(null);
  if (!review.data.pass) {
    const a = await wf.ask(`The review still fails after ${max} rounds:\n${review.data.issues.join("\n")}`, ["keep", "drop"]);
    return { pass: false, choice: a.choice };
  }
  return { pass: true, summary: code.result };
}
```

## Plan with structured data, then fan out

```ts
const PLAN = {
  type: "object",
  properties: { items: { type: "array", items: { type: "string" } } },
  required: ["items"],
};

export async function run(wf: Workflow, args: { goal: string }) {
  wf.phase("Plan");
  const plan = await wf.task("architect", `Split this work into independent parts: ${args.goal}`, { schema: PLAN });
  wf.phase("Build");
  const done = await wf.parallel(plan.data.items.map((item: string) => () => wf.task("coder", item)));
  const failed = done.filter((d) => d === null).length;
  if (failed > 0) wf.log(`${failed} parts failed`);
  return done.filter(Boolean).map((d) => d!.result);
}
```

## Ask before a risky step

```ts
const ok = await wf.ask("All tests pass. Merge the branch?", ["merge", "stop"]);
if (ok.choice === "stop") return "stopped by the user";
await wf.task("coder", "Merge the branch into main and push.");
```
