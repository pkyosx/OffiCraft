// components/DiffScreen.tsx — the compare BODY: two addresses in, one
// comparison on screen.
//
// T-59. It is the same screen it has always been — `DiffView` and only
// `DiffView`, the one the document version history uses — and this file adds no
// second renderer. What it owns is the three things around it that used to live
// inside the preview overlay, when a comparison was still an attachment whose
// bytes were a pointer pair:
//
//   * the READ (one `GET /api/diff`, not three fetches: the server resolves
//     both sides now, so there is no state here where one side is text and the
//     other is still null — that would draw every line of the survivor as a
//     deletion),
//   * the three honest empty states — still loading, would not load, and ONE
//     SIDE IS GONE, which is a fact about a different object and must not be
//     collapsed into "would not load",
//   * reading ONE SIDE on its own (owner 2026-09-03, c-944088dceab0: 「兩份應該
//     都要是連結」) — a VIEW of the pair already in hand, never a second read,
//     so the text shown is byte-for-byte the text the diff was drawn from.
//
// It is a BODY and not a shell on purpose: the same component is what the
// in-studio modal puts inside the preview overlay panel and what the standalone
// page puts on an empty page (DiffPage.tsx). One compare screen, two hosts —
// the alternative was a second copy that would start to drift on the owner's
// six 2026-07-31 diff rulings within a round.

import { useEffect, useRef, useState } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import {
  DIFF_SIDE_AT_CURRENT,
  DIFF_SIDE_AT_SEED,
  parseDiffSideAddress,
  type DiffParams,
} from "../lib/diffLink";
import type { DiffPairView, DiffSideView } from "../types";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { DiffView, type DiffMode } from "./DiffView";
import "./diff-screen.css";

