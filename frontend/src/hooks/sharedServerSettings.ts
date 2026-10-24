// hooks/sharedServerSettings.ts — the ONE /api/settings read a cockpit load
// makes (T-8115). See lib/sharedSnapshot.ts for the merge / cache / generation
// / invalidation contract; this file is only the wiring.
//
// Every mount-fetch consumer of the settings snapshot calls `loadServerSettings`
// instead of `api.getServerSettings` directly, and every successful PATCH hands
// its echoed effective values to `adoptServerSettings` — that echo is what
// keeps this tab's copy true (it is also the ONLY thing that does: the server
// pushes no settings delta, so another tab's save is invisible here until a
// reload, a known and accepted boundary). One exception: a change to
// `task_due_soon_lead_days` rides a nameless `task` delta, and
// useDueSoonLeadDays answers it with `refreshServerSettings`.
//
// Two callers deliberately do NOT read from the cache and use
// `refreshServerSettings`:
//   - the onboarding poll, which exists to WATCH a value change (a cached
//     answer would make it poll its own memory forever);
//   - 設定's 存檔測連通 read-back, whose entire job is to prove the server
//     agrees with what was just written.

import { api, type ServerSettingsView } from "../api";
import { AUTH_LOGIN_EVENT } from "../api/auth";
import { AUTH_EXPIRED_EVENT } from "../api/client";
import {
  createSharedSnapshot,
  registerSharedSnapshot,
} from "../lib/sharedSnapshot";

const snapshot = registerSharedSnapshot(
  createSharedSnapshot<ServerSettingsView>(() => api.getServerSettings()),
);

/** Mount-fetch path: merged with every other caller asking in the same moment,
 * then served from the cached answer. */
export function loadServerSettings(): Promise<ServerSettingsView> {
  return snapshot.load();
}

/** A real GET, always — for the callers that must observe the server rather
 * than remember it. Its answer becomes the shared copy. */
export function refreshServerSettings(): Promise<ServerSettingsView> {
  return snapshot.refresh();
}

/** THE invalidation point: this tab's own save succeeded and the server echoed
 * the effective values. Retires anything in flight so a slower older GET cannot
 * overwrite it. */
export function adoptServerSettings(next: ServerSettingsView): void {
  snapshot.adopt(next);
}

// The cached settings belong to ONE session. A login mints a different identity
// and an auth-expiry ends this one, so neither may inherit the other's copy.
if (typeof window !== "undefined") {
  const drop = () => snapshot.invalidate();
  window.addEventListener(AUTH_LOGIN_EVENT, drop);
  window.addEventListener(AUTH_EXPIRED_EVENT, drop);
}
