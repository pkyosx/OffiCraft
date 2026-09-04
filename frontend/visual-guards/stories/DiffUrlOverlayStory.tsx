// CT story (T-59): a compare URL opened IN PLACE, as the studio's modal.
//
// Mounts the real MarkdownPreviewOverlay in its compare mode, so the real
// DiffScreen makes the real read and renders the real DiffView inside the
// overlay panel — the one geometry the jsdom suite cannot see.
//
// The READ is the only seam this story replaces (same shape as
// ResumeAnsweredCardStory): a CT mount runs against the mock adapter, whose
// compare fixture is a short, tidy pair — and the widest thing a comparison
// will ever hold is exactly what this guard exists to measure. So the sides are
// supplied here, through the adapter the component really calls, with a line
// far too wide for the panel and a one-word change that says which side is
// which.
import { MarkdownPreviewOverlay } from "../../src/components/MarkdownPreviewOverlay";
import { I18nProvider } from "../../src/i18n";
import { api } from "../../src/api";
import "../../src/components/office.css";

const BEFORE = ["alpha", "bravo", "charlie", "delta ".repeat(40)].join("\n");
const AFTER = ["alpha", "BRAVO", "charlie", "delta ".repeat(40)].join("\n");

const PARAMS = {
  before: "doc:global_context/global/seed/text",
  after: "doc:global_context/global/current/text",
};

export function DiffUrlOverlayStory({
  variant = "labelled",
}: {
  variant?: "labelled" | "reader-labels";
}) {
  const labelled = variant === "labelled";
  // Labels are ECHOED, exactly as the route answers them: a side the url gave
  // no heading comes back with none, and the READER writes 「初始版本」/「目前存檔
  // 內容（讀取於 …）」 from the address. That reader-written heading is much
  // longer than anything a caller types by hand and one of them carries a
  // timestamp — which is the input the second geometry guard actually needs:
  // "we added no CSS" says nothing about a heading three times the width of the
  // old one.
  api.getDiff = async (params) => ({
    before: { address: params.before, text: BEFORE, label: params.labelBefore, gone: false },
    after: { address: params.after, text: AFTER, label: params.labelAfter, gone: false },
  });

  return (
    <I18nProvider>
      <MarkdownPreviewOverlay
        title="逐行比對"
        diffParams={
          labelled
            ? { ...PARAMS, labelBefore: "改動前", labelAfter: "改動後" }
            : PARAMS
        }
        onClose={() => {}}
      />
    </I18nProvider>
  );
}
