// hooks/useAttachmentStaging.ts — the ONE composer attachment-staging state
// machine, extracted from ChatArea so every reply surface (chat composer, the
// 等我回覆 reply cards, B3's inline chat card) stages uploads identically:
// same size/count caps, same paste/pick funnels, same preview shape.

import {
  useCallback,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import { useI18n } from "../i18n";
import { attachmentMimeForName, pastedImageName } from "../lib/attachmentMime";
import { composerDraftPersistence } from "../lib/composerDraftPersistence";
import {
  getChatAttachError,
  getChatDraftAttachments,
  hydrateChatDraft,
  setChatAttachError,
  subscribeChatDraft,
  updateChatDraftAttachments,
} from "../lib/chatDraftStore";

// Client-side size guards — mirror the backend (handlers): an image/*
// attachment is capped at 20 MB, any other file at 100 MB. We fail fast in the
// UI before uploading; the server re-checks authoritatively.
const CHAT_IMAGE_MAX_BYTES = 20 * 1024 * 1024;
const CHAT_FILE_MAX_BYTES = 100 * 1024 * 1024;
// Per-message ATTACHMENT COUNT cap — mirrors the backend's
// _CHAT_ATTACHMENTS_MAX_COUNT (a safety default, not a product decision). Over
// the cap → the extra files are refused with a visible notice; the ones that
// fit stay staged.
export const CHAT_MAX_ATTACHMENTS = 10;

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
 * React keys + per-item removal — duplicate filenames are legal).
 *
 * ⚠️ THERE IS NO `target` FIELD ANY MORE, AND THAT IS THE POINT (T-48, R13-2).
 * A row used to be stamped with the room it was picked for, because every room's
 * rows shared one list and the composer had to filter. Rows now live IN the room
 * they were picked for — a peer's slice of `chatDraftStore` — so "whose file is
 * this?" is answered by where it is, not by a field somebody has to compare. */
