// UpgradeInstructionsCard — 設定 › 系統更新與備份 · 換版交代單 (T-79).
//
// Its own file rather than a private function inside SettingsPage, for the same
// reason SigningKeysCard is: the browser-level guard beside it has to mount the
// real card, and exporting one purely so a test can reach it is a backdoor that
// then has to be kept honest forever.
import { useEffect, useState } from "react";
import { useI18n } from "../i18n";
import { useUpgradeInstructions } from "../hooks/useUpgradeInstructions";
import { ConfirmModal } from "./ConfirmModal";
import { formatAbsolute } from "../lib/dateFormat";

/**
 * 換版交代單 (T-79) — the standing instructions the owner leaves for the
 * assistant. The station hands the open ones to her in a chat message at every
 * upgrade, and keeps doing it until somebody ticks one off.
 *
 * WHAT THIS CARD HAS TO MAKE VISIBLE, beyond listing rows:
 *
 *  - **How many are still open.** That is the number that makes this feature's
 *    only failure mode visible — an instruction nobody ever acts on. It comes
 *    from the server, and it is shown even when the list is short, because a
 *    reader counting rows is doing the server's job with worse information.
 *  - **That a finished instruction is not gone.** Ticked rows stay in the list;
 *    they are the only evidence the work was ever picked up. So they are styled
 *    as finished rather than hidden — hiding them would make "she did it" and
 *    "it was never written" look identical.
 *  - **That the two verbs are not the same kind of thing.** 標記完成 is the
 *    assistant's verb and means "I did this"; the owner may use it when he did
 *    the work himself. 收回 is a DELETE with no undo, and it exists only
 *    because there is no honest way to retract a mistake with a tick. So the
 *    tick is a plain button and the withdraw goes through a confirmation that
 *    says what it costs.
 *  - 🔴 **That this list can be out of date.** This package ships no SSE topic
 *    for instructions, so the assistant ticking one off does not reach an open
 *    cockpit. "She just ticked it" and "this list is stale" look identical on
 *    screen, and only the second needs a reload — so the card says so in words
 *    rather than letting a stale list pass for a live one.
 */
