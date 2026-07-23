// wording.ts — T-16a1 P3b: apply a theme's per-language 用詞 (wording) overlay
// on top of a base locale dictionary, and read a single message by its internal
// code (dotted path, e.g. "profile.themeOffice"). Both the i18n provider (to
// APPLY an active custom theme's overlay) and the wording editor (to SHOW the
// current text of a code) go through here.
//
// The dictionary holds STRING leaves and interpolation-FUNCTION leaves. Only
// string leaves are overridable (the message-key whitelist excludes functions),
// so a function leaf is never clobbered. Unmodified subtrees — functions
// included — are shared by reference: the override copies only the objects along
// each overridden path, so applying an overlay is cheap and structuralClone
// (which cannot clone functions) is never needed.

/** Read a message by its dotted code. Returns the string leaf value, or null
 * when the path is missing or resolves to a non-string (e.g. a function leaf). */
export function readDictMessage(dict: unknown, code: string): string | null {
  let node: unknown = dict;
  for (const seg of code.split(".")) {
    if (typeof node !== "object" || node === null) return null;
    node = (node as Record<string, unknown>)[seg];
  }
  return typeof node === "string" ? node : null;
}

function setPath(
  node: Record<string, unknown>,
  path: string[],
  value: string
): Record<string, unknown> {
  const [head, ...rest] = path;
  const copy: Record<string, unknown> = { ...node };
  if (rest.length === 0) {
    // Only override an existing STRING leaf — never invent a key or clobber a
    // function/object (the whitelist already guarantees a string leaf, but the
    // overlay is user data, so guard anyway).
    if (typeof copy[head] === "string") copy[head] = value;
    return copy;
  }
  const child = copy[head];
  if (typeof child === "object" && child !== null && !Array.isArray(child)) {
    copy[head] = setPath(child as Record<string, unknown>, rest, value);
  }
  return copy;
}

/**
 * Return `base` with each `code → text` in `overlay` applied at its dotted path.
 * `base` is returned unchanged (same reference) when there is nothing to apply,
 * so the common no-wording case never repaints. `D` is the dictionary type.
 */
export function applyWording<D>(
  base: D,
  overlay: Record<string, string> | undefined
): D {
  if (!overlay) return base;
  const codes = Object.keys(overlay);
  if (codes.length === 0) return base;
  let result = base as unknown as Record<string, unknown>;
  for (const code of codes) {
    const text = overlay[code];
    if (typeof text !== "string" || text.trim() === "") continue;
    result = setPath(result, code.split("."), text);
  }
  return result as unknown as D;
}
