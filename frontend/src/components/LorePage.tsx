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
//   · the scope on the row itself — the 任務 dropdown says which scope is on
//     screen, so stamping it on every row would repeat the filter N times.

import {
  useCallback,
  useEffect,
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
import { useRoles } from "../hooks/useRoles";
import { useTaskManuals } from "../hooks/useTaskManuals";
import {
  useWorkerAvatarUrls,
  useWorkerCodenames,
} from "../hooks/useWorkerCodenames";
import { avatarKindForMember } from "../lib/avatarKind";
import { formatAbsolute } from "../lib/dateFormat";
import { navigateHash } from "../lib/hashRoute";
import { Avatar } from "./Avatar";
import { FilterPanel } from "./FilterPanel";
import { ChatBubbleIcon, ChevronDownIcon, ChevronRightIcon } from "./icons";
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

export function LorePage() {
  const { t, msg } = useI18n();
  const { members } = useMembers();
  const { workers } = useOutsourceWorkers();
  const { roles } = useRoles();
  const { manuals } = useTaskManuals();

  // ── 篩選 (all four axes are QUERY PARAMETERS, none is a client-side pass) ──
  //
  // `scope` is one control carrying the spec's 任務下拉: "" = 全部,
  // "role" = the explicit 「無 · 角色傳承」 item, "manual:<typeKey>" = one task
  // type. The item the spec names by hand exists because 「這一筆不屬於任何任務」
  // is a real answer about a real entry, and an empty dropdown slot does not
  // say it.
  //
  // `roleKey` is the SECOND half of a role scope. It exists because a budget
  // belongs to ONE scope, and `scopeKind: "role"` alone names a kind, not a
  // scope — the server answers `capChars: 0` for it and the page correctly
  // draws no line. Picking a role is what lets the 角色傳承 line appear at all,
  // which is most of what this page is for. It is hidden while a TASK is
  // selected, because it cannot narrow a manual scope.
  const [scope, setScope] = useState("");
  const [roleKey, setRoleKey] = useState("");
  const [stateFilter, setStateFilter] = useState<"" | LoreEntryState>("");
  const [authorFilter, setAuthorFilter] = useState("");

  const opts = useMemo<LoreListOptions>(() => {
    const next: LoreListOptions = {};
    if (scope === "role") {
      next.scopeKind = "role";
      if (roleKey) next.scopeKey = roleKey;
    } else if (scope.startsWith("manual:")) {
      next.scopeKind = "manual";
      next.scopeKey = scope.slice("manual:".length);
    }
    if (stateFilter) next.state = stateFilter;
    if (authorFilter) next.authorId = authorFilter;
    return next;
  }, [scope, roleKey, stateFilter, authorFilter]);

  const anyFilter =
    scope !== "" || roleKey !== "" || stateFilter !== "" || authorFilter !== "";

  function clearFilters() {
    setScope("");
    setRoleKey("");
    setStateFilter("");
    setAuthorFilter("");
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

  function openChat(peerId: string) {
    navigateHash({ page: "office", chatId: peerId });
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
  const scopeName =
    scope === "role"
      ? t.lore.capLineRolePrefix +
        t.lore.capLineSep +
        (roles.find((r) => r.key === roleKey)?.name || roleKey)
      : t.lore.capLineManualPrefix +
        t.lore.capLineSep +
        (() => {
          const key = scope.slice("manual:".length);
          const m = manuals.find((x) => x.typeKey === key);
          return m?.displayName || key;
        })();
  const capLineText =
    scopeName + t.lore.capLineMid + cap.capChars + t.lore.capLineTail;

  const roleOptions = roles.map((r) => ({ value: r.key, label: r.name || r.key }));
  const manualOptions = manuals.map((m) => ({
    value: `manual:${m.typeKey}`,
    label: m.displayName || m.typeKey,
  }));
  // 撰寫人 options are the LIVE roster only. An author who has left cannot be
  // offered here (there is no list of departed ids to offer), which is the same
  // fact the row states by dropping their 傳訊息 icon.
  const authorOptions = [
    ...members.map((m) => ({ value: m.id, label: m.name })),
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
        <LoreSelect
          testId="lore-filter-scope"
          label={t.lore.filterTaskNoun}
          value={scope}
          onChange={(v) => {
            setScope(v);
            // A task scope cannot be narrowed by role, so the role half is
            // dropped rather than kept as invisible state that would ride the
            // next request.
            if (v !== "role") setRoleKey("");
          }}
          options={[
            { value: "", label: t.lore.filterTaskAll },
            // 🔴 SPEC §6 NAMES THIS ITEM. 「無 · 角色傳承」 is not "no filter" —
            // it is the answer 「這一筆不屬於任何任務」, and it is a filter value
            // of its own (scopeKind: "role").
            { value: "role", label: t.lore.filterRoleLore },
            ...manualOptions,
          ]}
        />
        {scope === "role" && (
          <LoreSelect
            testId="lore-filter-role"
            label={t.lore.filterRoleNoun}
            value={roleKey}
            onChange={setRoleKey}
            options={[
              { value: "", label: t.lore.filterRoleAll },
              ...roleOptions,
            ]}
          />
        )}
        <LoreSelect
          testId="lore-filter-state"
          label={t.lore.filterStateNoun}
          value={stateFilter}
          onChange={(v) => setStateFilter(v as "" | LoreEntryState)}
          options={[
            { value: "", label: t.lore.filterStateAll },
            { value: "pinned", label: t.lore.statePinned },
            { value: "active", label: t.lore.stateActive },
            { value: "retired", label: t.lore.stateRetired },
          ]}
        />
        <LoreSelect
          testId="lore-filter-author"
          label={t.lore.filterAuthorNoun}
          value={authorFilter}
          onChange={setAuthorFilter}
          options={[
            { value: "", label: t.lore.filterAuthorAll },
            ...authorOptions,
          ]}
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
                    onOpenChat={openChat}
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

/** One filter field: a label + a native single-select pill.
 *
 * A NATIVE `<select>`, not the 任務頁's MultiSelectFilter, and the reason is
 * arity: `LoreListOptions` takes ONE `state` / ONE `authorId` / ONE scope pair,
 * so a multi-select control would be an interface that can express a request
 * this API cannot carry — and the page would have to silently drop the extra
 * ticks or fall back to filtering downloaded rows, which §6 forbids. */
function LoreSelect({
  testId,
  label,
  value,
  onChange,
  options,
}: {
  testId: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: { value: string; label: string }[];
}) {
  return (
    <label className="lore__filter-field">
      <span className="lore__filter-label">{label}</span>
      <select
        className="lore__filter"
        data-testid={testId}
        aria-label={label}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      >
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </label>
  );
}

/** ONE 傳承 row. Collapsed by default; the whole row is the toggle surface. */
function LoreRow({
  entry,
  dimmed,
  author,
  avatar,
  onOpenChat,
  onSetState,
  onBump,
}: {
  entry: LoreEntryView;
  dimmed: boolean;
  author: AuthorIdentity;
  avatar: { src?: string; kind: "member" | "outsource" | "owner" | "assistant" } | null;
  onOpenChat: (peerId: string) => void;
  onSetState: (id: string, next: LoreEntryState) => void;
  onBump: (id: string) => void;
}) {
  const { t } = useI18n();
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
            onOpenChat={onOpenChat}
          />

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