export function DiffScreen({ params }: { params: DiffParams }) {
  const { t } = useI18n();
  /** The pair, and WHEN it was read. The read time rides along because a
   * `current` side is a LIVE pointer: the heading has to say the content was
   * read at a moment that has since passed, and a stamp taken at render time
   * would keep quietly moving while the reader looks at it. */
  const [pair, setPair] = useState<{ sides: DiffPairView; readAt: string } | null>(null);
  const [failed, setFailed] = useState(false);
  /** Which single side is being read on its own, or null while the comparison
   * itself is on screen. */
  const [side, setSide] = useState<"before" | "after" | null>(null);
  /** 單欄 / 兩欄對照, held HERE rather than inside DiffView, because opening one
   * side unmounts it — see the `mode` prop's note in DiffView. Coming back from
   * a side must land the reader in the layout they left. */
  const [mode, setMode] = useState<DiffMode>("unified");

  /* Esc offers the SAME way out the 「回到比較」 button does: one side open means
   * Esc goes BACK to the comparison, not out of whatever is hosting this screen
   * — the reader got here from INSIDE the comparison, and closing would make
   * them re-open the link and find their layout again. The layer is nested
   * inside the host's own (a modal's), so the innermost registration wins
   * without either side knowing about the other; on the standalone page there
   * is no outer layer and Esc simply has nothing left to do.
   *
   * The ref is on the SIDE pane, not on this component's root: it only holds a
   * layer while a side is open, and layering is read off DOM containment. */
  const sideRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(() => setSide(null), sideRef, side !== null);

  const { before, after, labelBefore, labelAfter, sig } = params;
  useEffect(() => {
    let alive = true;
    setPair(null);
    setFailed(false);
    setSide(null);
    api
      .getDiff({ before, after, labelBefore, labelAfter, sig })
      .then((got) => {
        if (!alive) return;
        // The reader is told ONE sentence for a side that resolved to nothing —
        // "a pruned revision" and "a reclaimed blob" are the same fact to them.
        // The server's reason still goes somewhere it can be asked for.
        for (const [which, s] of [["before", got.before], ["after", got.after]] as const) {
          if (s.gone) console.warn(`DiffScreen: ${which} side is gone: ${s.goneReason ?? ""}`);
        }
        setPair({
          sides: got,
          // Minutes, not seconds: the point is "this was a moment, and it has
          // passed", and a second-precision stamp would only invite someone to
          // trust it as an identity for the content.
          readAt: new Date().toLocaleString(undefined, {
            month: "2-digit",
            day: "2-digit",
            hour: "2-digit",
            minute: "2-digit",
          }),
        });
      })
      .catch((e: unknown) => {
        if (alive) setFailed(true);
        console.warn("DiffScreen: compare load failed", e);
      });
    return () => {
      alive = false;
    };
  }, [before, after, labelBefore, labelAfter, sig]);

  if (failed) {
    return <div className="diff-screen__status">{t.chat.mdPreview.error}</div>;
  }
  if (pair === null) {
    return <div className="diff-screen__status">{t.chat.mdPreview.loading}</div>;
  }
  // A side that resolved to nothing is SAID, not drawn: the other way — the
  // surviving side against "" — marks every one of its lines as added, which is
  // a confident wrong answer to "what changed".
  const { sides, readAt } = pair;

  /* THE HEADING OVER ONE COLUMN, and who writes it.
   *
   * A label the LINK carried wins — someone chose those words for this
   * comparison. With none, a DOCUMENT side names itself: 「目前存檔內容」/
   * 「初始版本」/「版本 #12」 in the READER's language, which is exactly why the
   * server sends no label for one (baking one in at mint time would impose one
   * language on every later reader). A blob side with no label falls back to
   * the diff's own two words — a blob id is not a heading.
   *
   * 「目前存檔內容」 additionally carries WHEN it was read, because that side is a
   * LIVE pointer: the same link opened next month compares against a different
   * document. The reader has to see that rather than infer it, and the stamp is
   * what keeps the sentence true after the screen is photographed or shared. */
  const headingFor = (side: DiffSideView, fallback: string): string => {
    if (side.label) return side.label;
    const doc = parseDiffSideAddress(side.address)?.doc;
    if (doc === undefined) return fallback;
    if (doc.at === DIFF_SIDE_AT_CURRENT) {
      return t.chat.mdPreview.diffSideLive(t.chat.mdPreview.diffSideCurrent, readAt);
    }
    if (doc.at === DIFF_SIDE_AT_SEED) return t.chat.mdPreview.diffSideSeed;
    return t.chat.mdPreview.diffSideRevision(doc.at);
  };

  if (sides.before.gone || sides.after.gone) {
    return (
      <div className="diff-screen__status" data-testid="diff-screen-side-gone">
        {t.chat.mdPreview.diffSideGone}
      </div>
    );
  }

  if (side !== null) {
    const shown = side === "before" ? sides.before : sides.after;
    return (
      /* ONE side, on its own. `pre` and not Markdown on purpose: what a side IS
       * here is the exact text that went into the comparison, and rendering it
       * would hide the very whitespace and markers the reader came to check. */
      <div className="diff-screen__side" data-testid="diff-screen-side" ref={sideRef}>
        <button
          type="button"
          className="diff-screen__side-back"
          data-testid="diff-screen-side-back"
          onClick={() => setSide(null)}
        >
          {t.chat.mdPreview.diffSideBack}
        </button>
        <div className="diff-screen__side-title" data-testid="diff-screen-side-title">
          {headingFor(shown, side === "before" ? t.diff.beforeLabel : t.diff.afterLabel)}
        </div>
        <pre className="diff-screen__text">{shown.text}</pre>
      </div>
    );
  }

  return (
    <DiffView
      before={sides.before.text}
      after={sides.after.text}
      beforeLabel={headingFor(sides.before, t.diff.beforeLabel)}
      afterLabel={headingFor(sides.after, t.diff.afterLabel)}
      onOpenSide={setSide}
      mode={mode}
      onModeChange={setMode}
      testId="diff-screen"
    />
  );
}