export function UpgradeInstructionsCard() {
  const { t } = useI18n();
  const d = t.upgradeInstructions;
  // The fallback is this card's own copy, used only for a rejection that
  // carried no server reason — never an empty error line, which reads as
  // "nothing went wrong".
  const { instructions, openCount, loading, busy, error, create, markDone, remove } =
    useUpgradeInstructions(d.actionFailed);
  const [draft, setDraft] = useState("");
  const [confirming, setConfirming] = useState<string | null>(null);
  const nowSecs = Math.floor(Date.now() / 1000);

  // Close the confirmation when the withdrawal actually LANDS — i.e. when the
  // row is no longer in the set the server just answered with. A refusal leaves
  // the row there, so the modal stays open carrying the server's reason, which
  // is where the user pressed and where they are looking.
  useEffect(() => {
    if (confirming !== null && !instructions.some((u) => u.id === confirming)) {
      setConfirming(null);
    }
  }, [instructions, confirming]);

  const trimmed = draft.trim();

  // The draft is cleared only when the write LANDED. Clearing on a rejection
  // would throw away what the owner typed and leave him nothing to retry with.
  function submit() {
    if (trimmed === "" || busy) return;
    void create(trimmed).then((ok) => {
      if (ok) setDraft("");
    });
  }

  const target = instructions.find((u) => u.id === confirming) ?? null;

  return (
    <>
      <h2 className="settings__title settings__title--doc">{d.title}</h2>
      <div className="param-card upgrade-instr" data-testid="set-upgrade-instructions">
        <div className="upgrade-instr__hint">{d.intro}</div>

        {loading ? (
          <div className="upgrade-instr__loading">{d.loading}</div>
        ) : instructions.length === 0 ? (
          <div className="upgrade-instr__loading">{d.emptyState}</div>
        ) : (
          <>
            <div className="upgrade-instr__count" data-testid="set-upgrade-instructions-count">
              {/* Zero open is its own sentence rather than "0 still open":
                  a count of nothing reads as a stat, and this one is a state. */}
              {openCount === 0 ? d.allDoneLabel : d.openCountLabel(openCount)}
            </div>
            <ul className="upgrade-instr__list">
              {instructions.map((u) => (
                <li
                  className={`upgrade-instr__row${u.done ? " upgrade-instr__row--done" : ""}`}
                  key={u.id}
                  data-testid={`set-upgrade-instruction-${u.id}`}
                  data-done={u.done ? "yes" : "no"}
                >
                  <span
                    className={`upgrade-instr__badge upgrade-instr__badge--${
                      u.done ? "done" : "open"
                    }`}
                  >
                    {u.done ? d.doneBadge : d.openBadge}
                  </span>
                  <p className="upgrade-instr__body">{u.body}</p>
                  <span className="upgrade-instr__meta">
                    {`${d.createdLabel} ${formatAbsolute(u.createdTs, nowSecs)}`}
                    {/* doneBy is null while open — the mapper narrowed the
                        wire's "" so this branch cannot print an empty author
                        beside a date of 1970. */}
                    {u.done && u.doneBy !== null && ` · ${d.doneLabel(u.doneBy)}`}
                  </span>
                  <span className="upgrade-instr__rowactions">
                    {!u.done && (
                      <button
                        type="button"
                        className="btn btn--ghost upgrade-instr__done"
                        data-testid={`set-upgrade-instruction-done-${u.id}`}
                        disabled={busy}
                        onClick={() => markDone(u.id)}
                      >
                        {d.doneButton}
                      </button>
                    )}
                    <button
                      type="button"
                      className="btn btn--danger-ghost upgrade-instr__remove"
                      data-testid={`set-upgrade-instruction-remove-${u.id}`}
                      disabled={busy}
                      onClick={() => setConfirming(u.id)}
                    >
                      {d.deleteButton}
                    </button>
                  </span>
                </li>
              ))}
            </ul>
          </>
        )}

        <div className="upgrade-instr__form">
          <textarea
            className="upgrade-instr__input"
            data-testid="set-upgrade-instructions-input"
            rows={3}
            value={draft}
            placeholder={d.addPlaceholder}
            disabled={busy}
            onChange={(e) => setDraft(e.target.value)}
          />
          <div className="upgrade-instr__actions">
            <button
              type="button"
              className="btn"
              data-testid="set-upgrade-instructions-add"
              disabled={busy || loading || trimmed === ""}
              onClick={submit}
            >
              {d.addButton}
            </button>
            <span className="upgrade-instr__hint upgrade-instr__hint--inline">
              {d.addHint}
            </span>
          </div>
        </div>

        <p className="upgrade-instr__stale">{d.staleHint}</p>

        {error !== "" && (
          <div
            className="set-error param-error"
            data-testid="set-upgrade-instructions-error"
          >
            {error}
          </div>
        )}

        {target !== null && (
          <ConfirmModal
            testId="set-upgrade-instruction-confirm"
            confirmTestId="set-upgrade-instruction-confirm-ok"
            danger
            busy={busy}
            cancelLabel={d.deleteConfirmCancel}
            confirmLabel={d.deleteConfirmOk}
            body={
              <>
                <strong className="upgrade-instr__confirm-title">
                  {d.deleteConfirmTitle}
                </strong>
                {/* The instruction's own words, so the person confirming is
                    looking at WHICH one rather than at the id they clicked. */}
                <span className="upgrade-instr__confirm-quote">{target.body}</span>
                <span className="upgrade-instr__confirm-warn">
                  {d.deleteConfirmBody}
                </span>
              </>
            }
            error={error !== "" ? error : null}
            onCancel={() => setConfirming(null)}
            onConfirm={() => remove(target.id)}
          />
        )}
      </div>
    </>
  );
}
