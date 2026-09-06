import { useState, useEffect, useRef } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import type { LoreActivityView } from "../types";
import { useHashRoute } from "../lib/hashRoute";
import { BookIcon, ChevronDownIcon, ChevronRightIcon } from "./icons";
// 🔴 This component draws the `.mp-recall__*` block out of member-detail.css, so
// it OWNS that import (styleOwnership.test.ts). The panel that renders it
// imports the sheet too, but relying on that is exactly the free-riding this
// guard exists for — the day something else mounts this card it would render
// completely unstyled and nothing would go red.
import "./member-detail.css";

// LoreActivityCard — 傳承活動: what ONE member has read out of lore SINCE IT CAME
// ONLINE THIS TIME, under the RESUME SUMMARY card in the member detail panel.
//
// 🔴 THE QUESTION THIS ANSWERS IS THE OWNER'S OWN: 「agent 到底有沒有用到我們寫
// 的記憶？」 Until GET /api/members/{id}/lore-activity landed, the recall journal
// had three writers and no reader at all, which from this side of the screen is
// indistinguishable from a journal that was never written.
//
// ── THE FOUR ABSENCES, AND WHY THEY MAY NEVER SHARE A SENTENCE ───────────────
// Everything below renders as "nothing to show" if anybody gets lazy, and only
// ONE of them means the agent ignored the memory:
//
//   1. LOADING            — we have not asked yet, or the answer is in flight.
//   2. ERROR              — we asked and the read failed. NOT an empty list:
//                           「載入失敗」 and 「沒有取用紀錄」 are opposite claims
//                           and the second one is a statement about the agent.
//   3. NOT RUNNING        — `sessionActive: false`. There is no 這一任 to have a
//                           record in. Saying 「沒有取用紀錄」 here would blame a
//                           stopped agent for not reading while it was asleep.
//   4. RUNNING AND QUIET  — `sessionActive: true`, `rows: []`. THIS is the one
//                           that really says 「這一任還沒去翻過任何東西」.
//
// 🔴 EACH GETS ITS OWN STRING. That is the whole content of this component's
// honesty contract, and the reason the payload carries `session_active` as a
// field instead of leaving the screen to infer it from an empty array.
//
// ── 「每次重新上線就清空」 IS A FILTER, NOT A DELETE ──────────────────────────
// The owner asked for a panel that clears at every wake. Nothing is deleted:
// the journal is append-only by design (migrations/00082), and the server scopes
// this read to rows stamped with the member's CURRENT session anchor. What looks
// like clearing is the anchor moving.

/** Render 「上線後 N 分鐘」 / 「上線後 N 秒」.
 *
 * 🔴 THE NUMBER IS THE SERVER'S, THE UNIT IS OURS. `sinceBootSecs` arrives
 * already computed from the anchor STAMPED ON THE JOURNAL ROW — this function
 * must never re-derive it from `createdTs` minus the member's current anchor,
 * which would answer about the wrong session the moment the member reboots and
 * would look entirely plausible while doing it.
 *
 * Under a minute reads in seconds: the owner's format is 「上線後 10 分鐘」 and
 * rounding a 45-second read down to 「0 分鐘」 would print a number that says
 * nothing happened yet. The SKELETON of his format is unchanged — only the
 * unit moves. */
function sinceBoot(
  secs: number,
  t: ReturnType<typeof useI18n>["t"],
): string {
  // Negative is impossible from a well-formed row (a read cannot precede its
  // own session's boot), so it is not smoothed to 0 — it is shown as seconds,
  // where a reader can see something is wrong instead of reading a tidy zero.
  if (secs < 60) return t.mp.loreActivity.afterBootSecs(Math.round(secs));
  return t.mp.loreActivity.afterBootMins(Math.floor(secs / 60));
}

