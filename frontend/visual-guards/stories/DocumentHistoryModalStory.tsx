// CT story for the retained-revision reader (T-1f39) — the modal against the
// REAL sheets, with a page BEHIND it.
//
// Two facts jsdom cannot see are staged here:
//   ① the modal really OVERLAYS. jsdom has no stacking context and no hit
//      testing, so a panel that painted underneath the page — or a scrim that
//      let clicks through to the editor behind it — is invisible there.
//   ② the long document scrolls INSIDE the modal (its body, and the diff's own
//      horizontal scroller) and leaves the PAGE unscrollable. That is layout.
//
// The backdrop is deliberately given something to cover: a tall page with a
// marked element right where the panel lands.
import { useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { DocumentHistoryModal } from "../../src/components/DocumentHistoryModal";
import { contentSizes } from "../../src/api/docCap";

/** 300 chars, no space, no hyphen — the shape a pasted token/URL takes. */
const LONG_TOKEN =
  "sha256:" +
  "0123456789abcdef".repeat(18) +
  "/twin(desired_state/desired_machine_id/refocus_since)";

/** Long enough that the modal body MUST scroll at any sane viewport. */
const PARAGRAPHS = Array.from(
  { length: 40 },
  (_, i) => `${i + 1}. 這一行是版本內容的第 ${i + 1} 段，用來把 modal 的內文撐高。`
);

const BEFORE = ["# 學習經驗", "", ...PARAGRAPHS, "", LONG_TOKEN].join("\n");
const AFTER = [
  "# 學習經驗",
  "",
  ...PARAGRAPHS.map((line, i) => (i === 2 ? `${line}（已改寫）` : line)),
  "",
  LONG_TOKEN,
].join("\n");

/** The revision's own text — since T-1170 it reaches the modal as its own
 * prop (the version LIST carries only the row facts beside it). */
const VERSION_CONTENT = { text: BEFORE };

export function DocumentHistoryModalStory({
  open = true,
}: {
  /** The guard mounts it open; `false` exists so a click on the scrim can be
   * observed actually closing it rather than only calling a spy. */
  open?: boolean;
}) {
  const [showing, setShowing] = useState(open);
  return (
    <I18nProvider>
      {/* The page the modal has to cover — tall, and with a target sitting
        * exactly where the panel will land. */}
      <div style={{ padding: 16 }} data-surface="page">
        <div
          data-testid="page-behind"
          style={{
            height: 1200,
            background: "var(--color-surface-sunken, #222)",
          }}
        >
          編輯面
        </div>
      </div>
      {showing && (
        <DocumentHistoryModal
          kind="lessons"
          createdTs={1753776180}
          tombstoned={false}
          sizes={contentSizes(VERSION_CONTENT)}
          content={VERSION_CONTENT}
          actorLine="Mira（owner-1）"
          currentContent={{ text: AFTER }}
          onClose={() => setShowing(false)}
          onRestore={async () => {}}
        />
      )}
    </I18nProvider>
  );
}
