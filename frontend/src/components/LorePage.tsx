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
  ChatAttachmentInput,
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
  ATTACH_ACCEPT,
  STAGING_TARGET_PER_MOUNT,
  useAttachmentStaging,
} from "../hooks/useAttachmentStaging";
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
import { ComposerAttachmentPreview } from "./ComposerAttachmentPreview";
import { FilterPanel } from "./FilterPanel";
import { Markdown } from "./Markdown";
import { MultiSelectFilter } from "./MultiSelectFilter";
import {
  BookIcon,
  ChatBubbleIcon,
  CheckIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  GearIcon,
  PaperclipIcon,
  SendIcon,
  UserIcon,
} from "./icons";
import "./lore.css";
// The staged-attachment preview strip (ComposerAttachmentPreview) draws the
// `.chat__composer-preview` / `.chat__preview-*` block, which lives in
// office.css — so this page owns that import rather than free-riding on
// whichever page happened to be mounted first (frontend/.claude/rules/
// css-layout-traps.md).
import "./office.css";
// The 內容 renders through the shared <Markdown>, which wears the `.doc-md`
// document skin — declared in settings.css. This page imports it rather than
// free-riding on whichever page happened to mount first, the same convention
// DocUsage.tsx states (styleOwnership: the one time a component drew another
// sheet's block without importing it, the styles vanished the day the last
// transitive importer changed).
import "./settings.css";

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

  // ── 篩選: FOUR CONTROLS OVER THREE WIRE AXES, all query parameters, none a
  //    client-side pass ──────────────────────────────────────────────────────
  //
  // The row is 所有撰寫人 → 所有成員傳承 → 所有任務傳承 → 所有狀態 → 清除篩選
  // (owner 2026-09-08, verbatim, in that order). It is still the 任務頁's shape
  // — 負責人 → 類型 → 狀態 — with the 任務編號 box not (yet) present here.
  //
  // 🔴 THE REASON THAT BOX IS ABSENT IS NOT "NOBODY TYPES A 傳承 ID". That
  // sentence used to sit here, LORE_SPEC §6 records it as a post-hoc excuse for
  // a gap rather than a design decision, and this very branch disproved it:
  // owner asked for 傳承編號 search on 2026-09-08 (「跟 task 一樣」) and the
  // server axis for it shipped in ddf4a2f6 (`entry_ids`). The box is absent
  // because the FRONT half was not built in that round, and whether it belongs
  // in this row is an open question for owner — not a settled fact about what
  // readers want.
  //
  // 🔴 THE MEMBER CONTROL IS A ROUTE THAT DID NOT EXIST, NOT A SECOND 撰寫人.
  // The two ask different questions and, more to the point, they travel on
  // DIFFERENT WIRE AXES. 撰寫人 sends `authorIds` — 「誰寫的」 — which produces no
  // scope at all. The 上限線 needs the server to answer `capChars`, and the
  // server only answers it when the request carries EXACTLY ONE scope kind and
  // EXACTLY ONE scope key. So while members appeared on this page only under
  // 撰寫人, there was no path by which a 成員傳承 cap line could ever be asked
  // for. 所有成員傳承 sends `scope_kinds=[agent]` + `scope_keys=[<member id>]`,
  // which is that path.
  //
  // 🔴 IT SHARES BOTH SCOPE AXES WITH 所有任務傳承, and that is why the two are
  // held in ONE state set below rather than two. Ticking one of each sends two
  // kinds and two keys, the server answers 0/"" and the page draws no line —
  // the same honest answer an unfiltered page gets. The page never re-derives
  // that rule; it reads the server's answer.
  //
  // 🔴 AN EARLIER RULING SAID THE OPPOSITE AND HAS BEEN SUPERSEDED. On
  // 2026-09-07 the owner saw ONE combined 屬於 dropdown listing members and
  // manuals together and sent it back: 「成員的 filter 不是左邊那個嗎 你第二個
  // filter 應該只需要放任務」. What he rejected was one control asking two
  // questions; members were then dropped from the scope axis entirely, which is
  // what took the 成員傳承 cap line off the board. The 2026-09-08 ruling
  // restores the member axis as its OWN control — two controls, one question
  // each — and it is that ruling this code follows.
  //
  // ⚠️ NO COUNT BADGE, DELIBERATELY. The 任務頁's 狀態 pill reads 「狀態 · N」;
  // this one must not. Owner, card rc-a43100fd0486: 「we dont need count」 — and
  // the page could not honestly produce one anyway, because 傳承 loads by
  // scrolling, so any number computed here counts the rows fetched SO FAR and
  // would keep changing as the reader scrolls without anything having changed.
  //
  // `belongs` holds BOTH scope controls' ticks as `"<kind>:<key>"` values (see
  // `belongsValue`). One set, because one pair of wire axes: splitting it into
  // two states would mean re-merging them at every read and would let the two
  // halves disagree about what was sent.
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

  // ONE state set, TWO controls. Each scope control sees only the ticks whose
  // value carries its own kind, and writing back replaces only that kind's
  // ticks — the other control's are carried through untouched. Doing it this
  // way (rather than two states merged at send time) means there is exactly one
  // place that holds what was ticked, so the request and the two pills cannot
  // disagree about it.
  const ticksOfKind = useCallback(
    (kind: LoreScopeKind) =>
      new Set([...belongs].filter((v) => splitBelongs(v).kind === kind)),
    [belongs]
  );
  const setTicksOfKind = useCallback(
    (kind: LoreScopeKind, next: Set<string>) =>
      setBelongs(
        (prev) =>
          new Set([
            ...[...prev].filter((v) => splitBelongs(v).kind !== kind),
            ...next,
          ])
      ),
    []
  );

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
  // 🔴 THE NAME ALONE DOES NOT ANSWER THE QUESTION THE OWNER ASKED. A badge
  // reading 「特助」 does not say whether that is a member or a manual, and on
  // 全部 — where the page opens — every kind is mixed together. So the scope
  // carries TWO things: WHICH KIND, and WHICH ONE.
  //
  // 🔴 THE KIND IS NOW A VALUE, NOT A LABEL, AND THAT IS THE WHOLE CHANGE.
  // It used to be the WORD 「成員傳承 · 」 / 「任務傳承 · 」 printed to the left
  // of the badge. Owner removed that prefix on 2026-09-08 with a screenshot of
  // it circled: 「這個不必要」. So the kind is returned as the raw discriminator
  // and the row spends it on an ICON instead (person vs gear) — one badge, and
  // the glyph says which kind it is.
  //
  // ⚠️ WHICH MEANS THE ICON IS NOW THE ONLY DISCRIMINATOR ON A CLOSED ROW.
  // With the word gone, a member's entry and a manual's entry whose names
  // happen to match are the same pixels except for that glyph. Dropping the
  // icon "because the badge already carries the name" undoes the very thing the
  // 屬於 badge was added for (card rc-11734523eb52).
  //
  // `kind` is "" for the unknown arm: an entry the migration could not place
  // names no kind and gets no glyph, because inventing one would state a fact
  // the server never sent.
  const resolveScope = useCallback(
    (entry: LoreEntryView): {
      kind: "" | LoreScopeKind;
      label: string;
      manualKey: string;
    } => {
      if (entry.scopeKind === "agent") {
        // scopeKey is a MEMBER id here — staff and outsource alike. Reuse the
        // author resolver so a departed member reads the same way in both
        // places.
        return {
          kind: "agent",
          label: resolveAuthor(entry.scopeKey).text,
          manualKey: "",
        };
      }
      if (entry.scopeKind === "manual") {
        const m = manuals.find((x) => x.typeKey === entry.scopeKey);
        return {
          kind: "manual",
          label: m?.displayName || entry.scopeKey,
          manualKey: entry.scopeKey,
        };
      }
      // An unknown kind names no kind — inventing one here would state a fact
      // the server never sent.
      return {
        kind: "",
        label: entry.scopeKey || t.lore.scopeUnknown,
        manualKey: "",
      };
    },
    [manuals, resolveAuthor, t.lore.scopeUnknown]
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

  // 任務傳承 — every TASK MANUAL, and ONLY manuals.
  //
  // 🔴 THE KIND PREFIX IS IN THE VALUE, and it is what puts
  // `scope_kinds=["manual"]` on the request beside `scope_keys`, so the server
  // narrows on BOTH axes. It is also what lets ONE state set back TWO controls:
  // the prefix is how each control finds its own ticks again (see
  // `ticksOfKind`).
  const belongsOptions = manuals.map((m) => ({
    value: belongsValue("manual", m.typeKey),
    label: m.displayName || m.typeKey,
  }));
  // 🔴 THE LIVE ROSTER, BUILT ONCE AND READ BY TWO CONTROLS. 撰寫人 and
  // 成員傳承 offer the SAME people — they differ in which wire axis the tick
  // rides, not in who is on the list — so the list is derived once here. Two
  // separately-built lists would be two lists that can drift, and a name that
  // is offered as a 撰寫人 but not as a 成員 (or the reverse) is a gap nobody
  // would ever see reported.
  //
  // Options are the LIVE roster only. An author who has left cannot be offered
  // here (there is no list of departed ids to offer), which is the same fact the
  // row states by dropping their 傳訊息 icon.
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
  const roster = [
    ...members
      .filter((m) => m.kind === "staff")
      .map((m) => ({ id: m.id, label: m.name })),
    ...workers.map((w) => ({
      id: w.id,
      label: w.codename ? msg.outsourceLabel(w.codename) : w.id,
    })),
  ];
  const authorOptions = roster.map((r) => ({ value: r.id, label: r.label }));
  // 成員傳承 — the SAME people, carried on the SCOPE axis instead. The value is
  // prefixed `agent:` so the request says `scope_kinds=["agent"]` +
  // `scope_keys=[<member id>]`, which is the only shape the server will answer
  // a member's `capChars` for.
  const memberScopeOptions = roster.map((r) => ({
    value: belongsValue("agent", r.id),
    label: r.label,
  }));

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
        {/* 🔴 THE ORDER IS THE OWNER'S, VERBATIM (2026-09-08):
            所有撰寫人 → 所有成員傳承 → 所有任務傳承 → 所有狀態 → 清除篩選.
            WHO WROTE IT, then WHICH MEMBER it belongs to, then WHICH MANUAL,
            then WHAT STATE. It is still the 任務頁's order (負責人 → 類型 →
            狀態) with the scope half split in two, because a reader who has
            learned one page should not have to re-learn where things are on the
            other.

            The 任務頁 has a 任務編號 search box ahead of all of these; this
            page has no counterpart YET. The server side of it exists
            (`entry_ids`, ddf4a2f6) — owner asked for it on 2026-09-08 — but
            nothing on this page sends it. Do not re-write this into 「傳承的編號
            不是誰會去打的東西」: that claim is what LORE_SPEC §6 records as a
            justification written to fit the gap, and owner asked for the box.

            Nothing here appears conditionally — a filter row whose fields come
            and go as you tick is a row whose shape the reader cannot learn. */}
        <MultiSelectFilter
          noun={t.lore.filterAuthorNoun}
          allLabel={t.lore.filterAuthorAll}
          testId="lore-filter-author"
          options={authorOptions}
          selected={authors}
          onChange={setAuthors}
        />
        {/* 成員傳承 — WHICH member's lore, on the SCOPE axis.
            🔴 THIS IS NOT THE SAME QUESTION AS 撰寫人 EVEN THOUGH THE NAMES ARE
            THE SAME PEOPLE. 撰寫人 asks who WROTE the entry and sends
            `authorIds`; this asks whose lore the entry IS and sends
            `scope_kinds=[agent]` + `scope_keys=[<member id>]`. Only the second
            shape can make the server answer `capChars`, which is why this
            control exists at all — see the axes comment at the top of the
            component. */}
        <MultiSelectFilter
          noun={t.lore.filterMemberNoun}
          allLabel={t.lore.filterMemberAll}
          testId="lore-filter-member"
          options={memberScopeOptions}
          selected={ticksOfKind("agent")}
          onChange={(next) => setTicksOfKind("agent", next)}
        />
        {/* 任務傳承 — WHICH task manual. Same wire axes as the control above it;
            ticking one in each sends two kinds and two keys, and the server
            answers no cap line. That is stated in the axes comment rather than
            prevented here: the page does not re-implement the server's rule. */}
        <MultiSelectFilter
          noun={t.lore.filterTaskNoun}
          allLabel={t.lore.filterTaskAll}
          testId="lore-filter-belongs"
          options={belongsOptions}
          selected={ticksOfKind("manual")}
          onChange={(next) => setTicksOfKind("manual", next)}
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
   * so there is one place that decides it (resolveScope). `kind` is what picks
   * the glyph, and it comes from the same one place. */
  scope: { kind: "" | LoreScopeKind; label: string; manualKey: string };
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
    // `.lore-row__composer` is in this list for the same reason the controls
    // are: it is a WRITING surface that now renders on the COLLAPSED row, so a
    // click that lands on its padding (beside the box, not on it) would fold
    // the entry away mid-sentence. The textarea/buttons inside it are already
    // covered by the element entries; this covers the box AROUND them.
    const hit = target.closest(
      "button, a, textarea, input, select, .lore-row__composer, [role='button'], [role='menu'], [role='dialog']"
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
          {/* 🔴 THE TEXT PREFIX 「成員傳承 · 」/「任務傳承 · 」 IS GONE (owner
              2026-09-08, screenshot with it circled: 「這個不必要」). What is
              left is ONE badge, and the GLYPH carries the kind:
                · 任務傳承 → 齒輪, and the badge is a <button> that opens the
                  manual's settings page — unchanged.
                · 成員傳承 → the person glyph, the same UserIcon the 任務卡's
                  負責人 chip falls back to through <Avatar>. Not a button: a
                  member has no page to land on, and a pill that looks clickable
                  and goes nowhere is the dead affordance LoreAuthorChip exists
                  to avoid.
                · unknown → no glyph at all. There is no kind to name.
              🔴 EXACTLY ONE ARM RUNS, BECAUSE `scope.kind` IS ONE VALUE. The
              owner's rule 「一次只可能會出現一個」 is not enforced by a check
              here; it is a property of the data — resolveScope returns a single
              discriminator — and this chain is written as one if/else over it so
              there is no arrangement of props that could show both.
              The badge itself is the 任務卡's 類型 badge, copied value-for-value
              under lore names (.lore-badge--type ← tasks.css .task-badge--type;
              owner: 「傳承條目的樣子跟任務卡不一樣，我要它一樣」). */}
          {scope.kind === "manual" ? (
            <button
              type="button"
              className="lore-badge lore-badge--type lore-row__scope-chip"
              data-testid="lore-scope-name"
              data-scope-kind="manual"
              aria-label={msg.loreOpenManual(scope.label)}
              title={msg.loreOpenManual(scope.label)}
              onClick={() => onOpenManual(scope.manualKey)}
            >
              <GearIcon
                size={13}
                className="lore-row__scope-glyph lore-row__scope-glyph--task"
              />
              <span className="lore-row__scope-name">{scope.label}</span>
            </button>
          ) : (
            <span
              className="lore-badge lore-badge--type"
              data-testid="lore-scope-name"
              data-scope-kind={scope.kind}
            >
              {scope.kind === "agent" && (
                <UserIcon
                  size={13}
                  className="lore-row__scope-glyph lore-row__scope-glyph--member"
                />
              )}
              <span className="lore-row__scope-name">{scope.label}</span>
            </span>
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

      {/* 🔴 內容 IS THE ONE THING 展開／收合 STILL CONTROLS (owner 2026-09-08,
          card rc-2e10ee03b97c, superseding his own ruling 90 seconds earlier:
          「我覺得好像應該唯一要折疊的是 content 然後要支援 md format」). So the
          clamp stays HERE, and everything below — 撰寫人 / 生效期 / 失效理由 —
          moved OUT of `expanded` and now shows on a closed row too. The earlier
          reading (「才五百字好像沒什麼好折疊的」 ⇒ body always full, meta still
          hidden) was the exact opposite arrangement and is dead.

          🔴 THE TRUNCATION IS OF THE RENDERED OUTPUT, NEVER OF THE SOURCE. It
          is CSS (-webkit-line-clamp on .lore-row__body--clamped): the markdown
          is parsed in full and the BOX shows one line of it. Slicing the source
          string to N characters would cut a `**` or a fence in half and hand
          the parser something that is not the entry's markdown at all.

          🔴 MARKDOWN GOES THROUGH THE SHARED, XSS-SAFE RENDERER, exactly as the
          chat bubble does (ChatArea.tsx:2052). Never dangerouslySetInnerHTML:
          a 傳承 body is agent-authored free text.
          `breaks` for the same reason chat turns it on — this text was WRITTEN
          as plain text, where Enter meant "new line", and standard markdown's
          soft-wrap would silently join every such line into one run-on.

          ⚠️ WHAT THIS COSTS THE ENTRIES THAT ALREADY EXIST, stated rather than
          papered over: every stored body was written under a plain-text
          contract, so characters that are now syntax are now read as syntax.
          Chat had the identical problem with the identical corpus and did NOT
          escape the text (it renders `m.body` raw through the same component),
          and this follows chat — one behaviour for one kind of text, rather
          than a second, page-local escaping rule that would make the same
          sentence render two ways on two pages. What limits the blast radius is
          the renderer's own inline set: only `**bold**`, `` `code` `` and
          links. `_` is NOT inline syntax there, so a body carrying
          `note_size_chars` is unaffected — the case that prompted the worry. */}
      <div
        className={`lore-row__body${
          expanded ? "" : " lore-row__body--clamped"
        }`}
        data-testid="lore-body"
      >
        <Markdown source={entry.body} className="lore-row__body-md doc-md" breaks />
      </div>

      {/* 🔴 ALWAYS RENDERED — NOT `expanded &&` ANY MORE (owner 2026-09-08,
          card rc-2e10ee03b97c). 展開／收合 now governs the CONTENT's length and
          nothing else, so 撰寫人 / 生效期 / 失效理由 are on the closed row too.
          Re-wrapping this in `expanded` would put the page back to hiding the
          three rows the owner asked to see. */}
      <div className="lore-row__meta">
        {/* 撰寫人自成一列: label left, avatar+name pill right, and the
            傳訊息 icon INSIDE the pill. */}
        <span className="lore-row__meta-label">{t.lore.authorLabel}</span>
        <LoreAuthorChip
          author={author}
          avatar={avatar}
          onOpenChat={(peerId) => onOpenChat(peerId, entry.id)}
        />

        {/* 生效期 directly UNDER 撰寫人 (owner: 「生效期移到撰寫人下面一列」) —
            with the composer lifted out of this grid, the two label rows are
            now adjacent instead of being split by a writing surface. */}
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

      {/* The box to write to this entry's author — LAST ON THE CARD, in BOTH
          states (owner 2026-09-08, screenshot: 「訊息匡應該在最下方」). It sat
          between 內容 and 撰寫人／生效期, which put a writing surface in the
          middle of the things you read.
          🔴 IT IS OUTSIDE `expanded` AND MUST STAY OUTSIDE IT, the way 任務卡's
          message box is always on the card (owner: 「訊息輸入框預設就要在」). A
          reader who wants to ask one question about a row they can already read
          should not have to open the row first — a step the 任務 page does not
          ask for. Putting it after the expanded block is what satisfies BOTH
          rulings at once: collapsed, the block above renders nothing and this is
          the last thing on the card anyway; expanded, it is below 撰寫人 /
          生效期 / 失效理由. Moving it back INSIDE `expanded` to "put it at the
          bottom" would trade one owner ruling for the other.
          🔴 Only a reachable author gets one. The departed author's pill is
          deliberately not a button, and a composer that sends to nobody would be
          that same dead affordance in a larger shape.
          It is not a cell of `.lore-row__meta`'s grid, so lore.css gives it the
          full row width itself rather than a `grid-column` span. */}
      {author.peerId !== "" && (
        <LoreAuthorComposer entryId={entry.id} author={author} />
      )}
    </article>
  );
}

/** The box under 撰寫人 — one sentence to the person who wrote this entry,
 * sent without leaving the page.
 *
 * 🔴 THE ENTRY ID IS PREPENDED BY THIS BOX, NOT TYPED BY THE READER. The whole
 * reason this affordance exists is that the author may hold dozens of entries
 * and 「這條還適用嗎」 without a subject costs them a round trip to ask which
 * one. Leaving the reader to type the id would put the failure back exactly
 * where it was.
 *
 * ⚠️ NOTHING ON SCREEN SAYS SO ANY MORE. There used to be a line under the box
 * (`.lore-row__composer-note`) — the only place the reader learned that what
 * goes out is not exactly what they typed — and owner removed it in the round
 * that made this row look like a 任務卡 (任務卡 has no such line). The PREFIX
 * ITSELF WAS DELIBERATELY LEFT ALONE: that round was 外觀 only. So the silent
 * rewrite the note existed to disclose is now silent, and that is a known,
 * chosen cost, not an oversight to quietly re-fix here.
 *
 * 🔴 A FAILED SEND KEEPS THE DRAFT AND SAYS SO. A box that clears itself on a
 * failure looks identical to one that succeeded, and what is lost is the
 * reader's own sentence.
 *
 * ATTACHMENTS RIDE THE SAME MESSAGE, through the SAME staging machine the 任務卡
 * composer and the chat composer use (useAttachmentStaging): paste an image into
 * the box or pick files with the 📎, and they go out on the one postChat call
 * this box makes. The slot is per-mount, so one entry's staged files can never
 * surface under another's box.
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
  const fileInputRef = useRef<HTMLInputElement>(null);
  const isComposingRef = useRef(false);
  // The SHARED staging machine (same caps, same paste/pick funnels as the chat
  // composer / 任務卡). `STAGING_TARGET_PER_MOUNT` is the honest slot for this
  // surface: a LoreRow is mounted per entry, so the files die with the entry's
  // box and one row's staging cannot appear under another row's.
  const {
    pendingAttachments,
    attachError,
    onPaste,
    onPickFile,
    removeAttachment,
    clearAttachments,
  } = useAttachmentStaging(STAGING_TARGET_PER_MOUNT);
  // 🔴 TEXT **OR** FILES — the same rule 任務卡 uses. A message that is only a
  // screenshot is a legitimate message; requiring a sentence beside it would
  // make the 📎 an affordance that cannot complete on its own.
  const canSend =
    !sending && (draft.trim().length > 0 || pendingAttachments.length > 0);

  // Auto-grow to the draft, same as the chat composer; the CSS max-height caps
  // it and the box scrolls beyond that.
  useLayoutEffect(() => {
    if (boxRef.current) autosizeTextarea(boxRef.current);
  }, [draft]);

  async function send() {
    if (!canSend) return;
    const attachments: ChatAttachmentInput[] = pendingAttachments.map((a) => ({
      dataB64: a.dataUri,
      ...(a.filename ? { filename: a.filename } : {}),
      mime: a.mime,
    }));
    setSending(true);
    setSent(false);
    try {
      // The id LEADS the message, the same shape 任務卡 sends 「[T-1] …」 in.
      await api.postChat({
        to: author.peerId,
        body: `[${entryId}] ${draft.trim()}`,
        ...(attachments.length > 0 ? { attachments } : {}),
      });
      setDraft("");
      clearAttachments();
      setFailed(false);
      setSent(true);
    } catch (e) {
      console.warn("LorePage: message to author failed", e);
      // The typed content stays — retry-friendly, and the notice says so. The
      // STAGED FILES stay for the same reason: clearing them on a failure would
      // silently cost the reader the thing they cannot retype.
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
      {/* Staged-attachment preview strip — the SAME component (and therefore the
       * same markup and classes) the chat composer / ReplyComposer / 任務卡 use.
       * The mount guard lives here, as it does at every other call site. */}
      {(pendingAttachments.length > 0 || attachError) && (
        <ComposerAttachmentPreview
          pendingAttachments={pendingAttachments}
          attachError={attachError}
          onRemove={removeAttachment}
        />
      )}
      <div className="lore-row__composer-row">
        <input
          ref={fileInputRef}
          type="file"
          accept={ATTACH_ACCEPT}
          multiple
          onChange={onPickFile}
          hidden
        />
        <button
          type="button"
          className="lore-row__composer-attach"
          aria-label={t.chat.attachLabel}
          title={t.chat.attachLabel}
          disabled={sending}
          data-testid="lore-msg-attach"
          onClick={() => fileInputRef.current?.click()}
        >
          <PaperclipIcon size={18} />
        </button>
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
          onPaste={onPaste}
        ></textarea>
        <button
          type="button"
          className="lore-row__composer-send"
          disabled={!canSend}
          data-testid="lore-msg-send"
          onClick={() => void send()}
        >
          <SendIcon size={14} />
          {t.lore.messageSend}
        </button>
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
