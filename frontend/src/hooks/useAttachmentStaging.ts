// hooks/useAttachmentStaging.ts — the ONE composer attachment-staging state
// machine, extracted from ChatArea so every reply surface (chat composer, the
// 等我回覆 reply cards, B3's inline chat card) stages uploads identically:
// same size/count caps, same paste/pick funnels, same preview shape.

import { useState } from "react";
import { useI18n } from "../i18n";

// Client-side size guards — mirror the backend (handlers): an image/*
// attachment is capped at 20 MB, any other file at 100 MB. We fail fast in the
// UI before uploading; the server re-checks authoritatively.
const CHAT_IMAGE_MAX_BYTES = 20 * 1024 * 1024;
const CHAT_FILE_MAX_BYTES = 100 * 1024 * 1024;
// Per-message ATTACHMENT COUNT cap — mirrors the backend's
// _CHAT_ATTACHMENTS_MAX_COUNT (a safety default, not a product decision). Over
// the cap → the extra files are refused with a visible notice; the ones that
// fit stay staged.
const CHAT_MAX_ATTACHMENTS = 10;

/** `accept` for the file picker: images plus common office/doc/text/archive
 * types (an allow-anything wildcard is avoided — an explicit list is
 * friendlier on iOS, but we keep it broad). */
export const ATTACH_ACCEPT =
  "image/*,.pdf,.txt,.log,.csv,.json,.md,.zip,.doc,.docx,.xls,.xlsx,.ppt,.pptx";

/** ONE staged attachment held in a composer until the message is sent (or
 * cleared/removed). The clipboard-paste, attach-button and drag-drop paths all
 * funnel into this ONE shape; several may be staged at once (files + images
 * mixed) and are sent together on the SAME message. `dataUri` is a
 * `data:<mime>;base64,…` string (what FileReader.readAsDataURL yields), `size`
 * is the raw decoded byte estimate, `key` is a client-side list identity (for
 * React keys + per-item removal — duplicate filenames are legal). */
export interface PendingAttachment {
  key: string;
  dataUri: string;
  filename: string;
  mime: string;
  size: number;
  isImage: boolean;
}

// Monotonic client-side key mint for staged attachments.
let pendingAttachmentSeq = 0;

/** Estimate raw decoded byte size from a data-URI's base64 body. */
function estimateDataUriBytes(dataUri: string): number {
  const b64 = dataUri.split(",", 2)[1] ?? "";
  const padding = b64.endsWith("==") ? 2 : b64.endsWith("=") ? 1 : 0;
  return Math.floor((b64.length * 3) / 4) - padding;
}

/** Human-readable size for a staged file chip (e.g. "12 KB", "3.4 MB"). */
export function formatAttachmentSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export interface AttachmentStaging {
  pendingAttachments: PendingAttachment[];
  /** Transient rejection reason (too large / too many); null when none. */
  attachError: string | null;
  /** The ONE multi-file funnel: paste, picker and drag-drop all go through
   * here, one FileReader per file. */
  stageFiles: (files: File[]) => void;
  /** Paste handler: stage EVERY image/* item on the clipboard (a multi-image
   * paste stages them all). A paste with no image falls through untouched. */
  onPaste: (e: React.ClipboardEvent<HTMLTextAreaElement>) => void;
  /** Hidden-file-input onChange: stage every selected file, then clear the
   * input's value so picking the SAME file again still fires onChange. */
  onPickFile: (e: React.ChangeEvent<HTMLInputElement>) => void;
  removeAttachment: (key: string) => void;
  clearAttachments: () => void;
  /** Send-failure restore: put a snapshot back UNLESS the user already staged
   * new content while the send was in flight (never clobber fresh work). */
  restoreAttachments: (snapshot: PendingAttachment[]) => void;
}

export function useAttachmentStaging(): AttachmentStaging {
  const { t } = useI18n();
  const [pendingAttachments, setPendingAttachments] = useState<
    PendingAttachment[]
  >([]);
  const [attachError, setAttachError] = useState<string | null>(null);

  // Read a File → data-URI, size-check (image ≤ 20 MB, other ≤ 100 MB, mirroring
  // the backend), and APPEND it to the staged attachments. Over-size → surface
  // an error, skip the file; over the COUNT cap → surface an error, drop the
  // overflow (the ones that fit stay). The count guard lives INSIDE the
  // functional setState because FileReader completions land asynchronously —
  // checking a stale snapshot would race a multi-file batch past the cap.
  function stageFile(file: File) {
    const reader = new FileReader();
    reader.onload = () => {
      const dataUri = typeof reader.result === "string" ? reader.result : "";
      if (!dataUri) return;
      const mime = file.type || "application/octet-stream";
      const isImage = mime.startsWith("image/");
      const size = estimateDataUriBytes(dataUri);
      const limit = isImage ? CHAT_IMAGE_MAX_BYTES : CHAT_FILE_MAX_BYTES;
      if (size > limit) {
        setAttachError(
          isImage
            ? t.chat.imageTooLarge
            : t.chat.attachTooLarge(Math.round(limit / (1024 * 1024)))
        );
        return;
      }
      setPendingAttachments((prev) => {
        if (prev.length >= CHAT_MAX_ATTACHMENTS) {
          setAttachError(t.chat.attachTooMany(CHAT_MAX_ATTACHMENTS));
          return prev;
        }
        setAttachError(null);
        return [
          ...prev,
          {
            key: `pa-${++pendingAttachmentSeq}`,
            dataUri,
            // A pasted screenshot has no filename — leave it empty and let the
            // backend default it; a picked file keeps its real name.
            filename: file.name || "",
            mime,
            size,
            isImage,
          },
        ];
      });
    };
    reader.readAsDataURL(file);
  }

  function stageFiles(files: File[]) {
    for (const file of files) stageFile(file);
  }

  function onPaste(e: React.ClipboardEvent<HTMLTextAreaElement>) {
    const files = Array.from(e.clipboardData.items)
      .filter((it) => it.type.startsWith("image/"))
      .map((it) => it.getAsFile())
      .filter((f): f is File => f !== null);
    if (files.length === 0) return; // no image → default text paste happens
    e.preventDefault();
    stageFiles(files);
  }

  function onPickFile(e: React.ChangeEvent<HTMLInputElement>) {
    const files = Array.from(e.target.files ?? []);
    e.target.value = "";
    if (files.length > 0) stageFiles(files);
  }

  function removeAttachment(key: string) {
    setPendingAttachments((prev) => prev.filter((a) => a.key !== key));
    setAttachError(null);
  }

  function clearAttachments() {
    setPendingAttachments([]);
    setAttachError(null);
  }

  function restoreAttachments(snapshot: PendingAttachment[]) {
    if (snapshot.length === 0) return;
    setPendingAttachments((cur) => (cur.length > 0 ? cur : snapshot));
  }

  return {
    pendingAttachments,
    attachError,
    stageFiles,
    onPaste,
    onPickFile,
    removeAttachment,
    clearAttachments,
    restoreAttachments,
  };
}
