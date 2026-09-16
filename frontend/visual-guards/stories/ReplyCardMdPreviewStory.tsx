// CT story (T-7bc2): the 請示卡(question-side)附件的 .md 預覽 — real-browser
// layout the jsdom suite can't see. Mounts the REAL ReplyCardQuestionAttachments
// component (not a hand-copied approximation) inside the REAL .reply-card shell
// + replies.css, so a mutant in the real wiring can redden this guard. The .md
// attachment's blob is a data: URL — authedAttachmentUrl only token-stamps
// paths starting with "/", so a data: URL passes through untouched and the
// overlay's fetch resolves without a server (same trick as MarkdownPreviewStory).
import { I18nProvider } from "../../src/i18n";
import { ReplyCardQuestionAttachments } from "../../src/components/ReplyCardBody";
import type { ChatAttachmentView, ReplyCard } from "../../src/api/adapter";
import "../../src/components/replies.css";

const MD = [
  "# design-proposal.md",
  "",
  "## 目標",
  "把 .md 預覽接到請示卡的附件上。",
  "",
  "- **AttachmentStrip** — 唯一 renderer",
  "- **MarkdownPreviewOverlay** — 座艙內預覽",
].join("\n");

const MD_DATA_URL = "data:text/markdown;charset=utf-8," + encodeURIComponent(MD);

function att(over: Partial<ChatAttachmentView>): ChatAttachmentView {
  return {
    id: "att-1",
    url: MD_DATA_URL,
    filename: "design-proposal.md",
    mime: "text/markdown",
    isImage: false,
    ...over,
  };
}

function mkCard(attachments: ChatAttachmentView[]): ReplyCard {
  return {
    id: "rc-1",
    from: "mira",
    kind: "decision",
    summary: "這樣的排版可以嗎？附上設計文件給你參考。",
    body: "",
    options: [{ text: "可以", aiPick: true }, { text: "再調整", aiPick: false }],
    selectMode: "single",
    status: "waiting",
    attachments,
    createdTs: Date.now() / 1000 - 600,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
  };
}

export function ReplyCardMdPreviewStory() {
  const card = mkCard([
    att({ id: "att-md", filename: "design-proposal.md", mime: "text/markdown" }),
    att({
      id: "att-pdf",
      url: "data:application/pdf;base64,JVBERg==",
      filename: "report.pdf",
      mime: "application/pdf",
    }),
  ]);
  return (
    <I18nProvider>
      <div className="replies" style={{ padding: 16 }}>
        <article className="reply-card" data-testid="waiting-card">
          {/* ⚠️ `onOpenImage` is GONE (T-f014): every item — image thumbnail and
            * file chip alike — now opens the strip's OWN MarkdownPreviewOverlay,
            * and `ReplyCardQuestionAttachments` takes `card` and nothing else. The
            * caller-routed image lightbox it fed is unexpressable here, so passing
            * a handler would be describing a route that no longer exists. (The
            * still-live `onOpenImage` in the app belongs to a DIFFERENT component,
            * ComposerAttachmentPreview, and is untouched by this.) */}
          <ReplyCardQuestionAttachments card={card} />
        </article>
      </div>
    </I18nProvider>
  );
}
