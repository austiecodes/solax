// Chat: the conversation of the user with the Super Agent (the main view).

import type { Message, SolaxApi } from "../api/api";
import { errorText, h, type View } from "../dom";

export function chatView(api: SolaxApi): View {
  return (root) => {
    const list = h("ol", { class: "messages" });
    const input = h("textarea", { placeholder: "Tell the Super Agent what you need…", rows: 3 });
    const status = h("p", { class: "muted" });
    const form = h("form", { class: "composer" }, input, h("button", { type: "submit" }, "Send"));
    root.append(h("h2", {}, "Super Agent"), list, status, form);

    // last is the highest seq shown, so a reply is not shown twice.
    let last = 0;
    const show = (m: Message) => {
      if (m.seq <= last) return;
      list.append(messageItem(m));
      last = m.seq;
    };

    api.super
      .history(0)
      .then((ms) => ms.forEach(show))
      .catch((err) => (status.textContent = errorText(err)));

    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      const text = input.value.trim();
      if (!text) return;
      try {
        show(await api.super.send(text));
        input.value = "";
        status.textContent = "";
      } catch (err) {
        status.textContent = errorText(err);
      }
    });

    return api.on.superReplied(show);
  };
}

export function messageItem(m: Message): HTMLElement {
  const who = m.author.kind === "agent" ? m.author.id ?? "agent" : m.author.kind;
  return h(
    "li",
    { class: `message ${m.author.kind} ${m.kind}` },
    h("span", { class: "author" }, who),
    h("div", { class: "text" }, m.text),
  );
}
