/**
 * pi-solax: Solax tools for the Super Agent.
 *
 * The Super Agent runs with `--tools codemode` only. Each tool here has
 * `exposure: "codemode"`, so the model does not see it directly. Codemode
 * scripts call it as `tools.solax_list({...})` and get `structuredContent`.
 *
 * The daemon sets SOLAX_SERVER and SOLAX_TOKEN (a Super Agent token).
 */

import { Type, type TSchema } from "@earendil-works/pi-ai";
import { defineTool, type ExtensionAPI } from "@earendil-works/pi-coding-agent";

const server = process.env.SOLAX_SERVER ?? "";

/**
 * Reads the token for each call, so a rotated token is picked up without a
 * restart. SOLAX_TOKEN_FILE wins: the server writes a fresh token there
 * when a long Super Agent session outlives its previous one.
 */
async function readToken(): Promise<string> {
  const file = process.env.SOLAX_TOKEN_FILE;
  if (file) {
    const { readFile } = await import("node:fs/promises");
    const t = (await readFile(file, "utf8")).trim();
    if (t) return t;
  }
  return process.env.SOLAX_TOKEN ?? "";
}

/** Calls the Solax REST API. */
async function call<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const token = await readToken();
  const res = await fetch(new URL(path, server), {
    method,
    headers: {
      Authorization: `Bearer ${token}`,
      ...(body === undefined ? {} : { "Content-Type": "application/json" }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  });
  const text = await res.text();
  if (!res.ok) throw new Error(`solax ${method} ${path}: ${res.status} ${text}`);
  return (text ? JSON.parse(text) : undefined) as T;
}

/** Lists everything the Super Agent needs to plan, with IDs. */
async function list(): Promise<unknown> {
  const [workspaces, agents, machines] = await Promise.all([
    call<any[]>("GET", "/v1/workspaces"),
    call<any[]>("GET", "/v1/agents"),
    call<any[]>("GET", "/v1/machines"),
  ]);
  const tasks = await Promise.all(
    workspaces.map(async (w) => ({
      workspace: w.id,
      tasks: await call<any[]>("GET", `/v1/workspaces/${w.id}/tasks`),
    })),
  );
  return { workspaces, agents, machines, tasks };
}

/** Defines a codemode-only tool that returns structured data. */
function solaxTool<P extends TSchema>(spec: {
	name: string;
	description: string;
	parameters: P;
	outputSchema: TSchema;
	run: (params: any, signal?: AbortSignal) => Promise<unknown>;
}) {
	return defineTool({
		name: spec.name,
		label: spec.name,
		description: spec.description,
		parameters: spec.parameters,
		outputSchema: spec.outputSchema,
		exposure: "codemode",
		async execute(_id, params, signal) {
			const data = await spec.run(params, signal);
			return {
				content: [{ type: "text", text: JSON.stringify(data) }],
				structuredContent: data as any,
				details: undefined,
			};
		},
	});
}

const Obj = Type.Object({}, { additionalProperties: true });

const tools = [
	solaxTool({
		name: "solax_list",
		description: "List workspaces, agents, machine status and open tasks, with IDs.",
		parameters: Type.Object({}),
		outputSchema: Obj,
		run: (_p, _s) => list(),
	}),
	solaxTool({
		name: "solax_recall",
		description: "Recall any object by ID. The result has a summary and the IDs of related objects.",
		parameters: Type.Object({ id: Type.String() }),
		outputSchema: Obj,
		run: (p, s) => call("GET", `/v1/objects/${p.id}`, undefined, s),
	}),
	solaxTool({
		name: "solax_task_create",
		description: "Create a task in a workspace with a brief. This wakes the workspace leader.",
		parameters: Type.Object({ workspace: Type.String(), brief: Type.String() }),
		outputSchema: Obj,
		run: (p, s) => call("POST", `/v1/workspaces/${p.workspace}/tasks`, { brief: p.brief }, s),
	}),
	solaxTool({
		name: "solax_task_post",
		description: "Post a message into an existing task, for example a decision of the user.",
		parameters: Type.Object({
			workspace: Type.String(),
			task: Type.String(),
			text: Type.String(),
			mentions: Type.Optional(Type.Array(Type.String())),
		}),
		outputSchema: Obj,
		run: (p, s) =>
			call("POST", `/v1/workspaces/${p.workspace}/messages`, { task_id: p.task, text: p.text, mentions: p.mentions }, s),
	}),
	solaxTool({
		name: "solax_task_cancel",
		description: "Cancel a task.",
		parameters: Type.Object({ task: Type.String() }),
		outputSchema: Obj,
		run: (p, s) => call("POST", `/v1/tasks/${p.task}/cancel`, undefined, s),
	}),
	solaxTool({
		name: "solax_workspace_create",
		description: "Create a workspace with a leader agent (agent@machine).",
		parameters: Type.Object({ name: Type.String(), leader: Type.String() }),
		outputSchema: Obj,
		run: (p, s) => call("POST", "/v1/workspaces", p, s),
	}),
	solaxTool({
		name: "solax_member_add",
		description: "Add an agent (agent@machine) to a workspace.",
		parameters: Type.Object({ workspace: Type.String(), agent: Type.String(), leader: Type.Optional(Type.Boolean()) }),
		outputSchema: Obj,
		run: (p, s) => call("POST", `/v1/workspaces/${p.workspace}/members`, { agent: p.agent, leader: p.leader }, s),
	}),
	solaxTool({
		name: "solax_reply",
		description: "Reply to the user in the client conversation. Keep it short.",
		parameters: Type.Object({ text: Type.String() }),
		outputSchema: Obj,
		run: (_p, _s) => call("POST", "/v1/super/replies", _p, _s),
	}),
];

export default function (pi: ExtensionAPI) {
	for (const t of tools) pi.registerTool(t);
	// TODO: deliver notes without a wake: pi.sendMessage(..., { triggerTurn: false }) (verify in M0).
}
