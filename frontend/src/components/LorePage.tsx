// LorePage — the 傳承 tab (T-33, spec §6).
//
// WHAT THIS PAGE IS. A read-and-curate log. 傳承 entries are written by AGENTS
// through MCP and are NEVER editable (adapter: no route changes `title`/`body`),
// so there is no compose form and no edit affordance here. The four things the
// owner can do to an entry are 失效 / 生效 / 置頂 / 提到最新, and that is the
// whole mutable surface.
//
// IT WEARS THE 任務卡 DESIGN LANGUAGE, COPIED FROM TaskCard.tsx RATHER THAN
// SHARED. The classes below are `.lore-*` twins of `.task-card__*`, living in
// lore.css, because tasks.css is the 任務 page's sheet and this page must not
// be able to change how a task card looks by adjusting its own row. What was
// copied, and from where:
//
//   · whole-row expand/collapse with the closest() interaction filter
//     (TaskCard.tsx onCardToggleClick / onCardToggleKeyDown). A click that
//     lands on a button / menu / select does its own job; a click that ends a
//     text selection is reading, not a toggle.
//   · the ▸/▾ indicator: a DIFFERENT ICON, not a rotation, and a pure state
//     mirror — aria-hidden, pointer-events:none, no role, so a click on it
//     falls through to the row toggle (TaskCard.tsx:1395-1401).
//   · one status badge that DROPS A MENU from its left edge
//     (.lore-row__status-pop { left: 0; right: auto }), the current value
//     carrying `--active` = accent + 700 (tasks.css .task-card__menu-item--active).
//   · the label/value grid + the chat-bubble chip for the person row
//     (.task-card__meta / __chip).
//
// 🔴 THE 上限線 IS THE SERVER'S ANSWER, NEVER A SUM COMPUTED HERE.
// `listLoreEntries` returns `capChars` (the budget of the scope the request
// converged on) and `firstDroppedId` (the id of the entry immediately BELOW the
// line). This page reads those two fields and nothing else. Adding up the
// title+body lengths of the rows on screen would be wrong on every page after
// the first — the page is cut by `limit`/`offset` long before the budget is
// spent — and a line in the wrong place looks exactly like a line in the right
// place. Both empty (`capChars === 0` / `firstDroppedId === ""`) ⇒ the filter
// did not converge on ONE scope (or the whole scope fits): no line, no dimming.
// Whether the filter converged is the SERVER'S judgement, reported through
// `capChars`; this page never decides it.
//
// 🔴 THE FILTER TRAVELS WITH THE PAGE REQUEST. Every narrowing is a query
// parameter on `listLoreEntries`, never a `.filter()` over a downloaded list. A
// page that filtered after paging could not tell 「這一頁剛好沒有」 from
// 「根本沒有」, and its 上限線 would answer a different question than the one on
// screen. That is also why changing any filter throws the loaded rows away and
// re-asks from offset 0 (see the load effect): appending page 2 of the OLD
// question under page 1 of the new one is the same bug wearing a scrollbar.
//
// THE ORDER IS FIXED AND IS NOT A CONTROL. 置頂 → 生效中 → 已失效 (LORE_GROUPS
// below); newest effective first WITHIN a group is the server's, and this page
// preserves the order it received. Only the FILTER is the owner's to change.
//
// NOT HERE, ON PURPOSE:
//   · 轉派 — a 傳承 entry has no executor to hand over to (spec §6: 不做轉派).
//   · counts / progress bars / percentages — spec §6 names all three as things
//     the 上限線 must not become. The line says where the budget runs out; it
//     does not report how full it is.
// WAS "NOT HERE, ON PURPOSE", NOW ON EVERY ROW — 屬於 (owner, card
// rc-11734523eb52). The old reason was that the 任務 dropdown already says which
// scope is on screen, so stamping it per row would repeat the filter N times.
// That reason only holds on a FILTERED screen. On 全部 — which is where the page
// opens — every scope is mixed together and the reader has no way to tell a
// 角色傳承 from one manual's, which is exactly what the owner hit. The filter
// answers 「我要看哪一批」; the row has to answer 「這一筆是誰的」, and those are
// different questions.

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import type {
  LoreEntryState,
  LoreEntryView,
  LoreListOptions,
} from "../api/adapter";
import { isHttpStatus, serverMessageOf } from "../api/errors";
import { useMembers } from "../hooks/useMembers";
import { useOutsourceWorkers } from "../hooks/useOutsourceWorkers";
import { useIsMobile } from "../hooks/useIsMobile";
import { useTaskManuals } from "../hooks/useTaskManuals";
import {
  useWorkerAvatarUrls,
  useWorkerCodenames,
} from "../hooks/useWorkerCodenames";
import { avatarKindForMember } from "../lib/avatarKind";
import { copyText } from "../lib/clipboard";
import { formatAbsolute } from "../lib/dateFormat";
import { autosizeTextarea } from "../lib/autosize";
import { enterShouldSend } from "../lib/composerKeys";
import { navigateHash } from "../lib/hashRoute";
import { Avatar } from "./Avatar";
import { FilterPanel } from "./FilterPanel";
import { MultiSelectFilter } from "./MultiSelectFilter";
import {
  BookIcon,
  ChatBubbleIcon,
  CheckIcon,
  ChevronDownIcon,
  ChevronRightIcon,
} from "./icons";
import "./lore.css";

/** 捲到底一次載這麼多 (spec §6). The SAME number is the "is there more" test:
 * a page shorter than this is the last one. */
const PAGE = 30;

/** 捲到底 = the scrollport is within this many px of its own bottom. Copied
 * from ChatArea's scroll gate (ChatArea.tsx:994-1002) so the two ends of the
 * app agree on what "at the edge" means. */
const NEAR_BOTTOM_PX = 120;

/** 🔴 THE THREE GROUPS, IN THE ONE ORDER SPEC §6 ALLOWS. This array IS the
 * order — the render walks it — so reordering it is the only way to change the
 * groups' order, and there is no second copy of the rule to drift from it.
 * 置頂 leads because a pinned entry is the one somebody deliberately put in
 * front; 已失效 trails because it is history. Not configurable: the spec says
 * 排序固定不給改. */
const LORE_GROUPS: readonly LoreEntryState[] = ["pinned", "active", "retired"];

/** The state-change menu, in the order the owner asked for: 失效 / 生效 / 置頂. */
const STATE_ACTIONS: readonly LoreEntryState[] = ["retired", "active", "pinned"];

/** How one author id resolves against the LIVE roster. `peerId === ""` means
 * "not reachable" and is what drops the 傳訊息 icon — see LoreAuthorChip. */
interface AuthorIdentity {
  text: string;
  peerId: string;
}

/** The two scopes that can be ASKED for — the same closed set
 * `LoreListOptions.scopeKind` names. */
