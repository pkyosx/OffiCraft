// components/DocUsage.tsx — the 「已用 / 上限」 readout for a capped document
// that is edited OUTSIDE `DocCard` (T-100).
//
// It exists because the task manual's 任務定義 (the SOP) is edited by a
// hand-rolled card inside `TaskManualsPage`, not by `DocCard`, so it showed the
// owner NO budget at all. He met the cap by being refused after he had already
// written the thing.
//
// Two decisions worth stating, because both are easy to undo by accident:
//
//  1. THE ARITHMETIC IS NOT HERE. It is `shownDocSize` in api/docCap.ts, which
//     `DocCard` also calls. That is deliberate: the part that can be silently
//     WRONG (counting the draft rather than the stored document, and adding back
//     a read-only head) now has one implementation, so this component and
//     DocCard cannot drift into two answers for the same document. The markup
//     stays duplicated on purpose — DocCard's readout sits inside a header this
//     component knows nothing about, and it also feeds its own over-cap notice.
//
//  2. NOTHING IS DRAWN WHEN `cap` IS NOT A REAL CAP. The mapper floors an
//     absent cap at 0 (reachable from a server predating the fields), so
//     without this gate such a server would render 「1234 / 0」 — which
//     reads as "you are already over" on a document that is perfectly fine. A
//     missing budget must look missing, not look full.
//
// It is a READOUT ONLY: it does not gate saving, and DocCard's pre-block —
// which shuts the save button and quotes both numbers before anything is sent —
// is deliberately NOT copied here (T-100 scope).
//
// 🔴 SO SAY PLAINLY WHAT AN OVER-CAP SAVE DOES ON THESE TWO CARDS TODAY, AND IT
// IS NOT GOOD (measured, not read off a comment — an earlier draft of this
// block claimed the server's own words reach the screen and that was false):
// both manual cards keep `saveError` as a BOOLEAN and put the server's message
// into `console.warn`, so the owner sees the generic 「儲存失敗，請稍後重試」.
// Retrying is exactly what will not work. This readout therefore lets him watch
// 「18001 / 18000」 go past — with no warning styling of any kind — and then
// hands him an instruction that cannot succeed.
//
// That is a REAL GAP and it is left open on purpose: closing it means changing
// what these cards do on failure, which is a behaviour change beyond what this
// ticket was asked for. It is recorded on T-100 for the ticket owner to file.
// Do not read this paragraph as "handled".

import { useI18n } from "../i18n";
import { shownDocSize } from "../api/docCap";
// Owns its own style import rather than free-riding on whatever page happens to
// mount it — `.doc-card__usage` is declared in this sheet, and the one time a
// component drew another sheet's block without importing it, the styles vanished
// the day the last transitive importer changed (styleOwnership.test.ts).
//
// ⚠️ NOTHING ENFORCES THIS LINE. `settings.css` is not in that test's
// OWNED_SHEETS list, and deleting this import was measured (T-100) to leave the
// whole suite — the browser guard included — green, because the sheet reaches
// the page another way today. It is here because the convention is right, not
// because anything would tell you if it went.
import "./settings.css";

interface DocUsageProps {
  /** Size of the STORED document in characters, as the SERVER measured it. */
  size: number;
  /** The cap in force FOR THIS DOCUMENT — per-document, never a shared
   * constant: the two manual documents are judged against separate caps and on
   * a live station they differ. `<= 0` means "not known", and nothing renders. */
  cap: number;
  /** The live draft while editing; `null` when not editing. */
  draft: string | null;
  /** The stored text the editor holds — how `shownDocSize` derives the part of
   * `size` the editor does NOT hold. Pass the same text the draft started from. */
  storedText: string;
  /** Test hook. Each host names its own document, so a failing assertion says
   * WHICH readout is wrong rather than "one of them". */
  testId: string;
}

export function DocUsage({
  size,
  cap,
  draft,
  storedText,
  testId,
}: DocUsageProps) {
  const { t } = useI18n();
  if (cap <= 0) return null;
  return (
    <span
      className="doc-card__usage"
      data-testid={testId}
      title={t.settings.docUsage}
    >
      {shownDocSize(size, storedText, draft)} / {cap}
    </span>
  );
}
