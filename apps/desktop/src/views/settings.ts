// Settings: server profiles. The token goes to the Go side, which keeps it
// in the OS keychain.

import type { SolaxApi } from "../api/api";
import { errorText, h, type View } from "../dom";

export function settingsView(api: SolaxApi): View {
  return (root) => {
    const list = h("ul", { class: "list" });
    const name = h("input", { placeholder: "Name", value: "default" });
    const server = h("input", { placeholder: "https://solax.example.com" });
    // A client token from "solax-server token create".
    const token = h("input", { placeholder: "Client token (sxc_…)", type: "password" });
    const status = h("p", { class: "muted" });
    const form = h("form", { class: "stack" }, name, server, token, h("button", { type: "submit" }, "Save and use"));
    root.append(h("h2", {}, "Servers"), list, form, status);

    const refresh = () =>
      api.profiles
        .list()
        .then((ps) => list.replaceChildren(...ps.map((p) => h("li", {}, h("strong", {}, p.name), ` ${p.server}`))))
        .catch((err) => (status.textContent = errorText(err)));

    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      try {
        await api.profiles.save({ name: name.value, server: server.value }, token.value);
        await api.profiles.use(name.value);
        await api.profiles.test();
        token.value = "";
        status.textContent = "Connected.";
      } catch (err) {
        status.textContent = errorText(err);
      }
      void refresh();
    });

    void refresh();
    return () => {};
  };
}