export interface PendingAttachment {
  key: string;
  dataUri: string;
  filename: string;
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

/** WHOSE staging this is — REQUIRED and FIRST, because it names the SLOT the
 * files are written into, not a label they are compared against.
 *
 * 🔴 THIS HOOK HAS NO `await`, AND THAT IS WHY THE SLOT HAD TO BE DURABLE.
 * `stageFile` hands the file to `FileReader` and returns; the commit happens
 * later, inside `reader.onload`. Reading a 100 MB drop or a large pasted
 * screenshot takes SECONDS, and the surface that picked it can be gone by then
 * — a different conversation on screen, or no conversation at all because the
 * page was left. The measured results of putting that list in component state
 * were a file appearing in the wrong room's composer (R9-1), a file destroyed
 * by an unmount nobody was watching for (R10-4), and a file filed into a draft
 * that the composer then overwrote (R11-2).
 *
 * The commit now names its slot: `updateChatDraftAttachments(pickedFor, …)`.
 * It does not consult the screen, it does not consult React, and it works the
 * same whether a composer is mounted, mounted on another room, or gone.
 *
 * A caller mounted under a key that changes with the thing it stages for
 * (`TaskCard` under `key={task.id}`, `ReplyComposer` under `key={card.id}`)
 * passes this constant instead, and gets an EPHEMERAL slot that dies with the
 * mount — which is the honest description of those surfaces: a card torn down
 * mid-read has no room to come back to. Since R13-5 `ChatArea` is one of these
 * by mounting (`OfficePage` gives it `key={peerId}`), but NOT by staging: its
 * files outlive the visit on purpose, because the draft does. */
export const STAGING_TARGET_PER_MOUNT = "remounts-per-conversation";

export interface PersistentStagingTarget {
  kind: "persistent";
  draftKey: string;
}

export function persistentStagingTarget(draftKey: string): PersistentStagingTarget {
  return { kind: "persistent", draftKey };
}

export interface AttachmentStaging {
  /** The staged files for this slot. */
  pendingAttachments: PendingAttachment[];
  /** True while a persistent slot is restoring its saved files. */
  isRestoring: boolean;
  /** Transient rejection reason (too large / too many) raised in THIS slot;
   * null when none. */
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
  /** Clear this slot's staged files and its visible error. */
  clearAttachments: () => void;
}

/** Where one surface's staged rows live. Two implementations, chosen by the
 * `target` overload the caller picked: a peer's slice of `chatDraftStore`, or a
 * list that dies with the mount. */
interface StagingSlot {
  attachments: PendingAttachment[];
  isRestoring: boolean;
  error: string | null;
  update: (fn: (prev: PendingAttachment[]) => PendingAttachment[]) => void;
  setError: (message: string | null) => void;
}

const NO_UNSUBSCRIBE = () => {};
const NO_ROWS: PendingAttachment[] = [];

/** The DURABLE slot: a chat peer's own attachments, in the draft store. Reads
 * are a subscription, so a row written by a read that finished while nobody was
 * looking shows up the moment somebody is.
 *
 * `peerId === null` means the caller took the ephemeral slot instead. The hooks
 * still run — they must, the count cannot change over a mount — but they are
 * wired to nothing: no store key is subscribed and no snapshot is read, so this
 * slot is inert rather than quietly attached to a key nobody writes. */
function useDraftStoreSlot(peerId: string | null): StagingSlot {
  const [isRestoring, setIsRestoring] = useState(peerId !== null);
  useEffect(() => {
    if (peerId === null) {
      setIsRestoring(false);
      return;
    }
    let active = true;
    setIsRestoring(true);
    void hydrateChatDraft(peerId).finally(() => {
      if (active) setIsRestoring(false);
    });
    return () => {
      active = false;
    };
  }, [peerId]);
  const subscribe = useCallback(
    (cb: () => void): (() => void) =>
      peerId === null ? NO_UNSUBSCRIBE : subscribeChatDraft(peerId, cb),
    [peerId],
  );
  const attachments = useSyncExternalStore(
    subscribe,
    useCallback(
      () => (peerId === null ? NO_ROWS : getChatDraftAttachments(peerId)),
      [peerId],
    ),
  );
  const error = useSyncExternalStore(
    subscribe,
    useCallback(
      () => (peerId === null ? null : getChatAttachError(peerId)),
      [peerId],
    ),
  );
  return {
    attachments,
    isRestoring,
    error,
    update: (fn) => {
      if (peerId !== null) updateChatDraftAttachments(peerId, fn);
    },
    setError: (message) => {
      if (peerId !== null) setChatAttachError(peerId, message);
    },
  };
}

function usePersistentSlot(draftKey: string | null): StagingSlot {
  const [attachments, setAttachments] = useState<PendingAttachment[]>([]);
  const [isRestoring, setIsRestoring] = useState(draftKey !== null);
  const [error, setError] = useState<string | null>(null);
  const latest = useRef<{ key: string | null; attachments: PendingAttachment[] }>({
    key: draftKey,
    attachments: [],
  });
  if (latest.current.key !== draftKey) {
    latest.current = { key: draftKey, attachments: [] };
  }

  useEffect(() => {
    let active = true;
    latest.current = { key: draftKey, attachments: [] };
    setAttachments([]);
    setError(null);
    if (draftKey === null) {
      setIsRestoring(false);
      return () => {
        active = false;
      };
    }
    setIsRestoring(true);
    void composerDraftPersistence.loadAttachments(draftKey).then((restored) => {
      if (!active) return;
      const current = latest.current.key === draftKey
        ? latest.current.attachments
        : [];
      const byKey = new Map(restored.map((item) => [item.key, item]));
      for (const item of current) byKey.set(item.key, item);
      const next = [...byKey.values()];
      latest.current = { key: draftKey, attachments: next };
      setAttachments(next);
    }).finally(() => {
      if (active) setIsRestoring(false);
    });
    return () => {
      active = false;
    };
  }, [draftKey]);

  const update = useCallback(
    (fn: (prev: PendingAttachment[]) => PendingAttachment[]) => {
      if (draftKey === null || latest.current.key !== draftKey) return;
      const prev = latest.current.attachments;
      const next = fn(prev);
      if (next === prev) return;
      latest.current = { key: draftKey, attachments: next };
      setAttachments(next);
      void composerDraftPersistence.saveAttachments(draftKey, next);
    },
    [draftKey],
  );

  return {
    attachments: latest.current.key === draftKey ? attachments : NO_ROWS,
    isRestoring,
    error,
    update,
    setError,
  };
}

/** The EPHEMERAL slot: component state, gone on unmount. Correct for a surface
 * that is torn down together with the thing it stages for — a landing after its
 * unmount has no surface left to return to either. */
function useEphemeralSlot(): StagingSlot {
  const [attachments, setAttachments] = useState<PendingAttachment[]>([]);
  const [error, setError] = useState<string | null>(null);
  return {
    attachments,
    isRestoring: false,
    error,
    update: (fn) => setAttachments((prev) => fn(prev)),
    setError,
  };
}

/** 🔴 THE TYPE ASKS THE QUESTION BACK (T-48, R11-8, kept through R13-2).
 * `target: string` alone compiles for every caller and every mistake: pass a
 * task id where a peer id belongs and the files land in a room nobody will look
 * in. The two overloads put the choice back where the caller must answer it —
 *
 *   · a surface that is REMOUNTED with the thing it stages for says exactly
 *     that, by passing `STAGING_TARGET_PER_MOUNT`, and accepts that its staged
 *     files die with it;
 *   · a chat composer names the PEER, and its files are that peer's draft. */
export function useAttachmentStaging(
  target: typeof STAGING_TARGET_PER_MOUNT,
): AttachmentStaging;
export function useAttachmentStaging(target: PersistentStagingTarget): AttachmentStaging;
export function useAttachmentStaging(target: string): AttachmentStaging;
export function useAttachmentStaging(
  target: string | PersistentStagingTarget,
): AttachmentStaging {
  const { t } = useI18n();
  // Both slots are built every render and one is used. `target` does not change
  // over a mount's life at either kind of call site (the per-mount callers pass
  // a literal; `ChatArea` is mounted under `key={peerId}`), so the choice is a
  // constant and no hook is ever skipped.
  const persistentKey = typeof target === "object" ? target.draftKey : null;
  const perMount = typeof target === "string" && target === STAGING_TARGET_PER_MOUNT;
  const chatPeerId =
    typeof target === "string" && !perMount ? target : null;
  const ephemeral = useEphemeralSlot();
  const stored = useDraftStoreSlot(chatPeerId);
  const persistent = usePersistentSlot(persistentKey);
  const slot = persistentKey !== null
    ? persistent
    : perMount
      ? ephemeral
      : stored;

  // Read a File → data-URI, size-check (image ≤ 20 MB, other ≤ 100 MB, mirroring
  // the backend), and APPEND it to the staged attachments. Over-size → surface
  // an error, skip the file; over the COUNT cap → surface an error, drop the
  // overflow (the ones that fit stay). The count guard lives INSIDE the
  // functional update because FileReader completions land asynchronously —
  // checking a stale snapshot would race a multi-file batch past the cap.
  function stageFile(file: File) {
    // Captured at PICK time: this is the slot the owner was composing into when
    // they chose the file, and the commit below writes into THAT slot whatever
    // is on screen when the read finishes.
    const pickedInto = slot;
    const reader = new FileReader();
    reader.onload = () => {
      const dataUri = typeof reader.result === "string" ? reader.result : "";
      if (!dataUri) return;
      const filename = file.name || pastedImageName(file.type);
      const isImage = attachmentMimeForName(filename).startsWith("image/");
      const size = estimateDataUriBytes(dataUri);
      const limit = isImage ? CHAT_IMAGE_MAX_BYTES : CHAT_FILE_MAX_BYTES;
      if (size > limit) {
        pickedInto.setError(
          isImage
            ? t.chat.imageTooLarge
            : t.chat.attachTooLarge(Math.round(limit / (1024 * 1024))),
        );
        return;
      }
      const attachment: PendingAttachment = {
        key: `pa-${++pendingAttachmentSeq}`,
        filename,
        dataUri,
        size,
        isImage,
      };
      let refused = false;
      pickedInto.update((prev) => {
        if (prev.length >= CHAT_MAX_ATTACHMENTS) {
          refused = true;
          return prev;
        }
        return [...prev, attachment];
      });
      pickedInto.setError(
        refused ? t.chat.attachTooMany(CHAT_MAX_ATTACHMENTS) : null,
      );
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
    slot.update((prev) => prev.filter((a) => a.key !== key));
    slot.setError(null);
  }

  function clearAttachments() {
    slot.update((prev) => (prev.length === 0 ? prev : []));
    slot.setError(null);
  }

  return {
    pendingAttachments: slot.attachments,
    isRestoring: slot.isRestoring,
    attachError: slot.error,
    stageFiles,
    onPaste,
    onPickFile,
    removeAttachment,
    clearAttachments,
  };
}
