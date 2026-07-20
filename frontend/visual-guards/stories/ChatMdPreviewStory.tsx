// CT story (T-7bc2): the chat message's .md attachment preview — real-browser
// layout/keyboard the jsdom suite can't see. Mounts the REAL AttachmentStrip +
// MarkdownPreviewOverlay wired EXACTLY the way ChatArea.tsx wires them (same
// classnames, same onPreviewMarkdown knob) — not the whole ChatArea component,
// which would need a full API mock to render (see ChatMessagesStory's own
// comment for the same reasoning: real classnames, no function-prop mock).
// A mutant in AttachmentStrip's shared onPreviewMarkdown wiring reddens this
// guard exactly as it would in the real chat bubble.
import { useState } from "react";
import { AttachmentStrip } from "../../src/components/AttachmentStrip";
import { MarkdownPreviewOverlay } from "../../src/components/MarkdownPreviewOverlay";
import { I18nProvider } from "../../src/i18n";
import type { ChatAttachmentView } from "../../src/api/adapter";
import "../../src/components/office.css";

const MD = ["# design-proposal.md", "", "## 目標", "把 .md 預覽接到聊天室的附件上。"].join(
  "\n",
);
const MD_DATA_URL = "data:text/markdown;charset=utf-8," + encodeURIComponent(MD);

export function ChatMdPreviewStory() {
  const [mdPreview, setMdPreview] = useState<{ title: string; url: string } | null>(
    null,
  );
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
                onPreviewMarkdown={(att) =>
                  setMdPreview({ title: att.filename || "", url: att.url })
                }
              />
            </div>
          </div>
        </div>
      </div>
      {mdPreview && (
        <MarkdownPreviewOverlay
          title={mdPreview.title}
          url={mdPreview.url}
          onClose={() => setMdPreview(null)}
        />
      )}
    </I18nProvider>
  );
}
