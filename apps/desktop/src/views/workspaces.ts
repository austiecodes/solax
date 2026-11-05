// Workspaces: members, the task timeline, and a box to post into a task
// directly (the fast path).

import type { SolaxApi, Workspace } from "../api/api";
import { errorText, h, type View } from "../dom";
import { messageItem } from "./chat";

export function workspacesView(api: SolaxApi): View {
  return (root) => {
    const nav = h("ul", { class: "list" });
    const detail = h("section", { class: "detail" }, h("p", { class: "muted" }, "Select a workspace."));
    root.append(h("h2", {}, "Workspaces"), h("div", { class: "split" }, nav, detail));

    let current: { ws: Workspace; task: string } | undefined;

    const open = async (ws: Workspace) => {
      // The general task until a task picker exists.
      current = { ws, task: "" };
      const timeline = h("ol", { class: "messages" });
      const input = h("input", { placeholder: "Post to this task" });
      const form = h("form", { class: "composer" }, input, h("button", { type: "submit" }, "Post"));
      const members = ws.members.map((m) => `${m.agent_id}${m.role === "leader" ? " (leader)" : ""}`).join(", ");
      detail.replaceChildren(h("h3", {}, ws.name), h("p", { class: "muted" }, members), timeline, form);

      try {
        (await api.workspaces.messages(ws.id, current.task, 0)).forEach((m) => timeline.append(messageItem(m)));
      } catch (err) {
        timeline.append(h("li", { class: "muted" }, errorText(err)));
      }

      form.addEventListener("submit", async (e) => {
        e.preventDefault();
        if (!current || !input.value.trim()) return;
        try {
          await api.workspaces.post(current.ws.id, { task_id: current.task || undefined, text: input.value });
          input.value = "";
        } catch (err) {
          timeline.append(h("li", { class: "muted" }, errorText(err)));
        }
      });
    };

    api.workspaces
      .list()
      .then((wss) =>
        nav.replaceChildren(
          ...wss.map((ws) => {
            const b = h("button", { class: "link", type: "button" }, ws.name);
            b.addEventListener("click", () => void open(ws));
            return h("li", {}, b);
          }),
        ),
      )
      .catch((err) => nav.replaceChildren(h("li", { class: "muted" }, errorText(err))));

    return api.on.messageCreated((m) => {
      if (current && m.workspace_id === current.ws.id) {
        detail.querySelector("ol.messages")?.append(messageItem(m));
      }
    });
  };
}