type LoreScopeKind = NonNullable<LoreListOptions["scopeKind"]>;

/** 屬於 packs BOTH halves of a scope into one option value: `"<kind>:<key>"`.
 *
 * 🔴 THE PREFIX IS NOT DECORATION AND MUST NOT BE DROPPED. The 屬於 list mixes
 * two kinds of thing — members and task manuals — and their keys come from
 * different namespaces that nothing keeps apart: a member id is minted by the
 * server, a manual's `type_key` is typed by a person, and a station whose
 * manual is called `mira` would make the bare key ambiguous. The pair, kept
 * together from the option straight through to the request, is what stops one
 * tick asking about two different things.
 *
 * It is also what lets the request name the KINDS: `scope_kinds` and
 * `scope_keys` are separate axes on the wire, so the page has to be able to say
 * "these keys, and only these kinds" — which it cannot do from bare keys. */
function belongsValue(kind: LoreScopeKind, key: string): string {
  return `${kind}:${key}`;
}

function splitBelongs(v: string): { kind: LoreScopeKind; key: string } {
  const i = v.indexOf(":");
  return { kind: v.slice(0, i) as LoreScopeKind, key: v.slice(i + 1) };
}

export function LorePage() {
  const { t, msg } = useI18n();
  const { members } = useMembers();
  const { workers } = useOutsourceWorkers();
  const { manuals } = useTaskManuals();

  // ── 篩選: THREE AXES, ALL QUERY PARAMETERS, none a client-side pass ────────
  //
  // 🔴 THERE USED TO BE FOUR CONTROLS AND NOW THERE ARE THREE (owner
  // 2026-09-07, card rc-a43100fd0486). His words: 「我從使用者或是任務手冊作為
  // filter 另外就是狀態 三個而已」「你完全可以抄 task」. So the row is
  // 撰寫人 → 屬於 → 狀態 → 清除篩選, which is the 任務頁's shape
  // (任務編號 → 負責人 → 類型 → 狀態 → 清除篩選) with its search box dropped —
  // 傳承 has no id anybody types.
  //
  // 🔴 THE 範圍 DROPDOWN IS GONE, AND ITS REMOVAL IS THE POINT RATHER THAN A
  // SIMPLIFICATION. It offered 角色傳承 / 成員傳承 / 任務傳承 — and 角色傳承 no
  // longer exists (the scopes collapsed to two in the same ruling), which left
  // it a two-item list whose two items are exactly the two halves of 屬於. A
  // control that asks "which kind?" beside one that asks "which one?" is two
  // questions where the reader has one.
  //
  // 屬於 asks the whole question at once: every MEMBER and every TASK MANUAL in
  // one multi-select. Its option values carry the kind (see `belongsValue`), so
  // the request still names both wire axes precisely.
  //
  // ⚠️ NO COUNT BADGE, DELIBERATELY. The 任務頁's 狀態 pill reads 「狀態 · N」;
  // this one must not. Owner, same card: 「we dont need count」 — and the page
  // could not honestly produce one anyway, because 傳承 loads by scrolling, so
  // any number computed here counts the rows fetched SO FAR and would keep
  // changing as the reader scrolls without anything having changed.
  //
  // 🔴 THE 上限線 STILL NEEDS EXACTLY ONE SCOPE, and 屬於 is what produces one:
  // ticking a single member (or a single manual) sends one kind and one key,
  // which is the server's condition for answering `capChars`. Tick two and the
  // server answers 0/"" and the page draws no line — the same honest answer an
  // unfiltered page gets. The page never re-derives that rule; it reads the
  // server's answer.
  const [belongs, setBelongs] = useState<Set<string>>(new Set());
  const [states, setStates] = useState<Set<LoreEntryState>>(new Set());
  const [authors, setAuthors] = useState<Set<string>>(new Set());

  // 🔴 THE KIND SET IS DERIVED FROM WHAT IS TICKED, NEVER SENT WHOLE. Ticking
  // only members sends `scope_kinds=[agent]`; only manuals sends `[manual]`;
  // both sends both. Sending both kinds unconditionally would widen every
  // member-only page to include manuals, because the two axes are ANDed on the
  // wire and a kind nobody ticked would still admit its rows.
  //
  // ⚠️ WHAT THE AND-ING CANNOT EXPRESS, stated because it is a real (small) gap
  // rather than a thing this page gets right. With one member and one manual
  // ticked the request is (kind ∈ {agent, manual}) AND (key ∈ {m-x, tm-y}) — so
  // a manual whose `type_key` happened to be `m-x` would also match. The wire
  // has no per-pair form to say otherwise, and this is strictly narrower than
  // what the page sent before, so it is left as is and written down here rather
  // than papered over with a client-side pass (which would break paging: see
  // this file's header).
  const opts = useMemo<LoreListOptions>(() => {
    const next: LoreListOptions = {};
    const kinds = new Set<LoreScopeKind>();
    const keys: string[] = [];
    for (const v of belongs) {
      const { kind, key } = splitBelongs(v);
      kinds.add(kind);
      keys.push(key);
    }
    if (kinds.size > 0) next.scopeKinds = [...kinds];
    if (keys.length > 0) next.scopeKeys = keys;
    if (states.size > 0) next.states = [...states];
    if (authors.size > 0) next.authorIds = [...authors];
    return next;
  }, [belongs, states, authors]);

  // What 清除篩選 keys on: whether anything the reader can SEE is narrowing the
  // page. With one control per axis this is now exactly "is any set non-empty",
  // and there is no longer a way for a tick to be held in state while not being
  // in force — which is what the old four-control version had to reason about.
  const anyFilter = belongs.size > 0 || states.size > 0 || authors.size > 0;

  function clearFilters() {
    setBelongs(new Set());
    setStates(new Set());
    setAuthors(new Set());
  }

  // ── the page's own data ────────────────────────────────────────────────
  const [entries, setEntries] = useState<LoreEntryView[]>([]);
  const [cap, setCap] = useState({ capChars: 0, firstDroppedId: "" });
  const [loading, setLoading] = useState(true);
  const [fetchingMore, setFetchingMore] = useState(false);
  const [error, setError] = useState(false);
  const [exhausted, setExhausted] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  // Bumped after a successful mutation: 失效/生效/置頂/提到最新 all move an entry
  // between groups or within one, which moves every later page's offset AND can
  // move the 上限線. Re-asking from offset 0 is the only answer that stays
  // consistent; patching the row in place would leave the line where it was
  // drawn for the previous arrangement.
  const [reloadNonce, setReloadNonce] = useState(0);

  // 🔴 ONE REQUEST IN FLIGHT AT A TIME, and the latch remembers WHICH question
  // it is answering (the generation), not just that something is flying. A
  // filter change bumps the generation and frees the latch at once, so the
  // abandoned request can neither write its answer (the gen guard in .then)
  // nor free the latch out from under the new one (the gen guard in .finally).
  const genRef = useRef(0);
  const flightRef = useRef(0); // 0 = idle, else the gen of the request in flight

  // Read by the paging callback so it does not have to be re-created (and the
  // scroll listener re-attached) on every keystroke of state.
  const entriesRef = useRef(entries);
  entriesRef.current = entries;
  const optsRef = useRef(opts);
  optsRef.current = opts;

  const filterKey = JSON.stringify(opts);

  // FIRST PAGE — and the reset. Everything loaded for the previous filter is
  // dropped here rather than filtered down: the rows below came from
  // `offset` values that mean something different now.
  useEffect(() => {
    genRef.current += 1;
    const gen = genRef.current;
    flightRef.current = gen;
    setEntries([]);
    setCap({ capChars: 0, firstDroppedId: "" });
    setExhausted(false);
    setError(false);
    setLoading(true);
    let alive = true;
    api
      .listLoreEntries({ ...optsRef.current, limit: PAGE, offset: 0 })
      .then((page) => {
        if (!alive || gen !== genRef.current) return;
        setEntries(page.entries);
        setCap({
          capChars: page.capChars,
          firstDroppedId: page.firstDroppedId,
        });
        setExhausted(page.entries.length < PAGE);
      })
      .catch((e) => {
        console.warn("LorePage: initial load failed", e);
        if (alive && gen === genRef.current) setError(true);
      })
      .finally(() => {
        if (alive && gen === genRef.current) setLoading(false);
        if (flightRef.current === gen) flightRef.current = 0;
      });
    return () => {
      alive = false;
    };
  }, [filterKey, reloadNonce]);

  // NEXT PAGE — 捲到底載 30 筆. `offset` is how many rows are already held, and
  // the SAME filter rides along (that is the whole point of §6's last bullet:
  // a page-then-filter split makes 「捲到底沒有了」 and 「真的沒有了」 the same
  // screen).
  const loadMore = useCallback(() => {
    if (flightRef.current !== 0) return;
    const gen = genRef.current;
    flightRef.current = gen;
    setFetchingMore(true);
    api
      .listLoreEntries({
        ...optsRef.current,
        limit: PAGE,
        offset: entriesRef.current.length,
      })
      .then((page) => {
        if (gen !== genRef.current) return;
        // De-dupe by id: an entry whose state changed between two page reads
        // can slide across a page boundary and arrive twice. Keeping the first
        // copy preserves the server's order for everything else.
        setEntries((prev) => {
          const held = new Set(prev.map((e) => e.id));
          return prev.concat(page.entries.filter((e) => !held.has(e.id)));
        });
        // The line is re-answered by every page, over the WHOLE scope — it is
        // not accumulated from the rows that arrived.
        setCap({
          capChars: page.capChars,
          firstDroppedId: page.firstDroppedId,
        });
        setExhausted(page.entries.length < PAGE);
      })
      .catch((e) => {
        console.warn("LorePage: page load failed", e);
        // NOT `error`: the rows already on screen are real and stay. Only the
        // "there might be more" promise failed, so the sentinel keeps saying
        // 載入中… and another scroll retries.
      })
      .finally(() => {
        if (gen === genRef.current) setFetchingMore(false);
        if (flightRef.current === gen) flightRef.current = 0;
      });
  }, []);

  // The scroll gate. `.lore` is the scrollport (lore.css: overflow-y auto), the
  // same shape ChatArea uses — no IntersectionObserver, because jsdom has no
  // geometry to fire one from and this page's tests would then be pinning a
  // stub rather than the behaviour.
  const scrollRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    function onScroll() {
      if (!el) return;
      if (el.scrollHeight - el.scrollTop - el.clientHeight < NEAR_BOTTOM_PX) {
        loadMore();
      }
    }
    el.addEventListener("scroll", onScroll);
    return () => el.removeEventListener("scroll", onScroll);
  }, [loadMore]);

  // ── identity resolution (the SAME shape as TaskCard's `creator`) ────────
  const workerIds = useMemo(
    () => entries.map((e) => e.authorId).filter((id) => id.startsWith("ow-")),
    [entries]
  );
  const releasedCodenames = useWorkerCodenames(workerIds);
  const releasedAvatarUrls = useWorkerAvatarUrls(workerIds);

  const resolveAuthor = useCallback(
    (id: string): AuthorIdentity => {
      if (id === "") return { text: t.lore.authorUnknown, peerId: "" };
      const m = members.find((x) => x.id === id);
      if (m) return { text: m.name, peerId: id };
      if (id.startsWith("ow-")) {
        const cn =
          workers.find((x) => x.id === id)?.codename ??
          releasedCodenames.get(id);
        return { text: cn ? msg.outsourceLabel(cn) : id, peerId: id };
      }
      // 🔴 NOT ON THE ROSTER. `authorId` is pinned at write time and is never
      // re-resolved, so a writer who has since left still wrote this entry —
      // the ENTRY stays, only the writer's live affordance goes. peerId "" is
      // what makes the chip a plain span with no 傳訊息 icon (see
      // LoreAuthorChip); hiding just the icon inside a button would leave a
      // clickable pill that opens a chat with nobody.
      return { text: id, peerId: "" };
    },
    [members, workers, releasedCodenames, msg, t.lore.authorUnknown]
  );

  /** 屬於 — which scope this ONE entry rides, resolved for display.
   *
   * The two scopes are not interchangeable and the row must not blur them:
   * `agent` rides ONE member's own boot document — staff and outsource alike
   * since the scopes collapsed — and `manual` rides that task manual's
   * response. Only `manual` is CLICKABLE, and the asymmetry is deliberate: a
   * task manual has a settings page to land on, a member does not, and a pill
   * that looks clickable but goes nowhere is worse than a plain one (owner did
   * not overrule this on card rc-11734523eb52).
   *
   * 🔴 EVERY UNRESOLVED CASE FALLS BACK TO THE RAW KEY, never to a blank and
   * never to a guess. A deleted manual and a departed member both still have
   * entries riding them, and 「rc-…」 tells the reader something whereas an
   * empty cell tells them the field is broken. `unknown` is the third arm and
   * is NOT a scope, so it must never be renamed into one of the real two (see
   * LoreEntryView.scopeKind).
   *
   * 🔴 `unknown` IS A LIVE ARM ON EVERY STATION, NOT A FUTURE-PROOFING HATCH.
   * It is what the retired `role` kind maps to, and the server's migration
   * deliberately left that value on any entry whose owning member could not be
   * determined. Those rows appear on the unfiltered page — which is the page
   * this one opens on — so this arm renders in practice and its raw-key
   * fallback is what makes such an entry identifiable at all. */
  // 🔴 THE NAME ALONE DOES NOT ANSWER THE QUESTION THE OWNER ASKED. A chip
  // reading 「特助」 does not say whether that is a role or a manual, and on 全部
  // — where the page opens — every kind is mixed together. So the scope answers
  // in two parts: WHICH KIND, then WHICH ONE. Same two words the cap line opens
  // with, from the same i18n keys.
  const resolveScope = useCallback(
    (entry: LoreEntryView): {
      kindLabel: string;
      label: string;
      manualKey: string;
    } => {
      if (entry.scopeKind === "agent") {
        // scopeKey is a MEMBER id here — staff and outsource alike. Reuse the
        // author resolver so a departed member reads the same way in both
        // places.
        return {
          kindLabel: t.lore.scopeKindAgent,
          label: resolveAuthor(entry.scopeKey).text,
          manualKey: "",
        };
      }
      if (entry.scopeKind === "manual") {
        const m = manuals.find((x) => x.typeKey === entry.scopeKey);
        return {
          kindLabel: t.lore.scopeKindManual,
          label: m?.displayName || entry.scopeKey,
          manualKey: entry.scopeKey,
        };
      }
      // An unknown kind names no kind — inventing one here would state a fact
      // the server never sent.
      return {
        kindLabel: "",
        label: entry.scopeKey || t.lore.scopeUnknown,
        manualKey: "",
      };
    },
    [
      manuals,
      resolveAuthor,
      t.lore.scopeUnknown,
      t.lore.scopeKindAgent,
      t.lore.scopeKindManual,
    ]
  );

  const authorAvatar = useCallback(
    (id: string) => {
      const m = members.find((x) => x.id === id);
      if (m) return { src: m.avatarUrl, kind: avatarKindForMember(m) } as const;
      const w = workers.find((x) => x.id === id);
      if (w) return { src: w.avatarUrl, kind: "outsource" } as const;
      if (id.startsWith("ow-")) {
        return { src: releasedAvatarUrls.get(id), kind: "outsource" } as const;
      }
      return null;
    },
    [members, workers, releasedAvatarUrls]
  );

  // ── mutations. A 403 must READ as a refusal, never as nothing happening ──
  const runAction = useCallback(async (fn: () => Promise<unknown>) => {
    setActionError(null);
    try {
      await fn();
      setReloadNonce((n) => n + 1);
    } catch (e) {
      // 置頂/取消置頂 is admin-only and 失效/生效/提到最新 is author-or-admin, so
      // a 403 is a REACHABLE answer here, not a bug — and the server's own
      // sentence says which of the two rules refused. Fall back to our copy
      // only when the envelope carried none (serverMessageOf returns "").
      const reason = serverMessageOf(e);
      setActionError(
        reason || (isHttpStatus(e, 403) ? t.lore.forbidden : t.lore.actionFailed)
      );
    }
    // t.lore is read through the closure; the callback is recreated when the
    // language changes, which is exactly when the fallback copy changes too.
  }, [t.lore.forbidden, t.lore.actionFailed]);

  const setEntryState = useCallback(
    (id: string, next: LoreEntryState) =>
      void runAction(() => api.setLoreEntryState(id, next)),
    [runAction]
  );
  const bumpEntry = useCallback(
    (id: string) => void runAction(() => api.bumpLoreEntry(id)),
    [runAction]
  );

  // 撰寫人 → the office, WITH this entry's id seeded into the composer as
  // "[L-7] " — the same one-shot seed the 任務卡's 負責人 jump uses for a task
  // number (TaskCard.openChat → composeTaskNo).
  //
  // 🔴 THE SEED IS THE POINT, NOT THE JUMP. Without it the reader arrives at an
  // EMPTY composer and types "這條還適用嗎" — a sentence with no subject. The
  // author may hold dozens of entries and has to ask which one, which is the
  // question the jump was supposed to save. Showing the id on the row and
  // carrying the id across are two halves of one thing (owner, c-c933a2b41c54
  // then card rc-abf2c90d887c).
  //
  // ⚠️ The route field is named `composeTaskNo` and this passes a 傳承 id
  // through it. One field carrying two kinds of thing normally needs a second
  // field saying which — but not here: the VALUE says it. "T-1" and "L-7" are
  // distinguishable by prefix, so nothing has to be inferred. Renaming the
  // field would reach four pages and their tests, which is outside this
  // ticket's scope; it is reported rather than done (owner, c-0b690fa9c0de).
  function openChat(peerId: string, entryId: string) {
    navigateHash({ page: "office", chatId: peerId, composeTaskNo: entryId });
  }

  // 屬於 → the task-type settings hub, the SAME jump the 任務卡's 類型 chip
  // makes (TaskCard.openTypeSettings). Only a manual scope has a page to land
  // on; role and agent scopes render as plain text and never call this.
  function openManual(typeKey: string) {
    navigateHash({ page: "settings", manualKey: typeKey });
  }

  // ── grouping + where the line falls ────────────────────────────────────
  //
  // The groups are built by walking LORE_GROUPS, so the group order comes from
  // that array and from nowhere else. Order WITHIN a group is the server's and
  // is preserved exactly (filter is stable).
  const groups = useMemo(
    () =>
      LORE_GROUPS.map((state) => ({
        state,
        items: entries.filter((e) => e.state === state),
      })),
    [entries]
  );

  // Display order, flattened — this is the sequence the 上限線 cuts.
  const flat = useMemo(() => groups.flatMap((g) => g.items), [groups]);

  // 🔴 BOTH FIELDS, READ AS THEY CAME. `capChars === 0` OR an empty
  // `firstDroppedId` ⇒ no line and no dimming: either the filter did not
  // converge on one scope, or the whole scope fits inside its budget. A third
  // case is `firstDroppedId` naming an entry that has not been paged in yet —
  // `indexOf` is then -1 and, again, nothing is drawn and nothing is dimmed.
  // Drawing a line at the bottom of what happens to be loaded would put it in
  // the wrong place, and a wrong line is indistinguishable from a right one.
  const cutIndex =
    cap.capChars > 0 && cap.firstDroppedId !== ""
      ? flat.findIndex((e) => e.id === cap.firstDroppedId)
      : -1;
  const positions = useMemo(() => {
    const m = new Map<string, number>();
    flat.forEach((e, i) => m.set(e.id, i));
    return m;
  }, [flat]);

  // The line is NAMED — it says which scope's budget it is and how big that
  // budget is. No percentage, no 已用 x/y (spec §6 rules all three out).
  //
  // 🔴 WHETHER THERE IS A LINE IS THE SERVER'S ANSWER (`cap.capChars`), NOT
  // THIS. Everything here does is put a NAME on a line the server already
  // decided to draw. The condition below looks like the server's own
  // 「exactly one kind and one key」 rule and it must not be read as a second
  // copy of it: it exists so the name is empty when there is no single scope to
  // name, and if the two ever disagree the line still appears or not by the
  // server's answer alone. Re-deriving the DECISION here is the drift the whole
  // 上限線 design is built to avoid.
  //
  // 🔴 BOTH HALVES COME FROM `opts`, NOT FROM `belongs`. They have to describe
  // the request the server answered: `opts` is what was sent, and reading the
  // control's own state instead would let the name drift from the page whenever
  // the two disagree — which is exactly when a wrong name is hardest to notice.
  const soleKind = opts.scopeKinds?.length === 1 ? opts.scopeKinds[0] : "";
  const soleKey = opts.scopeKeys?.length === 1 ? opts.scopeKeys[0] : "";
  const scopeName =
    soleKind === "" || soleKey === ""
      ? ""
      : soleKind === "manual"
        ? t.lore.scopeKindManual +
          t.lore.capLineSep +
          (manuals.find((x) => x.typeKey === soleKey)?.displayName || soleKey)
        : t.lore.scopeKindAgent +
          t.lore.capLineSep +
          resolveAuthor(soleKey).text;
  const capLineText =
    scopeName + t.lore.capLineMid + cap.capChars + t.lore.capLineTail;

  // 屬於 — every MEMBER, then every TASK MANUAL, in one list.
  //
  // 🔴 THE LABELS CARRY THE KIND WORD, and that is not clutter. This one list
  // mixes people and manuals, and a bare name cannot say which: the owner
  // already hit exactly this on the row chip (「特助」 does not tell you whether
  // that is a person or a manual), and a station may legitimately have a manual
  // whose display name matches a member's. The two words are the SAME i18n keys
  // the row's 屬於 chip and the 上限線 use, so one thing is named one way
  // everywhere.
  //
  // 🔴 MEMBERS COME FIRST AND MANUALS SECOND, matching the order the owner said
  // it in (「我從使用者或是任務手冊作為 filter」) and the order the two scopes are
  // written in everywhere else in this feature.
  //
  // ⚠️ ONE ASYMMETRY WITH THE 撰寫人 LIST BELOW, on purpose: that one is an
  // ALLOW-list of kinds that can WRITE, and it excludes the warden because a
  // machine principal is refused at the route. This list is about what an entry
  // can BELONG to, which is a different question — but the answer happens to be
  // the same set, because only a member that can write can accumulate 傳承. It
  // is spelled out separately rather than shared so that the two can diverge
  // without one silently redefining the other.
  const belongsOptions = [
    ...members
      .filter((m) => m.kind === "staff")
      .map((m) => ({
        value: belongsValue("agent", m.id),
        label: t.lore.scopeKindAgent + t.lore.capLineSep + m.name,
      })),
    ...workers.map((w) => ({
      value: belongsValue("agent", w.id),
      label:
        t.lore.scopeKindAgent +
        t.lore.capLineSep +
        (w.codename ? msg.outsourceLabel(w.codename) : w.id),
    })),
    ...manuals.map((m) => ({
      value: belongsValue("manual", m.typeKey),
      label:
        t.lore.scopeKindManual +
        t.lore.capLineSep +
        (m.displayName || m.typeKey),
    })),
  ];
  // 撰寫人 options are the LIVE roster only. An author who has left cannot be
  // offered here (there is no list of departed ids to offer), which is the same
  // fact the row states by dropping their 傳訊息 icon.
  //
  // 🔴 kind === "staff" is an ALLOW-list, not a "drop the warden" deny-list, and
  // it is the same one the 辦公室 roster uses. A machine-layer member cannot be
  // an author at all: POST /api/lore requires principalAgent, classifyMember
  // sends kind "warden" to principalMachine (rank 0 < 1), so the request is a
  // 403 at the route and never reaches the handler. Offering it is offering a
  // choice whose result is ALWAYS an empty list — and an empty list reads as
  // 「這個人還沒寫過」, not as 「這個人不可能寫」. Owner hit this on the trial
  // station (card rc-11734523eb52). A deny-list would let the next machine-layer
  // kind back in silently; this cannot.
  const authorOptions = [
    ...members
      .filter((m) => m.kind === "staff")
      .map((m) => ({ value: m.id, label: m.name })),
    ...workers.map((w) => ({
      value: w.id,
      label: w.codename ? msg.outsourceLabel(w.codename) : w.id,
    })),
  ];

  return (
    <div className="lore" ref={scrollRef} data-testid="lore-page">
      {error && (
        <div className="lore__error" data-testid="lore-load-error">
          {t.lore.loadError}
        </div>
      )}
      {actionError && (
        <div className="lore__error" data-testid="lore-action-error">
          {actionError}
        </div>
      )}

      {/* 篩選列 — FilterPanel owns the ROW; the fields are ours (the panel is
          deliberately field-agnostic, FilterPanel.tsx point 4). `onClear` is
          passed ONLY while something is actually narrowing the list: the
          callback's existence IS the decision to show the button. */}
      <FilterPanel
        testId="lore-filter"
        clearLabel={t.lore.clearFilters}
        onClear={anyFilter ? clearFilters : undefined}
      >
        {/* 🔴 THE ORDER IS THE 任務頁'S ORDER, and it is not decoration (owner,
            2026-09-07: 「The order of buttons / filters matters」「make them
            consistent with task」「你完全可以抄 task」). That row reads
            負責人 → 類型 → 狀態 → 清除篩選, so this one reads
            撰寫人 → 屬於 → 狀態 → 清除篩選: WHO WROTE IT, then WHAT IT BELONGS
            TO, then WHAT STATE. A reader who has learned one page should not
            have to re-learn where things are on the other, and two pages that
            disagree teach that the position means nothing.

            The 任務頁 has a 任務編號 search box ahead of all of these; this page
            has no counterpart because a 傳承 id (L-7) is not something anyone
            goes looking for by typing it. */}
        <MultiSelectFilter
          noun={t.lore.filterAuthorNoun}
          allLabel={t.lore.filterAuthorAll}
          testId="lore-filter-author"
          options={authorOptions}
          selected={authors}
          onChange={setAuthors}
        />
        {/* 屬於 — WHICH member or WHICH manual, one control, always visible.
            It replaced the 範圍 + 角色 + 手冊 trio (owner, card rc-a43100fd0486):
            範圍's three options were the thing being collapsed away, and the two
            key lists behind it were the same question asked twice. Nothing here
            appears conditionally any more — a filter row whose fields come and
            go as you tick is a row whose shape the reader cannot learn. */}
        <MultiSelectFilter
          noun={t.lore.filterBelongsNoun}
          allLabel={t.lore.filterBelongsAll}
          testId="lore-filter-belongs"
          options={belongsOptions}
          selected={belongs}
          onChange={setBelongs}
        />
        <MultiSelectFilter
          noun={t.lore.filterStateNoun}
          allLabel={t.lore.filterStateAll}
          testId="lore-filter-state"
          options={[
            { value: "pinned", label: t.lore.statePinned },
            { value: "active", label: t.lore.stateActive },
            { value: "retired", label: t.lore.stateRetired },
          ]}
          selected={states}
          onChange={(next) => setStates(next as Set<LoreEntryState>)}
        />
      </FilterPanel>

      {!loading && !error && flat.length === 0 && (
        <div className="lore__empty" data-testid="lore-empty">
          {anyFilter ? t.lore.emptyFiltered : t.lore.empty}
        </div>
      )}

      {groups.map((group) =>
        group.items.length === 0 ? null : (
          <section
            className="lore__group"
            key={group.state}
            data-testid={`lore-group-${group.state}`}
          >
            <div className="lore__group-title">
              {group.state === "pinned"
                ? t.lore.groupPinned
                : group.state === "active"
                  ? t.lore.groupActive
                  : t.lore.groupRetired}
            </div>
            {group.items.map((entry) => {
              const pos = positions.get(entry.id) ?? -1;
              // 「線以下」 includes the entry the line sits directly above —
              // `firstDroppedId` IS the first one that does not get loaded.
              const dimmed = cutIndex >= 0 && pos >= cutIndex;
              return (
                <div key={entry.id}>
                  {cutIndex >= 0 && pos === cutIndex && (
                    <div className="lore__cap-line" data-testid="lore-cap-line">
                      <span className="lore__cap-line-text">{capLineText}</span>
                    </div>
                  )}
                  <LoreRow
                    entry={entry}
                    dimmed={dimmed}
                    author={resolveAuthor(entry.authorId)}
                    avatar={authorAvatar(entry.authorId)}
                    scope={resolveScope(entry)}
                    onOpenChat={openChat}
                    onOpenManual={openManual}
                    onSetState={setEntryState}
                    onBump={bumpEntry}
                  />
                </div>
              );
            })}
          </section>
        )
      )}

      {/* The bottom sentinel. It says 載入中… only while there IS more — an
          exhausted list ends silently rather than with a permanent spinner
          that reads as a stuck page. */}
      {!loading && !error && !exhausted && flat.length > 0 && (
        <div className="lore__more" data-testid="lore-more">
          {fetchingMore ? t.lore.loadingMore : ""}
        </div>
      )}
    </div>
  );
}


