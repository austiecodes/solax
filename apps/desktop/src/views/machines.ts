// Machines: online state and the agents that each daemon defines.

import type { Machine, SolaxApi } from "../api/api";
import { errorText, h, type View } from "../dom";

export function machinesView(api: SolaxApi): View {
  return (root) => {
    const list = h("ul", { class: "list" });
    const code = h("input", { placeholder: "Pairing code from solax daemon start" });
    const status = h("p", { class: "muted" });
    const approve = h("form", { class: "composer" }, code, h("button", { type: "submit" }, "Approve"));
    root.append(h("h2", {}, "Machines"), list, h("h3", {}, "Approve a new machine"), approve, status);

    approve.addEventListener("submit", async (e) => {
      e.preventDefault();
      if (!code.value.trim()) return;
      try {
        render(await api.machines.approve(code.value.trim()));
        code.value = "";
        status.textContent = "";
      } catch (err) {
        status.textContent = errorText(err);
      }
    });

    const rows = new Map<string, HTMLElement>();
    const render = (m: Machine) => {
      const agents = m.agents.map((a) => `${a.name} (${a.driver}${a.role ? `, ${a.role}` : ""})`).join(", ");
      const row = h(
        "li",
        { class: `machine ${m.state} ${m.online ? "online" : "offline"}` },
        h("strong", {}, m.name),
        h("span", { class: "muted" }, ` ${m.state} · ${m.online ? "online" : "offline"} · ${agents || "no agents"}`),
      );
      const old = rows.get(m.id);
      if (old) old.replaceWith(row);
      else list.append(row);
      rows.set(m.id, row);
    };

    api.machines
      .list()
      .then((ms) => ms.forEach(render))
      .catch((err) => list.replaceChildren(h("li", { class: "muted" }, errorText(err))));

    return api.on.machineUpdated(render);
  };
}
