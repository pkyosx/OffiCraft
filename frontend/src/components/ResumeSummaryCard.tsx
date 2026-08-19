import { useState, useEffect, useRef, type ReactNode } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import type {
  MemberResumeSummaryView,
  ChatMessage,
  ResumeRosterMemberView,
} from "../api/adapter";
import { Markdown } from "./Markdown";
import { ChevronDownIcon, ChevronRightIcon, ClockIcon } from "./icons";
// 🔴 This component draws itself with the `.mp-resume__*` block, so it OWNS
// that stylesheet's import (styleOwnership.test.ts). Both detail panels
// already import member-detail.css, but relying on that is what makes a
// component silently unstyled the day someone renders it somewhere else.
import "./member-detail.css";

// ResumeSummaryCard — 履歷摘要, the wake snapshot the cockpit shows for ONE
// agent, under 初始 PROMPT (afterPromptCards, the panel's last slot).
//
// It lives in its own file because BOTH detail panels render it: the staff
// panel always did, and the outsource panel gained it once the owner released
// the target read for workers (T-4595, ruling rc-64b712bfc703 option ①). The
// alternative — a second copy in WorkerDetailPanel — would have been two
// renderings of the same server payload, free to drift apart with nothing
// watching. `agentId` is the TARGET whose snapshot is fetched, so a worker id
// works exactly as a member id does.
//
// 🔴 HARD REQUIREMENT the panel's default load must not break: no request is
// issued for this section until the FIRST EXPAND. That is why the fetch fn is
// read through a ref (never in the effect's deps — an inline arrow rebuilt
// every render would tear the effect down mid-flight on any repaint, T-7526),
// the effect deps are `[showResumeSummary, agentId]` only, and the loaded
// stamp is written on ARRIVAL (not at fetch start) so a failed read retries.
//
// The `mp-resume-*` test ids are deliberately unchanged from when this lived
// inside MemberDetailPanel: the existing staff-panel tests are the regression
// net for the extraction itself, and renaming them would have thrown that net
// away in the same commit that needed it most.
//
// ── 🔴 THE RENDERING CONTRACT ────────────────────────────────────────────────
// Owner, verbatim: 「就是你怎麼給 agent 的就怎麼給我,格式應該要我們兩個雙方都
// 看得懂」 and 「我不要規則 我要看到到時候 agent 實際看到的 魔鬼藏在細節裡」.
//
// So: WHAT THIS DRAWS MUST LINE UP, SECTION BY SECTION, WITH THE SNAPSHOT THE
// AGENT RECEIVES AT THE SAME MOMENT. Three rules fall out of that, and they are
// the reason this component looks the way it does:
//
//   1. NO SECOND ANSWER. Anything the server already decided is PRINTED, never
//      re-derived here. `ts_display` and `generated_at` come rendered — this
//      component owns NO date formatter, because a browser-side one would state
//      the same instant in the viewer's zone while the agent reads the
//      server's: same payload, two different times, and nothing red.
//      Same for `from_name` — unresolved is "", and "" renders as the id ALONE.
//      Back-filling the id into the name slot would make "no name on file"
//      indistinguishable from "the name really is that id".
//   2. TWO KINDS OF ABSENCE, TWO SETS OF WORDS. `body_omitted_chars` > 0 means
//      THIS message IS here, folded, and the text is still on the server.
//      `chat_earlier_omitted` means whole messages MAY be missing entirely: the
//      stream was cut at a read or budget limit and nothing looked past the cut,
//      so it is raised even when nothing older exists — its `hint` says how to
//      check and fetch, and is shown VERBATIM. The two halves are asymmetric on
//      purpose and must stay that way: the fold is CERTAIN and counted, the cut
//      is a MAYBE that only a fetch settles.
//      Reading one as the other is how a reader concludes it has seen a
//      conversation it has not seen, so the two must never share a word — the
//      two-way vocabulary guard in the payload-parity test holds them apart.
//      ⚠️ There WAS a third marker here (`chat_budget_overrun`, T-3970): the
//      chat block could exceed its budget because each conversation line's
//      reserved messages were billed to the budget and never evicted by it. The
//      per-line floor was removed on 2026-08-13 by owner ruling, so the budget
//      is now a real ceiling and that marker was structurally unable to fire
//      again — it is gone rather than left standing as a guard nobody keeps.
//   3. EVERY SECTION SHOWS. Long ones may be collapsed, but which sections
//      EXIST is always visible — a section that renders nothing at all is
//      indistinguishable from a section the payload never carried.
//      ⚠️ This binds MARKERS too, not just sections: the truncation notice is
//      a sibling of the chat empty state, because the payload where every
//      message was evicted is exactly the payload whose reader most needs to be
//      told the line was cut.
//   4. THE SERVER'S PROSE IS PRINTED, AND PRINTED AS WRITTEN. `note` and the
//      cut `hint` are one text serving both readers (owner 2026-08-15: 「應該
//      好好寫 讓兩邊看得懂」), so the cockpit never substitutes a human-only
//      rewrite — but it must also not DESTROY the formatting that text carries.
//      Both go through `Markdown` with `breaks`, the same path a message body
//      takes: line breaks stay breaks, `**` reads as emphasis, and the field
//      and tool names the reader has to type stay in backticks. Rendered as
//      bare text nodes they collapsed into a wall of prose with the markup
//      showing — verbatim in bytes, unreadable on screen, which is the failure
//      this ticket was opened for. VERBATIM IS ABOUT THE WORDS, NOT THE
//      TYPOGRAPHY.
//
// Content here is agent- and outsider-authored, so bodies go through
// `Markdown`, which builds React elements only and never touches
// dangerouslySetInnerHTML — inline HTML in a snapshot stays inert text.

