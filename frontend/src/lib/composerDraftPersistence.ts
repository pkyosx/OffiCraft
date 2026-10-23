import type { PendingAttachment } from "../hooks/useAttachmentStaging";

const STORAGE_PREFIX = "oc.composerDraft.v1.";
const DATABASE_NAME = "officraft-composer-drafts";
const STORE_NAME = "drafts";

export interface ComposerDraftMetadata {
  text: string;
  attachmentKeys: string[];
  replyTo?: string;
  replyExcerpt?: string;
}

interface MetadataStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

export interface ComposerDraftAttachmentStore {
  load(key: string): Promise<PendingAttachment[]>;
  save(key: string, attachments: PendingAttachment[]): Promise<void>;
  remove(key: string): Promise<void>;
}

function requestValue<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error("IndexedDB request failed"));
  });
}

function transactionDone(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onerror = () => reject(transaction.error ?? new Error("IndexedDB transaction failed"));
    transaction.onabort = () => reject(transaction.error ?? new Error("IndexedDB transaction aborted"));
  });
}

function createIndexedDbAttachmentStore(): ComposerDraftAttachmentStore {
  let database: Promise<IDBDatabase> | undefined;

  function open(): Promise<IDBDatabase> {
    if (database) return database;
    database = new Promise((resolve, reject) => {
      if (typeof indexedDB === "undefined") {
        reject(new Error("IndexedDB is unavailable"));
        return;
      }
      const request = indexedDB.open(DATABASE_NAME, 1);
      request.onupgradeneeded = () => {
        if (!request.result.objectStoreNames.contains(STORE_NAME)) {
          request.result.createObjectStore(STORE_NAME, { keyPath: "key" });
        }
      };
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error ?? new Error("IndexedDB open failed"));
    }).catch((error) => {
      database = undefined;
      throw error;
    });
    return database;
  }

  return {
    async load(key) {
      const db = await open();
      const transaction = db.transaction(STORE_NAME, "readonly");
      const done = transactionDone(transaction);
      const record = await requestValue<{ key: string; attachments: PendingAttachment[] } | undefined>(
        transaction.objectStore(STORE_NAME).get(key),
      );
      await done;
      return record?.attachments ?? [];
    },
    async save(key, attachments) {
      const db = await open();
      const transaction = db.transaction(STORE_NAME, "readwrite");
      const done = transactionDone(transaction);
      transaction.objectStore(STORE_NAME).put({ key, attachments });
      await done;
    },
    async remove(key) {
      const db = await open();
      const transaction = db.transaction(STORE_NAME, "readwrite");
      const done = transactionDone(transaction);
      transaction.objectStore(STORE_NAME).delete(key);
      await done;
    },
  };
}

function browserMetadataStorage(): MetadataStorage | null {
  try {
    return typeof window === "undefined" ? null : window.localStorage;
  } catch {
    return null;
  }
}

function storageKey(key: string): string {
  return `${STORAGE_PREFIX}${encodeURIComponent(key)}`;
}

function emptyMetadata(): ComposerDraftMetadata {
  return { text: "", attachmentKeys: [] };
}

function hasContent(metadata: ComposerDraftMetadata): boolean {
  return (
    metadata.text.length > 0 ||
    metadata.attachmentKeys.length > 0 ||
    Boolean(metadata.replyTo)
  );
}

function decodeMetadata(raw: string | null): ComposerDraftMetadata | undefined {
  if (!raw) return undefined;
  try {
    const value = JSON.parse(raw) as Partial<ComposerDraftMetadata>;
    if (typeof value.text !== "string") return undefined;
    return {
      text: value.text,
      attachmentKeys: Array.isArray(value.attachmentKeys)
        ? value.attachmentKeys.filter((item): item is string => typeof item === "string")
        : [],
      ...(typeof value.replyTo === "string" && value.replyTo
        ? { replyTo: value.replyTo }
        : {}),
      ...(typeof value.replyExcerpt === "string" && value.replyExcerpt
        ? { replyExcerpt: value.replyExcerpt }
        : {}),
    };
  } catch {
    return undefined;
  }
}

export function createComposerDraftPersistence(
  getStorage: () => MetadataStorage | null,
  attachments: ComposerDraftAttachmentStore,
) {
  const writes = new Map<string, Promise<void>>();

  function read(key: string): ComposerDraftMetadata | undefined {
    try {
      return decodeMetadata(getStorage()?.getItem(storageKey(key)) ?? null);
    } catch {
      return undefined;
    }
  }

  function write(key: string, metadata: ComposerDraftMetadata): void {
    const storage = getStorage();
    if (!storage) return;
    const target = storageKey(key);
    try {
      if (!hasContent(metadata)) {
        storage.removeItem(target);
        return;
      }
      storage.setItem(target, JSON.stringify(metadata));
    } catch {
      try {
        storage.removeItem(target);
      } catch {
        // A blocked or full store only disables this draft's persistence.
      }
    }
  }

  function enqueue(key: string, operation: () => Promise<void>): Promise<void> {
    const previous = writes.get(key) ?? Promise.resolve();
    const next = previous.catch(() => {}).then(operation).catch(() => {});
    writes.set(key, next);
    void next.finally(() => {
      if (writes.get(key) === next) writes.delete(key);
    });
    return next;
  }

  return {
    read,
    saveText(
      key: string,
      text: string,
      replyTo?: string,
      replyExcerpt?: string,
    ): void {
      const current = read(key) ?? emptyMetadata();
      write(key, {
        ...current,
        text,
        replyTo,
        replyExcerpt: replyTo ? replyExcerpt : undefined,
      });
    },
    saveAttachments(key: string, value: PendingAttachment[]): Promise<void> {
      return enqueue(key, async () => {
        try {
          if (value.length > 0) await attachments.save(key, value);
          else await attachments.remove(key);
          write(key, {
            ...(read(key) ?? emptyMetadata()),
            attachmentKeys: value.map((item) => item.key),
          });
        } catch {
          // Keep the last committed metadata; an attachment quota error must
          // not interrupt the active composer or corrupt another draft.
        }
      });
    },
    async loadAttachments(key: string): Promise<PendingAttachment[]> {
      const metadata = read(key);
      if (!metadata?.attachmentKeys.length) return [];
      try {
        const saved = await attachments.load(key);
        const keys = new Set(metadata.attachmentKeys);
        return saved.filter((item) => keys.has(item.key));
      } catch {
        return [];
      }
    },
    clear(key: string): Promise<void> {
      return enqueue(key, async () => {
        try {
          getStorage()?.removeItem(storageKey(key));
        } catch {
          // Continue removing attachment data even if Web Storage is blocked.
        }
        try {
          await attachments.remove(key);
        } catch {
          // A leftover IndexedDB row has no metadata pointer and cannot restore.
        }
      });
    },
    async flush(): Promise<void> {
      while (writes.size > 0) await Promise.all([...writes.values()]);
    },
  };
}

export const composerDraftPersistence = createComposerDraftPersistence(
  browserMetadataStorage,
  createIndexedDbAttachmentStore(),
);

export const chatDraftStorageKey = (peerId: string) => `chat:${peerId}`;
export const replyCardDraftStorageKey = (cardId: string) => `reply-card:${cardId}`;
export const taskMessageDraftStorageKey = (taskId: string) => `task-message:${taskId}`;
