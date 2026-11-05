// The app shell: navigation, the connection badge and the current view.

import type { SolaxApi } from "./api/api";
import { desktopApi } from "./api/desktop";
import type { View } from "./dom";
import { chatView } from "./views/chat";
import { machinesView } from "./views/machines";
import { settingsView } from "./views/settings";
import { workspacesView } from "./views/workspaces";

const api: SolaxApi = desktopApi();

const views: Record<string, View> = {
  chat: chatView(api),
  workspaces: workspacesView(api),
  machines: machinesView(api),
  settings: settingsView(api),
};

const root = document.querySelector<HTMLElement>("#view")!;
const badge = document.querySelector<HTMLElement>("#connection")!;
let cleanup = () => {};

function show(name: string) {
  const view = views[name] ?? views.chat!;
  cleanup();
  root.replaceChildren();
  cleanup = view(root);
  document.querySelectorAll<HTMLAnchorElement>("nav a").forEach((a) => {
    a.classList.toggle("active", a.hash === `#${name}`);
  });
}

window.addEventListener("hashchange", () => show(location.hash.slice(1)));
show(location.hash.slice(1) || "chat");

api.on.connection((c) => {
  badge.className = `badge ${c.state}`;
  badge.textContent = c.error ? `${c.state}: ${c.error}` : c.state;
});