export function LoreActivityCard({
  agentId,
  loreEnabled,
}: {
  agentId: string;
  /** The station-wide lore switch, as the panel read it from
   * `GET /api/lore-switch`.
   *
   * 🔴 IT GATES THE LINK, NOT THE CARD. The journal keeps saying what happened
   * while the feature was on, so the history stays readable after somebody
   * switches lore off — but with the feature off there is no 傳承 tab to land
   * on (App.tsx drops a `#lore` route through to the office page), so a link
   * would take its clicker somewhere unrelated and say nothing. The honest
   * handling of a link that cannot arrive is not to offer it.
   *
   * ⚠️ THIS IS A SNAPSHOT AND THE RACE IS KNOWN AND ACCEPTED. The value is
   * whatever the switch said when the panel read it; the switch can be flipped
   * while the owner is still looking at this card (that is the entire reason
   * `/api/lore-switch` exists as a live route — a boot context's copy of the
   * same flag is a snapshot too). A link drawn a moment before the switch went
   * off stays on screen, and at worst lands on the office page. We do NOT poll
   * to close that window: a poll would spend a request every few seconds on
   * every open panel to tighten a failure whose whole cost is one wrong tab. */
  loreEnabled: boolean;
}) {
  const { t } = useI18n();
  const [, setRoute] = useHashRoute();
  const [show, setShow] = useState(false);
  const [state, setState] = useState<{
    data: LoreActivityView | null;
    loading: boolean;
    error: boolean;
  }>({ data: null, loading: false, error: false });
  const loadedKeyRef = useRef<string | null>(null);
  const inFlightKeyRef = useRef<string | null>(null);
  // Read through a ref for the same reason ResumeSummaryCard does (T-7526): an
  // inline arrow in the effect deps is rebuilt every render and would tear the
  // read down mid-flight on any repaint.
  const fetchRef = useRef<() => Promise<LoreActivityView>>(() =>
    api.getMemberLoreActivity(agentId),
  );
  fetchRef.current = () => api.getMemberLoreActivity(agentId);

  function runFetch(key: string) {
    inFlightKeyRef.current = key;
    setState({ data: null, loading: true, error: false });
    fetchRef
      .current()
      .then((data) => {
        if (inFlightKeyRef.current !== key) return;
        inFlightKeyRef.current = null;
        loadedKeyRef.current = key; // stamped on ARRIVAL only
        setState({ data, loading: false, error: false });
      })
      .catch(() => {
        if (inFlightKeyRef.current !== key) return;
        inFlightKeyRef.current = null;
        // No stamp: a failed read must be retryable by re-expanding.
        setState({ data: null, loading: false, error: true });
      });
  }

  // 🔴 NOTHING IS FETCHED UNTIL THE FIRST EXPAND. The panel's default load must
  // not grow a request per member just because this card exists.
  useEffect(() => {
    if (!show) return;
    if (loadedKeyRef.current === agentId) return;
    if (inFlightKeyRef.current === agentId) return;
    runFetch(agentId);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [show, agentId]);

  return (
    <div className="mp-card mp-expand" data-testid="mp-lore-activity">
      <button
        type="button"
        className="mp-expand__head"
        aria-expanded={show}
        onClick={() => setShow((v) => !v)}
        data-testid="mp-lore-toggle"
      >
        <BookIcon size={15} className="mp-expand__icon" />
        <span className="mp-expand__title">{t.mp.loreActivity.title}</span>
        {show ? (
          <ChevronDownIcon size={16} className="mp-expand__chevron" />
        ) : (
          <ChevronRightIcon size={16} className="mp-expand__chevron" />
        )}
      </button>
      {show && (
        <div className="mp-expand__body" data-testid="mp-lore-body">
          {/* `!data && !error` covers the render tick between the click and the
            * effect's own setState — LOADING, never a fabricated empty. */}
          {state.loading || (!state.data && !state.error) ? (
            <span data-testid="mp-lore-loading">
              {t.mp.loreActivity.loading}
            </span>
          ) : state.error ? (
            // ABSENCE 2. Never the empty-list sentence: a failed read says
            // nothing at all about what the agent did or did not read.
            <div data-testid="mp-lore-error">
              <span>{t.mp.loreActivity.error}</span>{" "}
              <button
                type="button"
                className="doc-btn"
                data-testid="mp-lore-retry"
                onClick={() => runFetch(agentId)}
              >
                {t.mp.loreActivity.retry}
              </button>
            </div>
          ) : state.data && !state.data.sessionActive ? (
            // ABSENCE 3 — no session to have a record in.
            <div className="mp-recall__note" data-testid="mp-lore-nosession">
              {t.mp.loreActivity.noSession}
            </div>
          ) : state.data && state.data.rows.length === 0 ? (
            // ABSENCE 4 — running, and has looked nothing up. The only one of
            // the four that is a statement about the agent.
            <div className="mp-recall__note" data-testid="mp-lore-empty">
              {t.mp.loreActivity.empty}
            </div>
          ) : state.data ? (
            <>
              {!loreEnabled && (
                // The headings below are history and stay readable; what is
                // gone is the place a link would land.
                <div className="mp-recall__note" data-testid="mp-lore-switchoff">
                  {t.mp.loreActivity.loreOff}
                </div>
              )}
              <ul className="mp-recall__list">
                {state.data.rows.map((row, i) => {
                  const line = sinceBoot(row.sinceBootSecs, t);
                  // 🔴 THE KEY CANNOT BE entryId ALONE. Reading the same entry
                  // twice in one session is TWO rows and is the signal the
                  // journal was built to carry (00082); a key that collapsed
                  // them would delete the repetition from the screen.
                  const key = `${row.createdTs}:${row.door}:${row.entryId}:${i}`;
                  // A heading nobody can look up gets the 查不到 wording and no
                  // link. ⚠️ It says 「查不到這個 id」 and NOT 「被刪掉了」 —
                  // measured 2026-09-06, there is no delete path for lore
                  // entries anywhere in the tree, so naming one would send its
                  // reader looking for a mechanism that does not exist.
                  const label = row.headingFound
                    ? row.heading
                    : t.mp.loreActivity.headingGone(row.entryId);
                  const linkable = row.headingFound && loreEnabled;
                  return (
                    <li
                      className="mp-recall__row"
                      key={key}
                      data-testid="mp-lore-row"
                    >
                      <span
                        className="mp-recall__when"
                        data-testid="mp-lore-when"
                      >
                        {line}
                      </span>
                      <span className="mp-recall__sep" aria-hidden="true">
                        {" - "}
                      </span>
                      <span className="mp-recall__verb">
                        {t.mp.loreActivity.loadLore}
                      </span>
                      {linkable ? (
                        <button
                          type="button"
                          className="mp-recall__heading mp-recall__heading--link"
                          data-testid="mp-lore-heading-link"
                          onClick={() =>
                            setRoute({ page: "lore", loreEntryId: row.entryId })
                          }
                        >
                          {label}
                        </button>
                      ) : (
                        <span
                          className="mp-recall__heading"
                          data-testid="mp-lore-heading-plain"
                        >
                          {label}
                        </span>
                      )}
                      {/* Retirement is DISPLAY, not an error: the entry is
                        * still there and the deep link still reaches it. The
                        * mark exists so a retired memory does not read as a
                        * live one. */}
                      {row.status === "retired" && (
                        <span
                          className="mp-recall__tag"
                          data-testid="mp-lore-retired"
                        >
                          {t.mp.loreActivity.retiredTag}
                        </span>
                      )}
                    </li>
                  );
                })}
              </ul>
            </>
          ) : null}
        </div>
      )}
    </div>
  );
}
