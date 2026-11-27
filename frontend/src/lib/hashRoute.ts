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
 *   #office/member/<detailId>/from/replies/<id>   → member detail, 返回 lands
 *                                                   on that reply card
 *   #office/worker/<workerId>                     → outsource worker detail
 *   #office/worker/<workerId>/from/replies/<id>   → worker detail, same 返回
 *                                                   destination as above
 *   #replies                                      → awaiting-reply page
 *   #replies/card/<replyCardId>                   → awaiting-reply page,
 *                                                   FILTERED to that card
 *   #replies/opener/<memberId>                    → awaiting-reply page, only
 *                                                   the 待回覆 section, 開卡人
 *                                                   seeded with that member
 *   #tasks                                        → tasks page (M3)
 *   #tasks/<taskId>                               → tasks page, located on <taskId>
 *   #tasks/executor/<memberId>                    → tasks page, filtered to that
 *                                                   member's unfinished tasks
 *   #lore                                         → 傳承 directory (one row per
 *                                                   target: 所有人, members, manuals)
 *   #lore/everyone                                → the 所有人 section page
 *   #lore/agent/<memberId>                        → that member's section page
 *   #lore/manual/<typeKey>                        → that manual's section page
 *   #lore/<…section…>/entry/<entryId>             → section page, that entry
 *                                                   highlighted
 *   #shared-docs                                  → 檔案庫 list
 *   #shared-docs/<docId>                          → that shared document
 *   #secrets                                      → Secrets page
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
 *   #settings/guide                               → settings, 使用說明 (product
 *                                                   guide) list
 *   #settings/guide/<docSlug>                     → settings, that guide doc
 *   #guide, #guide/<docSlug>                      → legacy aliases of the two
 *                                                   above (links already pasted
 *                                                   into chats and docs)
 *
 * Unknown / malformed hashes parse to the office home (self-healing — a stale
 * id inside a valid shape is healed by the consuming page when the lookup
 * misses). This module is the single parse/format seam; components read and
 * write routes only through `useHashRoute` so the scheme cannot drift.
 */
import { useCallback, useMemo, useSyncExternalStore } from "react";

export type LoreSectionKind = "everyone" | "agent" | "manual";

export interface HashRoute {
  page:
    | "office"
    | "replies"
    | "tasks"
    | "lore"
    | "shared_docs"
    | "secrets"
    | "monitor"
    | "settings";
  /** replies only — the card to show (e.g. a Web Push tap, or a link an agent
   * pasted into chat).
   *
   * T-93: this SEEDS the page's ID 篩選 — the list narrows to that one card and
   * stays narrowed, which is the effect. The scroll-and-focus that used to be
   * the whole of it is now secondary and happens AFTER the filter: it only
   * saves the reader a scroll when the hit sits in the collapsed 近期已處理
   * pane. Do not describe this as a locate mechanism; without it the filter
   * still works, and the owner's way out is 清除篩選, not the hash clearing
   * itself. */
  replyCardId?: string;
  /** replies only — the 聊天 header's 請示卡 jump: seeds the 開卡人 axis with
   * this member and shows only the 待回覆 section for as long as the route
   * stays. "opener" is a reserved segment, like tasks' "executor". */
  openerId?: string;
  /** office only — the member whose chat is open. */
  chatId?: string;
  /** office only, requires chatId — the message the chat locates + highlights
   * on open (B3 跳到原訊息: the reply card's originating ask message). */
  msgId?: string;
  /** office only, requires chatId — a task number, which IS the task id
   * (e.g. "t-7d40aabbccdd"; the four-hex "T-7d40" projection was retired by
   * T-5291), whose
   * "[<taskNo>] " prefix seeds the chat composer's draft (T-e987 任務卡 →
   * 負責人/建立者 聊天跳轉: the label routes here so the owner starts a message
   * already tagged with the task). One-shot, only seeds an empty draft. */
  composeTaskNo?: string;
  /** tasks only — the task the page narrows to + highlights on open (§3.6 請示
   * → 任務跳轉: the reply card's 查看任務 chevron routes here; the page
   * guarantees visibility — expands its lane, overrides the other filters).
   *
   * T-93 (owner 2026-09-05, `rc-428906235337`): when the id names NO task the
   * anchor STAYS and the page answers 沒有符合篩選條件的任務. It used to strip
   * itself back to `#tasks` in silence, which made a broken link and a link
   * that was never filtering look identical. A load FAILURE that is not a 404
   * is a third thing again — the page shows its load error. */
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
  /** settings only — the 使用說明 (product guide) is open (#settings/guide).
   * Owner ruling: the guide lives in Settings, not in a nav tab. */
  settingsGuide?: boolean;
  /** settings only, requires settingsGuide — the doc being read
   * (#settings/guide/<slug>); absent → the doc list. An unknown slug is
   * answered by the guide page with its load error. */
  guideSlug?: string;
  /** shared_docs only — the document open (#shared-docs/<id>); absent → the
   * list. An unknown id is answered by the page, not healed here. */
  sharedDocId?: string;
  /** office / monitor — the member whose detail panel is open. */
  detailId?: string;
  /** office only — the outsource worker whose detail panel is open (mutually
   * exclusive with detailId; an anonymous live worker's lean detail view). */
  workerId?: string;
  /** office only, requires detailId or workerId — where a same-page 返回 on
   * the open detail/worker panel should land INSTEAD of the office page's own
   * chat-view reset (T-a706: a card avatar elsewhere, e.g. RepliesPage, deep-
   * links straight into #office/member/<id> with no chat context of its own;
   * without this marker 返回 fell through to the roster's default chat,
   * dropping the owner into an unrelated chat room instead of back where they
   * came from). Extend the union as more surfaces deep-link in. */
  backTo?: "replies" | "tasks";
  /** replies/tasks only — the item whose deep link should be restored on return. */
  backToId?: string;
  /** lore only — the section page; absent ⇒ the directory. `key` is "" for
   * 所有人. */
  loreSection?: { kind: LoreSectionKind; key: string };
  /** lore only, with loreSection — the entry to highlight on that page. */
  loreEntryId?: string;
}

