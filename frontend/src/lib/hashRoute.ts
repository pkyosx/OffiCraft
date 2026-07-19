/**
 * URL-hash routing — the UI's navigational state lives in `location.hash` so a
 * browser refresh (and the top-bar refresh button, which is a full reload)
 * lands back on the same page, and any view is deep-linkable.
 *
 * Anchor scheme (segments, URL-encoded ids):
 *   ``            → office roster (canonical home = clean root, no hash)
 *   #office       → office roster (alias of root)
 *   #office/chat/<memberId>                       → chat open with <memberId>
 *   #office/chat/<memberId>/msg/<msgId>           → chat open, located on <msgId>
 *   #office/chat/<memberId>/compose/<taskNo>      → chat open, composer seeded
 *                                                   with a "[<taskNo>] " prefix
 *   #office/chat/<memberId>/member/<detailId>     → member detail over that chat
 *   #office/member/<detailId>                     → member detail (no chat pick)
 *   #office/worker/<workerId>                     → outsource worker detail
 *   #replies                                      → awaiting-reply page
 *   #tasks                                        → tasks page (M3)
 *   #tasks/<taskId>                               → tasks page, located on <taskId>
 *   #tasks/executor/<memberId>                    → tasks page, filtered to that
 *                                                   member's unfinished tasks
 *   #monitor                                      → monitor page
 *   #monitor/member/<detailId>                    → monitor's member detail
 *   #settings                                     → settings overlay
 *   #settings/roles                               → settings, opened on the
 *                                                   角色誌 (roles) list (deep-link)
 *   #settings/roles/new                           → settings, roles list opened
 *                                                   in 新增角色 (create) mode
 *   #settings/roles/<roleKey>                     → settings, opened on that
 *                                                   role's definition page
 *   #settings/manuals/<typeKey>                   → settings, opened on that
 *                                                   task manual's hub (deep-link)
 *
 * Unknown / malformed hashes parse to the office home (self-healing — a stale
 * id inside a valid shape is healed by the consuming page when the lookup
 * misses). This module is the single parse/format seam; components read and
 * write routes only through `useHashRoute` so the scheme cannot drift.
 */
import { useCallback, useMemo, useSyncExternalStore } from "react";

export interface HashRoute {
  page: "office" | "replies" | "tasks" | "monitor" | "settings";
  /** office only — the member whose chat is open. */
  chatId?: string;
  /** office only, requires chatId — the message the chat locates + highlights
   * on open (B3 跳到原訊息: the reply card's originating ask message). */
  msgId?: string;
  /** office only, requires chatId — a task number (e.g. "T-7d40") whose
   * "[<taskNo>] " prefix seeds the chat composer's draft (T-e987 任務卡 →
   * 負責人/建立者 聊天跳轉: the label routes here so the owner starts a message
   * already tagged with the task). One-shot, only seeds an empty draft. */
  composeTaskNo?: string;
  /** tasks only — the task the page locates + highlights on open (§3.6 請示 →
   * 任務跳轉: the reply card's 查看任務詳情 link routes here; the page
   * guarantees visibility — auto-expands 已結束, clears hiding filters). */
  taskId?: string;
  /** tasks only — seed the 執行者 filter with this member (T-dfae 聊天 header
   * 任務圖示: "show me what THIS member still owes"). A SEPARATE segment shape
   * from `taskId` on purpose: `#tasks/<id>` means "show me THIS one task,
   * overriding every filter" (see TasksPage.matches) — the exact OPPOSITE of
   * "narrow the list down by a filter", so the two cannot share the slot.
   * One-shot, like `composeTaskNo`: the page seeds its filter state and
   * normalises the hash back to `#tasks`, leaving the owner free to widen or
   * clear the filters from the dropdowns without the route re-imposing itself.
   * "executor" is a reserved first segment (same precedent as roles' "new"), so
   * a task whose id is literally "executor" cannot be single-task deep-linked;
   * ids are server-minted `t-<hex>`, so none ever is. */
  executorId?: string;
  /** settings only — deep-link to a task manual hub (#settings/manuals/<key>).
   * A stale/unknown key self-heals to the manuals list (SettingsPage). */
  manualKey?: string;
  /** settings only — deep-link straight to the 角色誌 (roles) list
   * (#settings/roles). The 正職 group header's ➕👤 button routes here so the
   * owner lands on the role list ready to add a role. */
  settingsRoles?: boolean;
  /** settings only — deep-link straight into 角色誌 CREATE mode
   * (#settings/roles/new): the roles list opens with the inline 新增角色 row
   * already expanded. The office roster ➕👤 routes here (T-25b7). Implies
   * settingsRoles; a trailing segment other than "new" self-heals to the plain
   * roles list. */
  settingsRolesNew?: boolean;
  /** settings only — deep-link to one role's definition page
   * (#settings/roles/<roleKey>; the roster role-line gear routes here). The
   * reserved "new" segment stays the create-mode deep-link, so a role
   * literally keyed "new" cannot be deep-linked (owner-minted keys never
   * are). A stale/unknown key self-heals to the roles list (SettingsPage). */
  roleKey?: string;
  /** office / monitor — the member whose detail panel is open. */
  detailId?: string;
  /** office only — the outsource worker whose detail panel is open (mutually
   * exclusive with detailId; an anonymous live worker's lean detail view). */
  workerId?: string;
}