/** ONE 傳承 row. Collapsed by default; the whole row is the toggle surface. */
function LoreRow({
  entry,
  dimmed,
  author,
  avatar,
  scope,
  onOpenChat,
  onOpenManual,
  onSetState,
  onBump,
}: {
  entry: LoreEntryView;
  dimmed: boolean;
  author: AuthorIdentity;
  avatar: { src?: string; kind: "member" | "outsource" | "owner" | "assistant" } | null;
  /** 屬於, already resolved. `manualKey` non-empty is the ONLY thing that makes
   * the pill clickable — the row never re-derives that from `entry.scopeKind`,
   * so there is one place that decides it (resolveScope). */
  scope: { kindLabel: string; label: string; manualKey: string };
  onOpenChat: (peerId: string, entryId: string) => void;
  onOpenManual: (typeKey: string) => void;
  onSetState: (id: string, next: LoreEntryState) => void;
  onBump: (id: string) => void;
}) {
  const { t, msg } = useI18n();
  const [expanded, setExpanded] = useState(false);
  const [stateOpen, setStateOpen] = useState(false);
  const statusRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!stateOpen) return;
    function onDown(e: MouseEvent) {
      if (!statusRef.current?.contains(e.target as Node)) setStateOpen(false);
    }
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [stateOpen]);

  // Copied verbatim in SHAPE from TaskCard.onCardToggleClick: a click that
  // landed on an interactive descendant does that control's job and must not
  // also flip the row, and a click that ended a drag-selection is reading. The
  // [role='button'] entry also matches THIS element (the article carries it),
  // so the `hit !== e.currentTarget` guard is what keeps ordinary body clicks
  // toggling instead of being swallowed.
  function onRowToggleClick(e: React.MouseEvent<HTMLElement>) {
    const target = e.target as HTMLElement;
    const hit = target.closest(
      "button, a, textarea, input, select, [role='button'], [role='menu'], [role='dialog']"
    );
    if (hit && hit !== e.currentTarget) return;
    const sel = window.getSelection();
    if (sel && sel.rangeCount > 0 && !sel.isCollapsed) return;
    setExpanded((v) => !v);
  }
  function onRowToggleKeyDown(e: React.KeyboardEvent<HTMLElement>) {
    if (e.target !== e.currentTarget) return;
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      setExpanded((v) => !v);
    }
  }

  const stateLabel =
    entry.state === "pinned"
      ? t.lore.statePinned
      : entry.state === "active"
        ? t.lore.stateActive
        : t.lore.stateRetired;

  function actionLabel(s: LoreEntryState): string {
    return s === "retired"
      ? t.lore.actionRetire
      : s === "active"
        ? t.lore.actionActivate
        : t.lore.actionPin;
  }

  // 條目編號 chip — the 任務卡's 編號 chip, same behaviour (owner: 「傳承的 id
  // 沒有顯示出來 像 task 那樣」, c-c933a2b41c54). Click copies the id, and the
  // 已複製 flag is set ONLY when the clipboard write actually succeeded:
  // copyText returns false on a denied clipboard and a chip that lied would be
  // worse than one that did nothing. The timer is cleared on unmount so a row
  // that scrolls away mid-feedback cannot setState on a dead component.
  const [copied, setCopied] = useState(false);
  const copiedTimer = useRef<number | null>(null);
  useEffect(
    () => () => {
      if (copiedTimer.current != null) window.clearTimeout(copiedTimer.current);
    },
    []
  );
  async function copyEntryId() {
    const ok = await copyText(entry.id);
    if (!ok) return;
    setCopied(true);
    if (copiedTimer.current != null) window.clearTimeout(copiedTimer.current);
    copiedTimer.current = window.setTimeout(() => setCopied(false), 1600);
  }

  return (
    <article
      className={`lore-row lore-row--${entry.state}${
        dimmed ? " lore-row--dimmed" : ""
      }`}
      data-testid="lore-row"
      data-entry-id={entry.id}
      data-dimmed={dimmed ? "true" : "false"}
      role="button"
      tabIndex={0}
      aria-expanded={expanded}
      aria-label={expanded ? t.lore.collapseCard : t.lore.expandCard}
      onClick={onRowToggleClick}
      onKeyDown={onRowToggleKeyDown}
    >
      <div className="lore-row__head">
        {/* 🔴 THE BADGE ORDER IS 任務卡'S ORDER (owner, 2026-09-07: 「The order
            of buttons / filters matters」「make them consistent with task」).
            That card reads 編號 → 優先權 → 狀態 → 類型, so this row reads
            編號 → 狀態 → 屬於. The id comes FIRST on both because it is what you
            quote to somebody else; it used to sit second here, behind the state,
            and the two pages disagreed about where a reader's eye should land.
            傳承 has no 優先權 — there is nothing to put in that slot, and
            inventing one to fill the gap would be consistency in the shape only. */}
        {/* 條目編號, right of the status chip. It is a <button>, so the row's
            own toggle handler lets it through the same way it lets the status
            chip through — clicking the id copies it and does NOT expand the
            row. */}
        <button
          type="button"
          className="lore-row__id-badge"
          data-testid="lore-entry-id"
          aria-label={msg.loreCopyEntryId(entry.id)}
          title={msg.loreCopyEntryId(entry.id)}
          onClick={copyEntryId}
        >
          {/* 🔴 THE SAME BADGE AS 任務卡's, DOWN TO THE `#` AND THE ICON. The two
              are the same thing — 「這一筆的編號」, click to copy — and they were
              drawn two different ways: the task carried a glyph and a `#`, the
              entry carried neither. The owner spotted it by putting the two
              screenshots side by side, which is how a difference like this gets
              found: never by reading one page against its own spec.
              The icon differs (a book, not a checklist) because it names WHICH
              register the number belongs to — the same icon the 傳承 tab uses.
              What is shared is the SHAPE; what identifies is the glyph. */}
          {copied ? <CheckIcon size={13} /> : <BookIcon size={13} />}#
          {entry.id}
          {copied && (
            <span
              className="lore-row__id-badge-copied"
              role="status"
              data-testid="lore-entry-id-copied"
            >
              {t.lore.entryIdCopied}
            </span>
          )}
        </button>

        {/* ONE status badge, and it is also the control (spec §6). The menu
            hangs from the badge's LEFT edge — .lore-row__status-pop pins
            left:0 — because the badge sits at the row's left end and a
            right-anchored pop would open off the row. */}
        <div className="lore-row__status" ref={statusRef}>
          <button
            type="button"
            className={`lore-badge lore-badge--state-${entry.state} lore-row__status-chip`}
            title={t.lore.stateMenuLabel}
            aria-label={t.lore.stateMenuLabel}
            aria-haspopup="menu"
            aria-expanded={stateOpen}
            data-testid="lore-state"
            onClick={() => setStateOpen((o) => !o)}
          >
            {stateLabel}
          </button>
          {stateOpen && (
            <div
              className="lore-row__menu-pop lore-row__status-pop"
              role="menu"
              aria-label={t.lore.stateMenuLabel}
              data-testid="lore-state-options"
            >
              {STATE_ACTIONS.map((s) => (
                <button
                  key={s}
                  type="button"
                  role="menuitem"
                  // 「當前值用強調色＋700」 IS this modifier — the same one the
                  // 任務卡's 優先權/狀態 menus use (tasks.css
                  // .task-card__menu-item--active), copied into lore.css.
                  className={`lore-row__menu-item${
                    entry.state === s ? " lore-row__menu-item--active" : ""
                  }`}
                  data-testid={`lore-state-${s}`}
                  onClick={() => {
                    setStateOpen(false);
                    if (entry.state !== s) onSetState(entry.id, s);
                  }}
                >
                  {actionLabel(s)}
                </button>
              ))}
            </div>
          )}
        </div>

        {/* 屬於 — which scope pays for this entry.
            🔴 IT IS ON THE COLLAPSED ROW, WHICH IS THE WHOLE POINT. It used to
            sit in the expanded body, and the reason given for putting it on the
            row at all was that 全部 — where the page OPENS — mixes every scope
            together and the reader cannot tell a 角色傳承 from one manual's.
            That reader is looking at a COLLAPSED list. A chip they have to open
            a row to reach answers the question only for somebody who already
            went looking, which is not the person who was lost (owner, card
            rc-11734523eb52; the first attempt landed inside `expanded &&`).
            It is a <button> when it leads somewhere, so the row's closest()
            filter lets the click through to the manual instead of toggling. */}
        <span className="lore-row__scope" data-testid="lore-scope">
          {scope.kindLabel !== "" && (
            <span className="lore-row__scope-kind">
              {scope.kindLabel}
              {t.lore.capLineSep}
            </span>
          )}
          {scope.manualKey === "" ? (
            <span className="lore-row__scope-name" data-testid="lore-scope-name">
              {scope.label}
            </span>
          ) : (
            <button
              type="button"
              className="lore-row__scope-name lore-row__scope-name--link"
              data-testid="lore-scope-name"
              aria-label={msg.loreOpenManual(scope.label)}
              title={msg.loreOpenManual(scope.label)}
              onClick={() => onOpenManual(scope.manualKey)}
            >
              {scope.label}
            </button>
          )}
        </span>

        {/* A pure STATE INDICATOR: aria-hidden, no role, pointer-events:none in
            CSS, and a DIFFERENT ICON per state rather than one icon rotated. */}
        <span
          className="lore-row__expand-mark"
          data-testid="lore-expand-mark"
          aria-hidden="true"
        >
          {expanded ? <ChevronDownIcon size={18} /> : <ChevronRightIcon size={18} />}
        </span>
      </div>

      <h3 className="lore-row__title">{entry.title}</h3>

      {/* 內容預設一行截斷 — one line collapsed, in full when expanded. */}
      <div
        className={`lore-row__body${
          expanded ? "" : " lore-row__body--clamped"
        }`}
        data-testid="lore-body"
      >
        {entry.body}
      </div>

      {expanded && (
        <div className="lore-row__meta">
          {/* 撰寫人自成一列: label left, avatar+name pill right, and the
              傳訊息 icon INSIDE the pill. */}
          <span className="lore-row__meta-label">{t.lore.authorLabel}</span>
          <LoreAuthorChip
            author={author}
            avatar={avatar}
            onOpenChat={(peerId) => onOpenChat(peerId, entry.id)}
          />

          {/* …and the box to write in, directly under the person it writes to
              (owner, card rc-abf2c90d887c option ②). The 名牌 jump STAYS — it is
              how you open the whole conversation; this box is how you say one
              sentence without leaving the entry you are reading.
              🔴 Only a reachable author gets one. The departed author's pill is
              deliberately not a button, and a composer that sends to nobody
              would be that same dead affordance in a larger shape.
              It spans BOTH grid columns: it is not a value belonging to a
              label, it is its own surface. */}
          {author.peerId !== "" && (
            <LoreAuthorComposer entryId={entry.id} author={author} />
          )}

          {/* 生效期 left, 提到最新 right, ONE row. */}
          <span className="lore-row__meta-label">{t.lore.effectiveLabel}</span>
          <div className="lore-row__effective">
            <span className="lore-row__effective-ts" data-testid="lore-effective">
              {formatAbsolute(entry.effectiveTs, Date.now() / 1000)}
            </span>
            <button
              type="button"
              className="lore-row__bump"
              data-testid="lore-bump"
              onClick={() => onBump(entry.id)}
            >
              {t.lore.bump}
            </button>
          </div>

          {/* 失效理由沒填就整列不顯示 — no label, no empty value, no dash. An
              empty reason is not a reason, and a row that renders one teaches
              the reader that retirements come without explanations. */}
          {entry.retireReason !== "" && (
            <>
              <span className="lore-row__meta-label">
                {t.lore.retireReasonLabel}
              </span>
              <span
                className="lore-row__retire-reason"
                data-testid="lore-retire-reason"
              >
                {entry.retireReason}
              </span>
            </>
          )}
        </div>
      )}
    </article>
  );
}

