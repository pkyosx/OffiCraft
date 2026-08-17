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

// T-76cd — a document far longer than one screen. The mobile report was a long
// .md whose panel lost its header and its last line; a story whose body fits
// the viewport cannot tell a scrolling overlay from a stuck one, so the
// scroll-to-the-end guard needs its own fixture. The last line carries a
// sentinel the guard scrolls to by name rather than by pixel arithmetic.
const LONG_MD = [
  "# 產物顯示架構設計（長文件）",
  "",
  ...Array.from({ length: 120 }, (_, i) => [
    `## 段落 ${i + 1}`,
    `這是第 ${i + 1} 段內容，用來把文件撐得比任何一個手機螢幕都高。`.repeat(3),
    "",
  ]).flat(),
  "最後一行 LAST_LINE_T76CD",
].join("\n");

const LONG_DATA_URL =
  "data:text/markdown;charset=utf-8," + encodeURIComponent(LONG_MD);

export function MarkdownPreviewLongStory() {
  return (
    <I18nProvider>
      <MarkdownPreviewOverlay
        title="架構設計-長文件.md"
        url={LONG_DATA_URL}
        onClose={() => {}}
      />
    </I18nProvider>
  );
}
