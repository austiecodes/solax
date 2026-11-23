// The prelude builds the wf object of a workflow script from the host
// functions that Go gives. It adds phase, parallel and pipeline, which need
// no Go code.
//
// Each step call passes an ident (the JSON of the arguments that name the
// step) and n (how many earlier calls had the same kind and ident). Go keys
// the step by them, so a step keeps its key however the order of the calls
// changes between runs: replay settles the saved steps in another order
// than the live run did.

export function makeWf(host) {
  let phase = undefined;
  const counts = new Map();

  const occurrence = (kind, ident) => {
    const key = kind + "\u0000" + ident;
    const n = (counts.get(key) || 0) + 1;
    counts.set(key, n);
    return n;
  };

  const withPhase = (opts) => {
    const out = Object.assign({}, opts || {});
    if (out.phase === undefined && phase !== undefined) out.phase = phase;
    return out;
  };

  const step = (kind, identParts, call) => {
    const ident = JSON.stringify(identParts);
    return call(ident, occurrence(kind, ident));
  };

  const message = (e) => (e && e.message) || String(e);

  const wf = {
    agents: () => step("agents", [], (ident, n) => host.agents(ident, n)),

    recall: (id) => step("recall", [String(id)], (ident, n) => host.recall(String(id), ident, n)),

    task: (agent, prompt, opts) => {
      const o = withPhase(opts);
      return step("task", [String(agent), String(prompt), o.schema ?? null, o.stepKey ?? null],
        (ident, n) => host.task(String(agent), String(prompt), o, ident, n));
    },

    reply: (taskId, text, opts) => {
      const o = withPhase(opts);
      return step("reply", [String(taskId), String(text), o.schema ?? null, o.stepKey ?? null],
        (ident, n) => host.reply(String(taskId), String(text), o, ident, n));
    },

    ask: (prompt, options) => {
      const opts = (options || []).map(String);
      return step("ask", [String(prompt), opts], (ident, n) => host.ask(String(prompt), opts, ident, n));
    },

    sleep: (ms) => step("sleep", [Number(ms)], (ident, n) => host.sleep(Number(ms), ident, n)),

    log: (msg) => { host.log(String(msg)); },

    phase: (title) => {
      phase = String(title);
      host.log("phase: " + phase);
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
