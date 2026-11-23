// The prelude builds the wf object of a workflow script from the host
// functions that Go gives. It adds phase, round, parallel and pipeline,
// which need no Go code.
//
// Each step call passes an ident (the JSON of the arguments that name the
// step) and n (how many earlier calls had the same kind and ident). Go keys
// the step by them, so a step keeps its key however the order of the calls
// changes between runs: replay settles the saved steps in another order
// than the live run did.

export function makeWf(host) {
  let phase = undefined;
  let round = undefined; // { loop, round, maxRounds } from wf.round
  const counts = new Map();

  const occurrence = (kind, ident) => {
    const key = kind + "\u0000" + ident;
    const n = (counts.get(key) || 0) + 1;
    counts.set(key, n);
    return n;
  };

  // scoped adds the current phase and loop round to the opts of a step,
  // where the script did not set them.
  const scoped = (opts) => {
    const out = Object.assign({}, opts || {});
    if (out.phase === undefined && phase !== undefined) out.phase = phase;
    if (out.loop === undefined && round !== undefined) Object.assign(out, round);
    return out;
  };

  const step = (kind, identParts, call) => {
    const ident = JSON.stringify(identParts);
    return call(ident, occurrence(kind, ident));
  };

  const message = (e) => (e && e.message) || String(e);

  const wf = {
    agents: () => step("agents", [], (ident, n) => host.agents(scoped(), ident, n)),

    recall: (id) => step("recall", [String(id)], (ident, n) => host.recall(String(id), scoped(), ident, n)),

    task: (agent, prompt, opts) => {
      const o = scoped(opts);
      return step("task", [String(agent), String(prompt), o.schema ?? null, o.stepKey ?? null],
        (ident, n) => host.task(String(agent), String(prompt), o, ident, n));
    },

    reply: (taskId, text, opts) => {
      const o = scoped(opts);
      return step("reply", [String(taskId), String(text), o.schema ?? null, o.stepKey ?? null],
        (ident, n) => host.reply(String(taskId), String(text), o, ident, n));
    },

    ask: (prompt, options) => {
      const opts = (options || []).map(String);
      return step("ask", [String(prompt), opts], (ident, n) => host.ask(String(prompt), opts, scoped(), ident, n));
    },

    sleep: (ms) => step("sleep", [Number(ms)], (ident, n) => host.sleep(Number(ms), scoped(), ident, n)),

    log: (msg) => { host.log(String(msg)); },

    phase: (title) => {
      phase = String(title);
      host.log("phase: " + phase);
    },

    round: (loop, n, maxRounds) => {
      if (loop === null || loop === undefined) {
        round = undefined;
        return;
      }
      const r = Math.trunc(Number(n));
      if (!(r >= 1)) throw new Error("wf.round: round must be a number from 1");
      round = { loop: String(loop), round: r };
      if (maxRounds !== undefined) round.maxRounds = Math.trunc(Number(maxRounds));
      host.log("round: " + round.loop + " " + r + (round.maxRounds ? "/" + round.maxRounds : ""));
    },

    parallel: (thunks) =>
      Promise.all(thunks.map(async (thunk, i) => {
        try {
          return await thunk();
        } catch (e) {
          host.log("parallel item " + i + " failed: " + message(e));
          return null;
        }
      })),

    pipeline: (items, ...stages) =>
      Promise.all(items.map(async (item, i) => {
        let value = item;
        for (const stage of stages) {
          try {
            value = await stage(value, item, i);
          } catch (e) {
            host.log("pipeline item " + i + " failed: " + message(e));
            return null;
          }
        }
        return value;
      })),
  };
  return Object.freeze(wf);
}