/** The box under 撰寫人 — one sentence to the person who wrote this entry,
 * sent without leaving the page.
 *
 * 🔴 THE ENTRY ID IS PREPENDED BY THIS BOX, NOT TYPED BY THE READER, and the
 * note under the box is the ONLY place that says so. The whole reason this
 * affordance exists is that the author may hold dozens of entries and 「這條還
 * 適用嗎」 without a subject costs them a round trip to ask which one. Leaving
 * the reader to type the id would put the failure back exactly where it was,
 * and prepending it silently would send something different from what they see
 * — so it is prepended, and it is stated.
 *
 * 🔴 A FAILED SEND KEEPS THE DRAFT AND SAYS SO. A box that clears itself on a
 * failure looks identical to one that succeeded, and what is lost is the
 * reader's own sentence.
 *
 * ⚠️ NO ATTACHMENTS HERE, unlike 任務卡's composer. That box carries the whole
 * conversation with an executor; this one carries one question about one entry.
 * Anything longer belongs in the chat the 名牌 jumps to, which is still there.
 */
function LoreAuthorComposer({
  entryId,
  author,
}: {
  entryId: string;
  author: AuthorIdentity;
}): ReactNode {
  const { t } = useI18n();
  const isMobile = useIsMobile();
  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);
  const [failed, setFailed] = useState(false);
  const [sent, setSent] = useState(false);
  const boxRef = useRef<HTMLTextAreaElement>(null);
  const isComposingRef = useRef(false);
  const canSend = !sending && draft.trim().length > 0;

  // Auto-grow to the draft, same as the chat composer; the CSS max-height caps
  // it and the box scrolls beyond that.
  useLayoutEffect(() => {
    if (boxRef.current) autosizeTextarea(boxRef.current);
  }, [draft]);

  async function send() {
    if (!canSend) return;
    setSending(true);
    setSent(false);
    try {
      // The id LEADS the message, the same shape 任務卡 sends 「[T-1] …」 in.
      await api.postChat({
        to: author.peerId,
        body: `[${entryId}] ${draft.trim()}`,
      });
      setDraft("");
      setFailed(false);
      setSent(true);
    } catch (e) {
      console.warn("LorePage: message to author failed", e);
      // The typed content stays — retry-friendly, and the notice says so.
      setFailed(true);
    } finally {
      setSending(false);
    }
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    // Shared send rule (IME gate + mobile newline) — see lib/composerKeys. On a
    // phone Enter is left un-prevented so the box inserts a native newline and
    // the button is the only way to send.
    if (enterShouldSend(e, { isMobile, composing: isComposingRef.current })) {
      e.preventDefault();
      void send();
    }
  }

  return (
    <div className="lore-row__composer" data-testid="lore-author-composer">
      <div className="lore-row__composer-row">
        <textarea
          ref={boxRef}
          className="lore-row__composer-input"
          rows={1}
          value={draft}
          disabled={sending}
          placeholder={t.lore.messagePlaceholder(author.text)}
          data-testid="lore-msg-input"
          onChange={(e) => {
            setDraft(e.target.value);
            setSent(false);
          }}
          onCompositionStart={() => {
            isComposingRef.current = true;
          }}
          onCompositionEnd={(e) => {
            isComposingRef.current = false;
            setDraft(e.currentTarget.value);
          }}
          onKeyDown={onKeyDown}
        ></textarea>
        <button
          type="button"
          className="lore-row__composer-send"
          disabled={!canSend}
          data-testid="lore-msg-send"
          onClick={() => void send()}
        >
          {t.lore.messageSend}
        </button>
      </div>
      {/* 🔴 The one line that says what gets sent is not what was typed. */}
      <div className="lore-row__composer-note" data-testid="lore-msg-prefix-note">
        {t.lore.messagePrefixNote(entryId)}
      </div>
      {failed && (
        <div
          className="lore-row__composer-error"
          role="status"
          data-testid="lore-msg-failed"
        >
          {t.lore.messageFailed}
        </div>
      )}
      {sent && !failed && (
        <div
          className="lore-row__composer-sent"
          role="status"
          data-testid="lore-msg-sent"
        >
          {t.lore.messageSent}
        </div>
      )}
    </div>
  );
}

