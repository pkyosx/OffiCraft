// CT story for the T-a1c4 .md preview overlay. A data: URL is used as the blob
// source so the overlay's fetch resolves in the real browser without a server
// (authedAttachmentUrl only token-stamps paths starting with "/", so a data:
// URL passes through untouched).
import { I18nProvider } from "../../src/i18n";
import { MarkdownPreviewOverlay } from "../../src/components/MarkdownPreviewOverlay";

const MD = [
  "# 產物顯示架構設計",
  "",
  "## 目標",
  "把聊天附件的顯示邏輯抽成共用元件 `AttachmentStrip`。",
  "",
  "- **AttachmentStrip** — 檔名 chip / 縮圖的唯一 renderer",
  "- **Lightbox** — 圖片全螢幕預覽 overlay",
  "",
  "> 主要互動是線上預覽，不是下載。",
].join("\n");

const DATA_URL = "data:text/markdown;charset=utf-8," + encodeURIComponent(MD);

export function MarkdownPreviewStory() {
  return (
    <I18nProvider>
      <MarkdownPreviewOverlay title="架構設計.md" url={DATA_URL} onClose={() => {}} />
    </I18nProvider>
  );
}
