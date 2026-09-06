// components/DocUsage.tsx — the 「已用 / 上限」 readout for a capped document
// that is edited OUTSIDE `DocCard` (T-100).
//
// It exists because the two task-manual documents — 任務定義 (the SOP) and
// 學習經驗 — are edited by hand-rolled cards inside `TaskManualsPage`, not by
// `DocCard`, so they showed the owner NO budget at all. He met the cap by being
// refused after he had already written the thing.
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
//  2. NOTHING IS DRAWN WHEN `cap` IS NOT A REAL CAP. The four manual size/cap
//     fields are optional on the wire and the mapper floors them at 0, so a
//     server too old to send them would otherwise render 「1234 / 0」 — which
//     reads as "you are already over" on a document that is perfectly fine. A
//     missing budget must look missing, not look full.
//
// It is a READOUT ONLY. It does not gate saving: an over-cap write is still
// refused by the server and surfaced by each card's own save-error line
// (T-100 scope — DocCard's pre-block is deliberately NOT copied here).

import { useI18n } from "../i18n";
import { shownDocSize } from "../api/docCap";
// Owns its own style import rather than free-riding on whatever page happens to
// mount it — `.doc-card__usage` is declared in this sheet, and the one time a
// component drew another sheet's block without importing it, the styles vanished
// the day the last transitive importer changed (styleOwnership.test.ts).
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
