// components/BootDocPage.tsx — the editable surface for ONE boot-context block
// (T-791e): 系統互動, 啟動程序（Claude Code）, 啟動程序（Codex CLI）.
//
// 🔴 THIS FILE HOLDS NO EDITOR. It is the three blocks' wiring — which document,
// which words, which slots — and nothing else: no draft state, no textarea, no
// button group, no usage readout, no confirmation of its own. All of that is
// <DocCard>, the one shell every editable document in 設定 draws itself with
// (T-c33e). The version this replaced hand-copied that markup, which is how the
// two surfaces drifted into two different shapes of the same card; the owner's
// ruling is that they must be ONE component, so the only thing this page may
// own is what is genuinely特有 about a boot-context block.
//
// What it owes on top of "edit / version history / restore to factory" comes
// from one sentence: THE PERSON PRESSING THE BUTTON IS THE OWNER, NOT US.
//
// ⚠️ THIS HEADER USED TO SAY THREE THINGS THIS PAGE "MUST SAY OUT LOUD" — when
// it takes effect, that history keeps ten counted in saves, and what the cap
// does. That block was rendered above the card, and the owner had it REMOVED on
// 2026-08-14 with an argument that generalises: if that explanation were needed,
// EVERY editable context block would need one, and none of the others carry it.
// He was then offered two homes for the text (the usage guide, or a dialog after
// saving) and answered 「先不用」 ⇒ removed, and nothing put in its place.
//   * Of the three, only the retention number survives, and it moved to the
//     surface that USES it: the history list's own note (`note:` below), which
//     overrides a default sentence that says three and would be false here.
//   * The other two are simply not stated any more. That is the decision, not an
//     oversight — do not "restore" them, here or above the card.
//
// 🔴 THE FAILURE THIS SURFACE RISKS IS SILENT. A broken boot sequence means the
// agent never attaches to SSE, so it never comes online, so there is NOBODY
// ONLINE TO FIX IT. So saving goes through a confirmation that STATES that
// consequence.
//
// ⚠️ 還原出廠版 used to ALSO stand here as a top-level button, on the argument
// that a recovery path may not have prerequisites. The owner OVERRODE that on
// 2026-08-14 (card rc-f1950f4d286e, option 2: "完全照 insight") with the cost
// spelled out on the card — the restore now lives only inside edit mode, in the
// history list's 初始版本 row, exactly like every other editable document.
// Do not "restore" it here; that decision was made with the trade-off in view.
//
// 🔴 SAVING REPLACES THE WHOLE DOCUMENT, and this page says so on screen
// (`replaceNote`). The editor here used to be per-section — paste one block,
// apply it, leave the rest alone — and the wire was a whole-document replace
// underneath it either way. With one editor over the whole text, the thing that
// was implicit in the section rows has to be stated, because the failure it
// prevents (pasting one proposed block over a 45,000-character document and
// saving the rest away) is silent and unrecoverable except through history.
//
// 🔴 The claude and codex boot sequences are TWO DIFFERENT DOCUMENTS — their
// third step means opposite things (one attaches `ocagent listen` itself, the
// other must NOT and hands that to the sidecar). So each opens its own page
// from its own list row; there is no "apply this text to both runtimes"
// affordance and the two are never rendered side by side, because a
// side-by-side invites exactly the copy this page exists to prevent.

import { useI18n } from "../i18n";
import type { BootDocKind } from "../types";
import { useBootDoc } from "../hooks/useBootDoc";
import { DocCard } from "./DocCard";
import { type Crumb } from "./Breadcrumbs";
import { BOOT_DOC_HISTORY_KEPT, runeLength } from "../api/docCap";
import "./settings.css";

export function BootDocPage({
  kind,
  docKey,
  title,
  historyTitle,
  crumbs,
  collapsible,
}: {
  kind: BootDocKind;
  /** "global" for system_interaction; the RUNTIME ("claude" / "codex") for
   * boot_sequence. Required, never defaulted — see the header. */
  docKey: string;
  title: string;
  /** Names the document inside its own history list. This page carries exactly
   * one versioned document, but the list is the same component every editable
   * document shares, and 「版本紀錄」 alone cannot say which runtime it holds. */
  historyTitle: string;
  crumbs: Crumb[];
  /** Start closed behind the heading. 🔴 NO CALLER PASSES THIS (T-bac4): the
   * page that stacked two of these is gone — 啟動程序 is an index of two rows
   * now, one document per page. This line used to say it WAS passed by that
   * page, in the present tense. See DocCard's `collapsible` for why the prop
   * outlived its only caller and what has to happen together if it is retired. */
  collapsible?: boolean;
}) {
  const { t, msg } = useI18n();
  const { doc, error, refetch, save, reset } = useBootDoc(kind, docKey);

  return (
    <DocCard
      title={title}
      crumbs={crumbs}
      collapsible={collapsible}
      doc={doc}
      onSave={save}
      onReset={reset}
      // The STORED size; DocCard follows the draft once the editor is open.
      // `doc === null` passes none rather than "0 / 0", which would read as a
      // real budget of zero.
      usage={doc ? { size: runeLength(doc.text), cap: doc.capChars } : undefined}
      replaceNote={t.settings.docReplaceNote}
      // A no-op save would flip the document out of 預設 for ever, and these
      // three are the documents where "is this still the factory version" is
      // the question people ask about them.
      requireDirty
      confirmSave={{
        // The boot sequences get the sentence about the silent failure; the
        // system-interaction block does not, because it is not true of it — an
        // agent with a mangled system block still boots. A warning that is
        // false for the document on screen is worse than none: it teaches the
        // reader to dismiss the real one.
        body:
          kind === "boot_sequence"
            ? t.settings.bootDocSaveConfirmBoot
            : kind === "offboard"
              ? t.settings.bootDocSaveConfirmOffboard
              : t.settings.bootDocSaveConfirmSystem,
        confirmLabel: t.settings.bootDocSaveConfirmAction,
      }}
      // No explanatory notes block above the card. There used to be three
      // bullets here (what a save affects, how many revisions are kept, what
      // the cap does). The owner asked for them out on 2026-08-14 with an
      // argument that generalises: if that explanation were needed, EVERY
      // editable context block would need it — and none of the others carry
      // one. So it was not this document being special, it was noise.
      errorNote={error ? <div className="set-error">{t.settings.loadError}</div> : null}
      history={{
        kind,
        docKey,
        title: historyTitle,
        // The list's default note says the cockpit keeps the last THREE — true
        // everywhere else, false here. Overriding it is not decoration: an
        // owner who reads "3" under a list of ten will assume something is
        // broken, and one who reads "10" without "counted in saves" will assume
        // a run of small edits lost nothing.
        note: msg.bootDocNoteHistory(BOOT_DOC_HISTORY_KEPT),
        // The live document under its WIRE field name: the modal diffs a
        // revision against what the server currently stores.
        currentContent: doc ? { text: doc.text } : undefined,
        onRestored: refetch,
      }}
    />
  );
}
