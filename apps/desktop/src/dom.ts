// Small DOM helpers. Replace them when the app adopts a UI framework.

export function h<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  props: Partial<HTMLElementTagNameMap[K]> & { class?: string } = {},
  ...children: (Node | string)[]
): HTMLElementTagNameMap[K] {
  const el = document.createElement(tag);
  const { class: cls, ...rest } = props;
  if (cls) el.className = cls;
  Object.assign(el, rest);
  el.append(...children);
  return el;
}

/** errorText returns the message of a rejected call. */
export function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** A view mounts into a root element and returns its cleanup function. */
export type View = (root: HTMLElement) => () => void;