/** `decodeURIComponent` THROWS on a malformed escape — a lone `%`, `%zz` — and
 * `parseHash` runs inside `useHashRoute`'s useMemo, which App calls at the root
 * with no ErrorBoundary anywhere in src/ (measured; see http.ts). So one
 * mangled character in a pasted URL took the whole page white instead of just
 * failing to filter. That is the exact shape T-93 exists to prevent: the ticket
 * is "an agent pastes a link and the owner clicks it", and a link that survives
 * a chat client, a terminal wrap and a manual re-paste is not guaranteed to
 * still be well-formed. An undecodable segment is passed through RAW — it then
 * simply matches no card, which is the honest "沒有符合篩選條件" answer. */
function decodeSegment(seg: string): string {
  try {
    return decodeURIComponent(seg);
  } catch {
    return seg;
  }
}

function guideRoute(slug: string | undefined): HashRoute {
  return slug
    ? { page: "settings", settingsGuide: true, guideSlug: slug }
    : { page: "settings", settingsGuide: true };
}

export function parseHash(raw: string): HashRoute {
  const segs = raw
    .replace(/^#/, "")
    .split("/")
    .filter((s) => s !== "")
    .map(decodeSegment);
  const [head, ...rest] = segs;

  if (head === "guide") return guideRoute(rest[0]);

  if (head === "settings") {
    if (rest[0] === "guide") return guideRoute(rest[1]);
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

  if (head === "replies") {
    if (rest[0] === "card" && rest[1]) {
      return { page: "replies", replyCardId: rest[1] };
    }
    return rest[0] === "opener" && rest[1]
      ? { page: "replies", openerId: rest[1] }
      : { page: "replies" };
  }

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

  // 傳承: the hash names a section (and an entry to highlight), never a
  // filter. The rows and their numbers are the server's answer for the filter
  // actually sent, so a hash-seeded filter would answer a question not on screen.
  if (head === "lore") {
    let section: HashRoute["loreSection"];
    let i = 0;
    if (rest[0] === "everyone") {
      section = { kind: "everyone", key: "" };
      i = 1;
    } else if ((rest[0] === "agent" || rest[0] === "manual") && rest[1]) {
      section = { kind: rest[0], key: rest[1] };
      i = 2;
    }
    if (!section) return { page: "lore" };
    return rest[i] === "entry" && rest[i + 1]
      ? { page: "lore", loreSection: section, loreEntryId: rest[i + 1] }
      : { page: "lore", loreSection: section };
  }

  if (head === "shared-docs") {
    return rest[0]
      ? { page: "shared_docs", sharedDocId: rest[0] }
      : { page: "shared_docs" };
  }

  if (head === "secrets") {
    return { page: "secrets" };
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
    i += 2;
  } else if (rest[i] === "worker" && rest[i + 1]) {
    route.workerId = rest[i + 1];
    i += 2;
  }
  if (
    (route.detailId || route.workerId) &&
    rest[i] === "from" &&
    (rest[i + 1] === "replies" || rest[i + 1] === "tasks")
  ) {
    route.backTo = rest[i + 1] as "replies" | "tasks";
    if (rest[i + 2]) route.backToId = rest[i + 2];
  }
  return route;
}

export function formatHash(route: HashRoute): string {
  if (route.page === "settings") {
    if (route.settingsGuide)
      return route.guideSlug
        ? `#settings/guide/${encodeURIComponent(route.guideSlug)}`
        : "#settings/guide";
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
  if (route.page === "replies")
    return route.replyCardId
      ? `#replies/card/${encodeURIComponent(route.replyCardId)}`
      : route.openerId
        ? `#replies/opener/${encodeURIComponent(route.openerId)}`
        : "#replies";
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
  if (route.page === "lore") {
    const sec = route.loreSection;
    if (!sec) return "#lore";
    const base =
      sec.kind === "everyone" ? "#lore/everyone" : `#lore/${sec.kind}/${encodeURIComponent(sec.key)}`;
    return route.loreEntryId ? `${base}/entry/${encodeURIComponent(route.loreEntryId)}` : base;
  }
  if (route.page === "shared_docs")
    return route.sharedDocId
      ? `#shared-docs/${encodeURIComponent(route.sharedDocId)}`
      : "#shared-docs";
  if (route.page === "secrets") return "#secrets";
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
  // backTo is meaningless without an open detail/worker panel — same
  // drop-if-dangling rule as msg/compose above.
  if ((route.detailId || route.workerId) && route.backTo)
    segs.push(
      "from",
      route.backTo,
      ...(route.backToId ? [encodeURIComponent(route.backToId)] : []),
    );
  // Bare office is the canonical HOME → clean root (no lone "#office" clutter).
  return segs.length ? `#office/${segs.join("/")}` : "";
}

/** Asked before the route changes; false keeps the current route. One at a
 * time — the page holding unsaved work installs it. */
export type NavigationGuard = (next: HashRoute) => boolean;
let navigationGuard: NavigationGuard | null = null;
// The last hash the guard let through. A refused browser back/forward or a
// hand-edited hash is put back to this.
let acceptedHash: string | null = null;
// Each history entry this page sees is stamped with its position in
// history.state, so a refused Back/Forward can be undone with history.go()
// instead of rewriting an entry (which would erase the reader's back history).
// A new entry is inserted right after the current one (forward entries are
// dropped), so it is always the accepted position + 1.
const HIST_IDX = "ocHistIdx";
let acceptedIdx = 0;
// history.length when the accepted entry was taken: a refused entry with no
// stamp and a longer history is a new one (a typed hash); with the same length
// it is an older entry reached by Back that predates the stamps.
let acceptedLength = 0;
// While the guard is asked and a refusal is walked back with history.go() /
// back(), the route stays pinned to the accepted page. The pin ends when the
// walk-back lands (popstate / hashchange). Should it never land (a go() with
// nowhere to go), a fallback timer started at the go() puts the accepted URL
// back and ends it.
let restoring = false;
let restoreTimer: ReturnType<typeof setTimeout> | null = null;
const RESTORE_FALLBACK_MS = 1000;

function clearRestoreTimer(): void {
  if (restoreTimer !== null) {
    clearTimeout(restoreTimer);
    restoreTimer = null;
  }
}

/** Unpin. A URL that is not the accepted one is put back first, so the pin
 * never hands the route to a page the guard refused. */
function endRestoring(): void {
  if (!restoring) return;
  clearRestoreTimer();
  const before = snapshot();
  if (acceptedHash !== null && window.location.hash !== acceptedHash) {
    history.replaceState(history.state, "", window.location.pathname + window.location.search + acceptedHash);
  }
  restoring = false;
  if (snapshot() !== before) for (const cb of listeners) cb();
}

function startRestoreTimer(): void {
  clearRestoreTimer();
  restoreTimer = setTimeout(endRestoring, RESTORE_FALLBACK_MS);
}

/** Test-only: forget the module's history bookkeeping between tests, so a
 * timer or pin left by one test cannot act in the next. */
export function __resetHashRouteForTests(): void {
  clearRestoreTimer();
  restoring = false;
  navigationGuard = null;
  acceptedHash = null;
  acceptedIdx = 0;
  acceptedLength = 0;
}

function entryIdx(): number | undefined {
  const st: unknown = history.state;
  if (st && typeof st === "object") {
    const v = (st as Record<string, unknown>)[HIST_IDX];
    if (typeof v === "number") return v;
  }
  return undefined;
}

function stampEntry(idx: number): void {
  const st: unknown = history.state;
  const base = st && typeof st === "object" ? (st as Record<string, unknown>) : {};
  history.replaceState({ ...base, [HIST_IDX]: idx }, "");
}

function acceptEntry(): void {
  const idx = entryIdx();
  if (idx === undefined) {
    acceptedIdx += 1;
    stampEntry(acceptedIdx);
  } else {
    acceptedIdx = idx;
  }
  acceptedLength = history.length;
}

export function setNavigationGuard(guard: NavigationGuard): () => void {
  navigationGuard = guard;
  return () => {
    if (navigationGuard === guard) navigationGuard = null;
  };
}

/** Write a route into the URL. A non-empty hash goes through `location.hash`
 * (fires `hashchange` natively); the empty home route strips the hash entirely
 * via pushState + a synthetic `hashchange` so subscribers still re-read. */
export function navigateHash(route: HashRoute): void {
  const next = formatHash(route);
  const cur = window.location.hash;
  if (next === cur || (next === "" && (cur === "" || cur === "#"))) return;
  if (navigationGuard && !navigationGuard(route)) return;
  acceptedHash = next;
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

const listeners = new Set<() => void>();

function onHashChange(): void {
  const cur = window.location.hash;
  const prev = acceptedHash ?? "";
  if (cur !== prev && navigationGuard) {
    // Pinned before asking: the guard itself may re-render the page.
    restoring = true;
    if (!navigationGuard(parseHash(cur))) {
      const idx = entryIdx();
      if (idx !== undefined && idx !== acceptedIdx) {
        // Back/Forward onto an entry we know: step back to where we were. The
        // hashchange that brings us there matches acceptedHash and passes.
        startRestoreTimer();
        history.go(acceptedIdx - idx);
      } else if (idx === undefined && history.length > acceptedLength) {
        // A new entry (a hand-typed hash): return to the accepted one behind it.
        startRestoreTimer();
        history.back();
      } else if (idx === undefined) {
        // An unstamped older entry reached by Back: stepping further back would
        // walk the reader out of the app, so put the accepted page on top of it.
        history.pushState({ [HIST_IDX]: acceptedIdx }, "", window.location.pathname + window.location.search + prev);
        acceptedLength = history.length;
        endRestoring();
      } else {
        endRestoring();
      }
      return;
    }
    restoring = false;
  }
  endRestoring();
  acceptEntry();
  acceptedHash = cur;
  for (const cb of listeners) cb();
}

function subscribe(cb: () => void): () => void {
  if (listeners.size === 0) {
    acceptedHash = window.location.hash;
    acceptEntry();
    window.addEventListener("hashchange", onHashChange);
    // By popstate the traversal has landed, so the URL is right to read again.
    window.addEventListener("popstate", endRestoring);
  }
  listeners.add(cb);
  return () => {
    listeners.delete(cb);
    if (listeners.size === 0) {
      window.removeEventListener("hashchange", onHashChange);
      window.removeEventListener("popstate", endRestoring);
    }
  };
}

// Between a refused Back and the history.go() that undoes it, the URL briefly
// shows the refused page; a render in that gap must read the accepted one (the
// lore page would otherwise drop its staged work for a section change that
// never happened).
function snapshot(): string {
  return restoring && acceptedHash !== null ? acceptedHash : window.location.hash;
}

/** The current route (re-rendering on every hash change) + the setter. */
export function useHashRoute(): [HashRoute, (route: HashRoute) => void] {
  const hash = useSyncExternalStore(subscribe, snapshot);
  const route = useMemo(() => parseHash(hash), [hash]);
  const setRoute = useCallback((next: HashRoute) => navigateHash(next), []);
  return [route, setRoute];
}
