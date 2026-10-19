// CT story (T-7bc2): the chat message's .md attachment preview — real-browser
// layout/keyboard the jsdom suite can't see. Mounts the REAL AttachmentStrip
// with the SAME classnames ChatArea.tsx gives it — not the whole ChatArea
// component, which would need a full API mock to render (see ChatMessagesStory's
// own comment for the same reasoning: real classnames, no function-prop mock).
// A mutant in AttachmentStrip's chip/overlay wiring reddens this guard exactly
// as it would in the real chat bubble.
//
// ⚠️ The `onPreviewMarkdown` knob and the caller-owned MarkdownPreviewOverlay
// this story used to mount beside the strip are GONE (T-f014). The strip now
// OWNS its preview: it renders the overlay itself and no caller can route the
// preview anywhere else, so the prop is unexpressable and the second overlay
// could never open — it was the exact "a mounted overlay that has no way to be
// opened" shape AttachmentStrip's own header comment describes. What the guard
// clicks and what appears are now the same component's, which is the point.
import { AttachmentStrip } from "../../src/components/AttachmentStrip";
import { AttachmentModalHost } from "../../src/components/AttachmentModalHost";
import { Markdown } from "../../src/components/Markdown";
import { I18nProvider } from "../../src/i18n";
import type { ChatAttachmentView } from "../../src/api/adapter";
import "../../src/components/office.css";

const MD = ["# design-proposal.md", "", "## 目標", "把 .md 預覽接到聊天室的附件上。"].join(
  "\n",
);
const MD_DATA_URL = "data:text/markdown;charset=utf-8," + encodeURIComponent(MD);

export function ChatMdPreviewStory() {
  const atts: ChatAttachmentView[] = [
    {
      id: "att-md",
      url: MD_DATA_URL,
      filename: "design-proposal.md",
      mime: "text/markdown",
      isImage: false,
    },
    {
      id: "att-pdf",
      url: "data:application/pdf;base64,JVBERg==",
      filename: "report.pdf",
      mime: "application/pdf",
      isImage: false,
    },
  ];
  return (
    <I18nProvider>
      <div className="chat__body">
        <div className="chat__messages">
          <div className="chat__msg">
            <div className="chat__msg-bubble">
              <AttachmentStrip
                attachments={atts}
                className="chat__msg-attachments"
                itemClassName="chat__msg-attachment"
                imageClassName="chat__msg-image chat__msg-image--clickable"
              />
            </div>
          </div>
        </div>
      </div>
    </I18nProvider>
  );
}

const DOC_COMMENT_BODY = [
  "**文件註解**：[一份檔名相當長的設計提案文件-第三版.md](/api/chat/attachment/att-aaaaaaaaaaaa)",
  "",
  "1. 原文：",
  "   > 目標",
  "",
  "   註解：這段要再寫清楚",
].join("\n");

/** A 文件註解 message as the chat bubble renders its body, inside the studio's
 * attachment-link host. */
export function ChatAttachmentLinkStory() {
  return (
    <I18nProvider>
      <AttachmentModalHost>
        <div className="chat__body">
          <div className="chat__messages">
            <div className="chat__msg">
              <div className="chat__msg-bubble">
                <Markdown source={DOC_COMMENT_BODY} className="chat__msg-text doc-md" breaks />
              </div>
            </div>
          </div>
        </div>
      </AttachmentModalHost>
    </I18nProvider>
  );
}
