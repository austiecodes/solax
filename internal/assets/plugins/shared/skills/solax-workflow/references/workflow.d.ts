// The API of a Solax workflow script. The server runs the script; the
// Go tests check that the server gives exactly these methods and fields.
//
// A script is one TypeScript module:
//
//   export const meta: WorkflowMeta = { ... };              // optional; needed to save it
//   export async function run(wf: Workflow, args: any) {   // required
//     ...
//     return result;                                         // any JSON value
//   }

/** Plain data that describes the script. `solax workflow save` reads it. */
interface WorkflowMeta {
  name: string;                // letters, digits, ".", "_", "-"; never starts with "wfl_"
  description: string;         // one line
  whenToUse?: string;          // shown by `solax workflow list`
  phases?: { title: string; detail?: string }[];
}

interface Workflow {
  /** Lists the worker agents and whether they are online. */
  agents(): Promise<AgentInfo[]>;

  /** Reads any Solax object by ID: tsk_, ses_, msg_, agt_, mch_. */
  recall(id: string): Promise<RecalledObject>;

  /**
   * Starts a task in a new session of an agent and waits until its turn
   * ends. Throws when the task fails or is cancelled, unless
   * opts.failOnError is false.
   */
  task(agent: string, prompt: string, opts?: TaskOpts): Promise<TaskOutcome>;

  /**
   * Sends a follow-up to a finished task, in the same session, and waits
   * until that turn ends. The agent keeps its context.
   */
  reply(taskId: string, text: string, opts?: TaskOpts): Promise<TaskOutcome>;

  /**
   * Asks whoever started the workflow and waits for the answer. With
   * options, the answer is one of them.
   */
  ask(prompt: string, options?: string[]): Promise<{ choice: string; text: string }>;

  /** Waits ms milliseconds. */
  sleep(ms: number): Promise<void>;

  /** Adds a line to the workflow log. */
  log(message: string): void;

  /** Starts a phase: later steps without opts.phase belong to it. */
  phase(title: string): void;

  /**
   * Starts a round of a loop: later steps without opts.loop belong to it,
   * so the user sees the loop and its rounds. Call it at the top of each
   * round, with the same loop name. wf.round(null) ends the loop.
   * Example: for (let r = 1; r <= max; r++) { wf.round("fix", r, max); ... }
   */
  round(loop: string | null, round?: number, maxRounds?: number): void;

  /**
   * Runs all thunks at once and waits for all of them. Never throws: a
   * failed item is null. Use it when the next step needs all results.
   */
  parallel<T>(thunks: Array<() => Promise<T>>): Promise<Array<T | null>>;

  /**
   * Runs each item through all stages with no wait between stages: item A
   * can be in stage 2 while item B is still in stage 1. A stage gets
   * (previous result, item, index); the first stage gets the item as its
   * previous result. Never throws: a failed item is null.
   */
  pipeline(items: unknown[], ...stages: Array<(prev: any, item: any, index: number) => Promise<any>>): Promise<any[]>;
}

interface TaskOpts {
  /**
   * A JSON Schema with { type: "object" } at its root. The agent must end
   * its answer with a matching JSON object; TaskOutcome.data holds it. The
   * server asks the agent again up to 2 times when it does not match.
   * Known keywords: type, properties, required, items, enum,
   * additionalProperties.
   */
  schema?: object;
  project?: string;        // wf.task only: project name or ID to work in; default: a new project
  phase?: string;          // progress group; default: the current phase
  label?: string;          // short name in `solax workflow list <wfl_id>`
  loop?: string;           // loop name; default: the loop of wf.round
  round?: number;          // round in the loop, from 1; default: from wf.round
  maxRounds?: number;      // most rounds of the loop; default: from wf.round
  stepKey?: string;        // custom step key; rarely needed
  failOnError?: boolean;   // default true
}

interface TaskOutcome {
  id: string;              // tsk_...
  sessionId: string;       // ses_...
  agent: string;
  project: string;         // the project the task works in
  state: "done" | "failed" | "cancelled";
  result: string;          // the last message of the agent (up to 16 KiB)
  data?: any;              // the checked JSON, when opts.schema is set
  error?: string;
}

interface AgentInfo {
  name: string;
  machine: string;
  runtime: string;         // pi | claude | codex | ...
  online: boolean;
  description: string;
}

interface RecalledObject {
  id: string;
  type: string;
  summary: string;
  links?: Record<string, string>;  // relation -> ID
}