/** The 撰寫人 pill.
 *
 * 🔴 THE TWO ARMS ARE DIFFERENT ELEMENTS, not one element with a hidden icon.
 * A resolvable author gets a `<button>` carrying avatar + name + 傳訊息 icon; an
 * author who is no longer on the roster gets a plain `<span>` with the same
 * pill look and NO icon. Hiding only the glyph would leave a clickable pill
 * that opens a chat with nobody — the affordance, not just its decoration, is
 * what has to go. (Same construction as TaskCard's creator row.) */
function LoreAuthorChip({
  author,
  avatar,
  onOpenChat,
}: {
  author: AuthorIdentity;
  avatar: { src?: string; kind: "member" | "outsource" | "owner" | "assistant" } | null;
  onOpenChat: (peerId: string) => void;
}): ReactNode {
  const { t } = useI18n();
  if (author.peerId === "") {
    return (
      <span className="lore-row__chip" data-testid="lore-author-row">
        <span className="lore-row__author" data-testid="lore-author">
          {author.text}
        </span>
      </span>
    );
  }
  return (
    <button
      type="button"
      className="lore-row__chip lore-row__chip--link"
      title={t.lore.messageAuthor}
      aria-label={t.lore.messageAuthor}
      data-testid="lore-author-link"
      onClick={() => onOpenChat(author.peerId)}
    >
      {avatar ? <Avatar size={18} kind={avatar.kind} src={avatar.src} /> : null}
      <span className="lore-row__author" data-testid="lore-author">
        {author.text}
      </span>
      <ChatBubbleIcon size={13} />
    </button>
  );
}