/** Render a chat participant as BOTH its display name and its id.
 *
 * 🔴 The id is never dropped (it is the ADDRESS you must reply to; names are
 * editable and repeat) and the name is never FABRICATED: `name === ""` is the
 * server saying the id resolves to no roster row, and that renders as the id
 * alone. Filling the id into the name slot would erase that distinction. */
function Party({
  name,
  id,
  testid,
}: {
  name: string;
  id: string;
  testid: string;
}) {
  return (
    <span className="mp-resume__party" data-testid={testid}>
      {name !== "" && <span className="mp-resume__partyname">{name}</span>}
      <code className="mp-resume__partyid">{id}</code>
    </span>
  );
}

/** A section whose title is ALWAYS rendered and whose body can be folded away.
 * The title stays put on purpose: which sections the snapshot carries is part
 * of what the owner is checking against the agent's copy, so a collapsed
 * section must still announce that it exists. */
function Section({
  title,
  testid,
  collapseLabel,
  expandLabel,
  children,
}: {
  title: string;
  testid: string;
  collapseLabel: string;
  expandLabel: string;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(true);
  return (
    <div className="mp-resume__section" data-testid={testid}>
      <div className="mp-resume__sectionhead">
        <span className="mp-resume__sectiontitle">{title}</span>
        <button
          type="button"
          className="mp-resume__sectiontoggle"
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
          data-testid={`${testid}-toggle`}
        >
          {open ? collapseLabel : expandLabel}
        </button>
      </div>
      {open && children}
    </div>
  );
}


/** One chat message, rendered as the snapshot carries it: who (name AND id) →
 * who, WHEN (the server's own rendered stamp), the body, and — folded in place
 * on the very message that opened it — the reply card's decision.
 *
 * 🔴 The card is drawn HERE, inline, and not in a separate card section. The
 * payload deliberately gives a card ONE home (the message id it rides on);
 * giving it a second one on screen would carry the same decision twice and
 * leave the reader joining two lists by hand — the exact chore the inline
 * shape was chosen to remove. */
/** One message row of the wake snapshot's chat block.
 *
 * 🔴 EXPORTED FOR THE LAYOUT GUARD, and that is the only reason. The alignment
 * contract this row carries (body and marks on the section's own left edge, the
 * fold mark beside its message, the timestamp always in one place) is a
 * GEOMETRY statement, and geometry is exactly what jsdom cannot see. The
 * Playwright CT guard therefore mounts THIS component rather than a hand-copied
 * skeleton of it — a copy would keep passing while the real row drifted, which
 * is the failure mode the guard exists to remove.
 * See visual-guards/resume-chat-row-align.ct.spec.tsx. */
export function ChatRow({
  m,
  t,
  msg,
}: {
  m: ChatMessage;
  t: ReturnType<typeof useI18n>["t"];
  msg: ReturnType<typeof useI18n>["msg"];
}) {
  const r = t.mp.resumeSummary;
  const card = m.card ?? null;
  return (
    <div className="mp-resume__chatrow" data-testid="mp-resume-chat-row">
      <div className="mp-resume__chatmeta">
        {/* TWO LINES, in the markup rather than left to wrapping. The parties
          * line may wrap when the names are long; the stamp line below it may
          * not be affected by that, because "where is the time" must have the
          * same answer on every row. It used to depend on leftover width. */}
        <div className="mp-resume__chatparties">
          <Party
            name={m.fromName ?? ""}
            id={m.from}
            testid="mp-resume-chat-from"
          />
          <span className="mp-resume__arrow" aria-hidden="true">
            →
          </span>
          <Party name={m.toName ?? ""} id={m.to} testid="mp-resume-chat-to" />
        </div>
        <div className="mp-resume__chatstamp">
          {/* PRINTED, never formatted here. See rule 1 in the file header. */}
          {(m.tsDisplay ?? "") !== "" && (
            <span className="mp-resume__chatts" data-testid="mp-resume-chat-ts">
              {m.tsDisplay}
            </span>
          )}
          {m.replyCardStatus != null && (
            <span
              className="mp-resume__cardstatus"
              data-testid="mp-resume-chat-cardstatus"
            >
              {r.replyCardStatusLabel}
              {": "}
              {m.replyCardStatus}
            </span>
          )}
        </div>
      </div>
      <Markdown
        source={m.body}
        breaks
        className="mp-resume__chatbody doc-md"
      />
      {/* COLLAPSE — this message IS here, shortened, and the rest is still on
        * the server. It is a MARK (word + count), not a sentence: what the mark
        * means is stated once at the top of the block. Its wording shares no
        * word with the truncation notice under the list; see rule 2 in the file
        * header. */}
      {(m.bodyOmittedChars ?? 0) > 0 && (
        <div
          className="mp-resume__folded"
          data-testid="mp-resume-chat-body-omitted"
        >
          {msg.resumeBodyOmitted(m.bodyOmittedChars ?? 0)}
        </div>
      )}
      {(m.attachments ?? []).length > 0 && (
        <div
          className="mp-resume__attachments"
          data-testid="mp-resume-chat-attachments"
        >
          <span className="mp-resume__generatedlabel">
            {r.cardAttachmentsLabel}
          </span>{" "}
          {m.attachments.map((a) => (
            <code className="mp-resume__partyid" key={a.id}>
              {a.filename !== "" ? a.filename : a.id}
            </code>
          ))}
        </div>
      )}
      {card && (
        <div className="mp-resume__card" data-testid="mp-resume-chat-card">
          {card.options.length > 0 && (
            <div className="mp-resume__cardoptions">
              <span className="mp-resume__generatedlabel">
                {r.cardOptionsLabel}
              </span>
              {card.options.map((opt, i) => (
                <span
                  className="mp-resume__cardoption"
                  key={`${i}-${opt}`}
                  data-testid="mp-resume-card-option"
                  data-picked={i === card.answerOptionIdx ? "true" : "false"}
                >
                  {opt}
                  {/* options[0] is the AI pick — a property of how the card
                    * was OFFERED, which is why it is tagged separately from
                    * which option was actually chosen. */}
                  {i === 0 && (
                    <span className="mp-resume__cardtag">{r.cardAiPickTag}</span>
                  )}
                  {i === card.answerOptionIdx && (
                    <span
                      className="mp-resume__cardtag mp-resume__cardtag--picked"
                      data-testid="mp-resume-card-picked"
                    >
                      {r.cardPickedTag}
                    </span>
                  )}
                </span>
              ))}
            </div>
          )}
          {card.answerText !== "" && (
            <div data-testid="mp-resume-card-answer-text">
              <span className="mp-resume__generatedlabel">
                {r.cardAnswerTextLabel}
              </span>{" "}
              <Markdown
                source={card.answerText}
                breaks
                className="mp-resume__chatbody doc-md"
              />
            </div>
          )}
          {/* Rendered by the server, in the same form as ts_display and for
            * the same reason. "" means still waiting — said as a sentence,
            * not as a blank. */}
          {card.answeredAtDisplay !== "" ? (
            <div data-testid="mp-resume-card-answered-at">
              <span className="mp-resume__generatedlabel">
                {r.cardAnsweredAtLabel}
              </span>{" "}
              {card.answeredAtDisplay}
            </div>
          ) : (
            <div data-testid="mp-resume-card-unanswered">
              {r.cardUnanswered}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

/** One roster row: the id you address, the name you read, and — depending on
 * kind — the member's duty or the contractor's bound task and progress. The
 * asymmetry is the server's (正職給職責、外包給任務標題), so the row shows
 * whichever field the payload actually filled rather than laying out empty
 * slots for both. */
function RosterRow({
  r,
  t,
}: {
  r: ResumeRosterMemberView;
  t: ReturnType<typeof useI18n>["t"];
}) {
  const d = t.mp.resumeSummary;
  return (
    <div className="mp-resume__rosterrow" data-testid="mp-resume-roster-row">
      <div className="mp-resume__chatmeta">
        <Party name={r.name} id={r.id} testid="mp-resume-roster-party" />
        <span className="mp-resume__rosterkind">{r.kind}</span>
        {r.roleName !== "" && (
          <span className="mp-resume__rosterkind">{r.roleName}</span>
        )}
        <span className="mp-resume__rosterpresence">{r.presence}</span>
        {r.machine !== "" && (
          <code className="mp-resume__partyid">{r.machine}</code>
        )}
      </div>
      {r.duty !== "" && (
        <div data-testid="mp-resume-roster-duty">
          <span className="mp-resume__generatedlabel">{d.rosterDutyLabel}</span>{" "}
          <Markdown
            source={r.duty}
            className="mp-resume__chatbody doc-md"
          />
        </div>
      )}
      {r.currentTask !== "" && (
        <div data-testid="mp-resume-roster-task">
          <span className="mp-resume__generatedlabel">
            {d.rosterCurrentTaskLabel}
          </span>{" "}
          <span className="mp-resume__chatbody">{r.currentTask}</span>{" "}
          {/* 0/0 is AMBIGUOUS on its own — a bound task with no steps yet, or
            * no bound task at all — and task_status is the field that tells
            * them apart, so the two are never shown without each other. */}
          <span className="mp-resume__rosterpresence">
            {r.taskStatus} {r.progressDone}/{r.progressTotal}
          </span>
        </div>
      )}
    </div>
  );
}

export function ResumeSummaryCard({ agentId }: { agentId: string }) {
  const { t, msg } = useI18n();
  const [show, setShow] = useState(false);
  const [state, setState] = useState<{
    data: MemberResumeSummaryView | null;
    loading: boolean;
    error: boolean;
  }>({ data: null, loading: false, error: false });
  const loadedKeyRef = useRef<string | null>(null);
  const inFlightKeyRef = useRef<string | null>(null);
  const fetchRef = useRef<() => Promise<MemberResumeSummaryView>>(() =>
    api.getMemberResumeSummary(agentId),
  );
  fetchRef.current = () => api.getMemberResumeSummary(agentId);

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
        // No stamp: the read failed, so re-expanding (or 重試) reads again.
        setState({ data: null, loading: false, error: true });
      });
  }

  useEffect(() => {
    if (!show) return;
    if (loadedKeyRef.current === agentId) return;
    if (inFlightKeyRef.current === agentId) return;
    runFetch(agentId);
    // NO cleanup that cancels the read (a repaint/unmount is not a
    // cancellation); staleness is decided by comparing the key, not an
    // `alive` flag a repaint can flip.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [show, agentId]);

  const stats: Array<[string, string, number]> = state.data
    ? [
        ["chatCount", t.mp.resumeSummary.chatCount, state.data.overview.chatCount],
        ["chatChars", t.mp.resumeSummary.chatChars, state.data.overview.chatChars],
        [
          "tasksReturned",
          t.mp.resumeSummary.tasksReturned,
          state.data.overview.tasksReturned,
        ],
        [
          "tasksOpenTotal",
          t.mp.resumeSummary.tasksOpenTotal,
          state.data.overview.tasksOpenTotal,
        ],
        [
          "tasksDetailChars",
          t.mp.resumeSummary.tasksDetailChars,
          state.data.overview.tasksDetailChars,
        ],
        [
          "cardsWaiting",
          t.mp.resumeSummary.cardsWaiting,
          state.data.overview.cardsWaiting,
        ],
        [
          "cardsAnsweredRecent",
          t.mp.resumeSummary.cardsAnsweredRecent,
          state.data.overview.cardsAnsweredRecent,
        ],
        [
          "stepsOnAnsweredCard",
          t.mp.resumeSummary.stepsOnAnsweredCard,
          state.data.overview.stepsOnAnsweredCard,
        ],
        [
          "stepsOnAnsweredCardChars",
          t.mp.resumeSummary.answeredCardStepChars,
          state.data.overview.stepsOnAnsweredCardChars,
        ],
        // The two studio-floor block sizes. They belong next to the other
        // figures for the same reason the blocks themselves belong on screen:
        // the agent's copy reports them, so hiding them here would be one more
        // place the two views could disagree without anything noticing.
        [
          "rosterChars",
          t.mp.resumeSummary.rosterChars,
          state.data.overview.rosterChars,
        ],
        [
          "machinesChars",
          t.mp.resumeSummary.machinesChars,
          state.data.overview.machinesChars,
        ],
      ]
    : [];

  return (
    <div className="mp-card mp-expand">
      <button
        type="button"
        className="mp-expand__head"
        aria-expanded={show}
        onClick={() => setShow((v) => !v)}
        data-testid="mp-resume-toggle"
      >
        <ClockIcon size={15} className="mp-expand__icon" />
        <span className="mp-expand__title">{t.mp.resumeSummary.title}</span>
        {show ? (
          <ChevronDownIcon size={16} className="mp-expand__chevron" />
        ) : (
          <ChevronRightIcon size={16} className="mp-expand__chevron" />
        )}
      </button>
      {show && (
        <div className="mp-expand__body" data-testid="mp-resume-body">
          {/* `!state.data && !state.error` covers the one render tick between
           * the toggle click and the effect's own setState — treated as
           * loading, not as a fabricated empty state. */}
          {state.loading || (!state.data && !state.error) ? (
            t.mp.resumeSummary.loading
          ) : state.error ? (
            <div data-testid="mp-resume-error">
              <span>{t.mp.resumeSummary.error}</span>{" "}
              <button
                type="button"
                className="doc-btn"
                data-testid="mp-resume-retry"
                onClick={() => runFetch(agentId)}
              >
                {t.mp.resumeSummary.retry}
              </button>
            </div>
          ) : state.data ? (
            <>
              {/* The anchor for every ts_display below it. Rendered FIRST and
                * as the server wrote it — without it, "2026-08-13 09:47" is a
                * timestamp the reader has nothing to measure against. Skipped
                * only when the server sent nothing, which is honest silence
                * rather than a time this component invented. */}
              {state.data.generatedAt !== "" && (
                <div
                  className="mp-resume__generated"
                  data-testid="mp-resume-generated-at"
                >
                  <span className="mp-resume__generatedlabel">
                    {t.mp.resumeSummary.generatedAtLabel}
                  </span>{" "}
                  <span data-testid="mp-resume-generated-at-value">
                    {state.data.generatedAt}
                  </span>
                </div>
              )}
              {/* 🔴 DRAWN THROUGH `Markdown`, AND THAT IS THE POINT (rule 4 in
                * the file header). The server writes this note for BOTH
                * readers, so it carries the only formatting plain text has:
                * line breaks, `**` for emphasis, backticks around the field
                * and tool names the reader must type. As a bare text node with
                * no `white-space` rule, every one of those collapsed — seven
                * lines ran together into one paragraph and the `**` printed
                * literally, which is the unreadable wall this ticket exists to
                * remove. `breaks` is required: the source separates its lines
                * with single \n, and without it they would be re-joined. */}
              <div data-testid="mp-resume-note">
                <Markdown
                  source={state.data.note}
                  breaks
                  className="mp-resume__note doc-md"
                />
              </div>
              <div
                className="mp-resume__statsgrid"
                data-testid="mp-resume-overview"
              >
                {stats.map(([key, label, value]) => (
                  <div className="mp-resume__stat" key={key}>
                    <div className="mp-resume__statlabel">{label}</div>
                    <div
                      className="mp-resume__statvalue"
                      data-testid={`mp-resume-stat-${key}`}
                    >
                      {value}
                    </div>
                  </div>
                ))}
              </div>

              <Section
                title={t.mp.resumeSummary.chatSection}
                testid="mp-resume-chat-section"
                collapseLabel={t.mp.resumeSummary.collapse}
                expandLabel={t.mp.resumeSummary.expand}
              >
                {/* TRUNCATION, not collapse: whole messages that are NOT in
                  * this payload.
                  *
                  * 🔴 IT SITS AT THE TOP, AND THE POSITION IS THE POINT
                  * (owner 2026-08-15). The list runs OLD → NEW, so the
                  * boundary this marker describes — "there may be more,
                  * further back" — is the TOP edge. It used to render below
                  * the last message, which was correct only while the list ran
                  * new → old; the order was changed and the marker was not
                  * moved, leaving it pointing at the wrong end. A reader
                  * scrolling up to the start hit the oldest message and had no
                  * way to know the line was cut there. Pinned by DOM ORDER in
                  * the payload-parity suite — a presence check stays green
                  * with it back at the bottom.
                  *
                  * 🔴 IT IS ALSO A SIBLING OF THE EMPTY STATE, NOT A CHILD OF
                  * THE MESSAGE LIST. Moving it to the top the first time put it
                  * INSIDE the non-empty branch, and that silently deleted it
                  * from the one payload that needs it most: budget pressure can
                  * evict EVERY message, and the reader was then shown a bare
                  * 「沒有訊息」 with nothing saying the line had been cut — the
                  * exact false reading rule 3 of the header forbids. Independent
                  * review caught it; the parity fixture is never empty, so no
                  * test did.
                  *
                  * The hint is the SERVER's own recovery instruction and is
                  * printed VERBATIM — re-writing it here would be the cockpit
                  * inventing a procedure it cannot keep in step with the
                  * endpoint. Verbatim is about the WORDS, not the typography:
                  * it goes through `Markdown` for the same reason the note
                  * does (rule 4). */}
                {state.data.chatEarlierOmitted.omitted && (
                  <div
                    className="mp-resume__chatcut"
                    data-testid="mp-resume-chat-earlier-omitted"
                  >
                    <span className="mp-resume__chatcutlabel">
                      {t.mp.resumeSummary.chatCutLabel}
                    </span>
                    <div data-testid="mp-resume-chat-earlier-omitted-hint">
                      <Markdown
                        source={state.data.chatEarlierOmitted.hint}
                        breaks
                        className="mp-resume__chatcuthint doc-md"
                      />
                    </div>
                  </div>
                )}
                {state.data.chat.length === 0 ? (
                  <div className="mp-resume__empty">
                    {t.mp.resumeSummary.chatEmpty}
                  </div>
                ) : (
                  <>
                    {/* The COLLAPSE convention, stated ONCE for the whole
                      * block. It used to ride under every folded message as a
                      * full sentence; on a snapshot with hundreds of rows that
                      * template cost more than the folds saved. It draws only
                      * when at least one message is actually folded — a legend
                      * for a mark nobody can see is the same orphan problem in
                      * the other direction. */}
                    {state.data.chat.some(
                      (m) => (m.bodyOmittedChars ?? 0) > 0,
                    ) && (
                      <div
                        className="mp-resume__foldednote"
                        data-testid="mp-resume-chat-body-omitted-note"
                      >
                        {t.mp.resumeSummary.bodyOmittedNote}
                      </div>
                    )}
                    {state.data.chat.map((m) => (
                      <ChatRow key={m.id} m={m} t={t} msg={msg} />
                    ))}
                  </>
                )}
              </Section>

              <Section
                title={t.mp.resumeSummary.tasksSection}
                testid="mp-resume-tasks-section"
                collapseLabel={t.mp.resumeSummary.collapse}
                expandLabel={t.mp.resumeSummary.expand}
              >
                {state.data.tasks.length === 0 ? (
                  <div className="mp-resume__empty">
                    {t.mp.resumeSummary.tasksEmpty}
                  </div>
                ) : (
                  state.data.tasks.map((rt) => (
                    <div className="mp-resume__task" key={rt.id}>
                      <div className="mp-resume__taskrow">
                        <code className="mp-resume__taskno">{rt.taskNo}</code>
                        <span className="mp-resume__tasktitle">{rt.title}</span>
                        <span className="mp-resume__taskstatus">{rt.status}</span>
                      </div>
                      {rt.answeredCardSteps.length > 0 && (
                        <div
                          className="mp-resume__answeredcard"
                          data-testid="mp-resume-task-answered-card"
                        >
                          <div className="mp-resume__answeredcardlabel">
                            {t.mp.resumeSummary.answeredCardSteps}
                          </div>
                          {rt.answeredCardSteps.map((step) => (
                            <div
                              className="mp-resume__answeredcardstep"
                              data-testid="mp-resume-answered-card-step"
                              key={step.stepId}
                            >
                              <span className="mp-resume__answeredcardstepname">
                                {step.stepName}
                              </span>
                              <code className="mp-resume__answeredcardid">
                                {step.cardId}
                              </code>
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  ))
                )}
              </Section>

              {/* ── the two studio-floor blocks ──────────────────────────────
                * These are the sections the seam used to drop on the floor.
                * They render UNCONDITIONALLY (the empty state is a sentence,
                * not an absence) because "this snapshot carries no roster" and
                * "the cockpit forgot to draw the roster" looked identical for
                * as long as the bug lived, and that is precisely the failure
                * this panel exists to make impossible. */}
              <Section
                title={t.mp.resumeSummary.rosterSection}
                testid="mp-resume-roster-section"
                collapseLabel={t.mp.resumeSummary.collapse}
                expandLabel={t.mp.resumeSummary.expand}
              >
                {state.data.roster.length === 0 ? (
                  <div className="mp-resume__empty">
                    {t.mp.resumeSummary.rosterEmpty}
                  </div>
                ) : (
                  state.data.roster.map((r) => (
                    <RosterRow key={r.id} r={r} t={t} />
                  ))
                )}
              </Section>

              <Section
                title={t.mp.resumeSummary.machinesSection}
                testid="mp-resume-machines-section"
                collapseLabel={t.mp.resumeSummary.collapse}
                expandLabel={t.mp.resumeSummary.expand}
              >
                {state.data.machines === null ? (
                  <div className="mp-resume__empty">
                    {t.mp.resumeSummary.machinesEmpty}
                  </div>
                ) : (
                  <>
                    <div
                      className="mp-resume__youareon"
                      data-testid="mp-resume-you-are-on"
                    >
                      <span className="mp-resume__generatedlabel">
                        {t.mp.resumeSummary.machinesYouAreOnLabel}
                      </span>{" "}
                      {/* "" is the SERVER-RECORDED absence of a binding, said
                        * as a sentence. It is never guessed from the machine
                        * list — a host's own idea of its name is exactly the
                        * thing machine_id exists to stop us trusting. */}
                      {state.data.machines.youAreOn !== "" ? (
                        <code className="mp-resume__partyid">
                          {state.data.machines.youAreOn}
                        </code>
                      ) : (
                        <span className="mp-resume__empty">
                          {t.mp.resumeSummary.machinesYouAreOnNone}
                        </span>
                      )}
                    </div>
                    {state.data.machines.list.map((mc) => (
                      <div
                        className="mp-resume__machinerow"
                        key={mc.machineId}
                        data-testid="mp-resume-machine-row"
                      >
                        <code className="mp-resume__partyid">
                          {mc.machineId}
                        </code>
                        <span className="mp-resume__machinename">
                          {mc.displayName}
                        </span>
                        <span className="mp-resume__machineonline">
                          {mc.online
                            ? t.mp.resumeSummary.machineOnline
                            : t.mp.resumeSummary.machineOffline}
                        </span>
                      </div>
                    ))}
                  </>
                )}
              </Section>
            </>
          ) : (
            <div className="mp-resume__empty" data-testid="mp-resume-empty">
              {t.mp.resumeSummary.chatEmpty}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
