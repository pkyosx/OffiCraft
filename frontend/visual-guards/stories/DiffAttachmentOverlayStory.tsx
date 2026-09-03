// CT story (T-59): the COMPARE attachment opened in the shared preview overlay.
//
// Mounts the real MarkdownPreviewOverlay with the real diff mime, so the
// component resolves the pointer pair and renders the real DiffView inside the
// overlay panel — the one geometry the jsdom suite cannot see. The pair itself
// arrives as a data: URL (authedAttachmentUrl leaves non-"/" URLs alone); the
// two SIDES are ordinary "/api/chat/attachment/…" reads, which the spec serves
// with page.route so the story stays a pure component mount.
import { MarkdownPreviewOverlay } from "../../src/components/MarkdownPreviewOverlay";
import { I18nProvider } from "../../src/i18n";
import "../../src/components/office.css";

const PAIR = JSON.stringify({
  before: { attachment_id: "att-0123456789ab", label: "改動前" },
  after: { attachment_id: "att-fedcba987654", label: "改動後" },
});

/** The second round's shape: both sides name a DOCUMENT and NEITHER carries a
 * label, so the columns get the reader's own default headings — which are much
 * longer than anything a caller writes by hand, and one of them carries a
 * timestamp. That is the input the geometry guard actually needs: "we added no
 * CSS" says nothing about a heading three times the width of the old one. */
//
// The live side is `global_context/current` and the other side stays a blob:
// CT mounts run against the MOCK adapter, whose retained-revision store starts
// empty (a revision only exists after a save) and which has no seed for most
// kinds — so a revision id or a `seed` here would resolve to "this side is
// gone" and the geometry would never be measured at all.
const DOC_PAIR = JSON.stringify({
  before: { attachment_id: "att-0123456789ab", label: "改動前" },
  after: { doc: { kind: "global_context", key: "global", at: "current", field: "text" } },
});

export function DiffAttachmentOverlayStory({
  variant = "blobs",
}: {
  variant?: "blobs" | "docs";
}) {
  const pair = variant === "docs" ? DOC_PAIR : PAIR;
  return (
    <I18nProvider>
      <MarkdownPreviewOverlay
        title="lineDiff.before.ts → lineDiff.after.ts"
        url={"data:application/json;charset=utf-8," + encodeURIComponent(pair)}
        attachmentId="att-aaaaaaaaaaaa"
        mime="application/vnd.officraft.diff"
        onClose={() => {}}
      />
    </I18nProvider>
  );
}
