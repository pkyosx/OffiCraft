// lib/clipboard.ts — copy plain text to the clipboard, honestly.
//
// navigator.clipboard.writeText is the primary path, but it only exists in a
// secure context (https / localhost); over plain http it is undefined and any
// call throws. So we fall back to the legacy execCommand("copy") via an
// off-screen textarea, which works in non-secure contexts too. The function
// resolves to `true` ONLY when a copy actually succeeded — callers key their
// 「已複製」feedback on that and NEVER fake success on failure.

export async function copyText(text: string): Promise<boolean> {
  // Primary: the async Clipboard API (secure contexts only).
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // fall through to the legacy path (permission denied / non-secure)
    }
  }
  // Fallback: a hidden textarea + execCommand("copy"). Deprecated but still the
  // only clipboard write available over plain http.
  try {
    const ta = document.createElement("textarea");
    ta.value = text;
    // Keep it out of view and out of the layout's way while still selectable.
    ta.setAttribute("readonly", "");
    ta.style.position = "fixed";
    ta.style.top = "-9999px";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand("copy");
    document.body.removeChild(ta);
    return ok;
  } catch {
    return false;
  }
}