export function parseHash(raw: string): HashRoute {
  const segs = raw
    .replace(/^#/, "")
    .split("/")
    .filter((s) => s !== "")
    .map(decodeURIComponent);
  const [head, ...rest] = segs;

  if (head === "settings") {
    if (rest[0] === "manuals" && rest[1]) {
      return { page: "settings", manualKey: rest[1] };
    }
    if (rest[0] === "roles") {
      // #settings/roles/new opens the list already in create mode; any other
      // trailing segment is a role-definition deep-link (an unknown key
      // self-heals to the roles list on the consuming page).
      if (rest[1] === "new") {
        return { page: "settings", settingsRoles: true, settingsRolesNew: true };
      }
      return rest[1]
        ? { page: "settings", settingsRoles: true, roleKey: rest[1] }
        : { page: "settings", settingsRoles: true };
    }
    return { page: "settings" };
  }

  if (head === "replies") return { page: "replies" };

  if (head === "tasks") {
    // #tasks/executor/<memberId> narrows the list by 執行者; any other trailing
    // segment is the single-task anchor. A dangling "executor" with no id
    // self-heals to the plain tasks page (never to a task literally id'd
    // "executor").
    if (rest[0] === "executor") {
      return rest[1]
        ? { page: "tasks", executorId: rest[1] }
        : { page: "tasks" };
    }
    return rest[0] ? { page: "tasks", taskId: rest[0] } : { page: "tasks" };
  }

  if (head === "monitor") {
    if (rest[0] === "member" && rest[1]) {
      return { page: "monitor", detailId: rest[1] };
    }
    return { page: "monitor" };
  }

  // office (also the fallback for empty/unknown heads)
  const route: HashRoute = { page: "office" };
  if (head !== undefined && head !== "office") return route;
  let i = 0;
  if (rest[i] === "chat" && rest[i + 1]) {
    route.chatId = rest[i + 1];
    i += 2;
    if (rest[i] === "msg" && rest[i + 1]) {
      route.msgId = rest[i + 1];
      i += 2;
    } else if (rest[i] === "compose" && rest[i + 1]) {
      route.composeTaskNo = rest[i + 1];
      i += 2;
    }
  }
  if (rest[i] === "member" && rest[i + 1]) {
    route.detailId = rest[i + 1];
  } else if (rest[i] === "worker" && rest[i + 1]) {
    route.workerId = rest[i + 1];
  }
  return route;
}

export function formatHash(route: HashRoute): string {
  if (route.page === "settings") {
    if (route.manualKey)
      return `#settings/manuals/${encodeURIComponent(route.manualKey)}`;
    if (route.settingsRoles) {
      if (route.settingsRolesNew) return "#settings/roles/new";
      return route.roleKey
        ? `#settings/roles/${encodeURIComponent(route.roleKey)}`
        : "#settings/roles";
    }
    return "#settings";
  }
  if (route.page === "replies") return "#replies";
  if (route.page === "tasks") {
    // executor (filter) and taskId (single-task anchor) are mutually exclusive
    // — opposite semantics (narrow vs. override-all). executor wins if both are
    // somehow set; neither → the bare list.
    if (route.executorId)
      return `#tasks/executor/${encodeURIComponent(route.executorId)}`;
    return route.taskId
      ? `#tasks/${encodeURIComponent(route.taskId)}`
      : "#tasks";
  }
  if (route.page === "monitor") {
    return route.detailId
      ? `#monitor/member/${encodeURIComponent(route.detailId)}`
      : "#monitor";
  }
  const segs: string[] = [];
  if (route.chatId) {
    segs.push("chat", encodeURIComponent(route.chatId));
    // msg / compose are meaningless without an open chat — a dangling one is
    // dropped; they are mutually exclusive (msg wins if both are set).
    if (route.msgId) segs.push("msg", encodeURIComponent(route.msgId));
    else if (route.composeTaskNo)
      segs.push("compose", encodeURIComponent(route.composeTaskNo));
  }
  if (route.detailId) segs.push("member", encodeURIComponent(route.detailId));
  else if (route.workerId)
    segs.push("worker", encodeURIComponent(route.workerId));
  // Bare office is the canonical HOME → clean root (no lone "#office" clutter).
  return segs.length ? `#office/${segs.join("/")}` : "";
}

/** Write a route into the URL. A non-empty hash goes through `location.hash`
 * (fires `hashchange` natively); the empty home route strips the hash entirely
 * via pushState + a synthetic `hashchange` so subscribers still re-read. */
export function navigateHash(route: HashRoute): void {
  const next = formatHash(route);
  const cur = window.location.hash;
  if (next === cur || (next === "" && (cur === "" || cur === "#"))) return;
  if (next === "") {
    history.pushState(
      null,
      "",
      window.location.pathname + window.location.search
    );
    window.dispatchEvent(new HashChangeEvent("hashchange"));
  } else {
    window.location.hash = next;
  }
}

function subscribe(cb: () => void): () => void {
  window.addEventListener("hashchange", cb);
  return () => window.removeEventListener("hashchange", cb);
}

function snapshot(): string {
  return window.location.hash;
}

/** The current route (re-rendering on every hash change) + the setter. */
export function useHashRoute(): [HashRoute, (route: HashRoute) => void] {
  const hash = useSyncExternalStore(subscribe, snapshot);
  const route = useMemo(() => parseHash(hash), [hash]);
  const setRoute = useCallback((next: HashRoute) => navigateHash(next), []);
  return [route, setRoute];
}
