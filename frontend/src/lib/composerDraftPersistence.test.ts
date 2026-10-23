import { describe, expect, it } from "vitest";
import type { PendingAttachment } from "../hooks/useAttachmentStaging";
import { createComposerDraftPersistence } from "./composerDraftPersistence";

function createMemoryStores() {
  const metadata = new Map<string, string>();
  const rows = new Map<string, PendingAttachment[]>();
  const saves: string[] = [];
  const storage = {
    getItem: (key: string) => metadata.get(key) ?? null,
    setItem: (key: string, value: string) => metadata.set(key, value),
    removeItem: (key: string) => metadata.delete(key),
  };
  const attachments = {
    load: async (key: string): Promise<PendingAttachment[]> => rows.get(key) ?? [],
    save: async (key: string, value: PendingAttachment[]) => {
      saves.push(key);
      rows.set(key, value);
    },
    remove: async (key: string) => {
      rows.delete(key);
    },
  };
  return {
    metadata,
    rows,
    saves,
    storage,
    attachments,
    persistence: createComposerDraftPersistence(() => storage, attachments),
  };
}

describe("composer draft persistence", () => {
  it("keeps attachment bytes out of metadata and restores only referenced files", async () => {
    const { metadata, persistence, saves } = createMemoryStores();
    const key = "reply-card:c-1";
    const attachment: PendingAttachment = {
      key: "pa-1",
      dataUri: `data:image/png;base64,${"A".repeat(4 * 1024 * 1024)}`,
      filename: "screen.png",
      size: 3 * 1024 * 1024,
      isImage: true,
    };

    persistence.saveText(key, "答覆內容");
    await persistence.saveAttachments(key, [attachment]);

    const stored = metadata.get("oc.composerDraft.v1.reply-card%3Ac-1");
    expect(stored).toBeTruthy();
    expect(stored).not.toContain(attachment.dataUri);
    expect(JSON.parse(stored!)).toEqual({
      text: "答覆內容",
      attachmentKeys: ["pa-1"],
    });
    persistence.saveText(key, "更新文字");
    expect(saves).toEqual([key]);
    expect(JSON.parse(metadata.get("oc.composerDraft.v1.reply-card%3Ac-1")!).text).toBe("更新文字");
    await expect(persistence.loadAttachments(key)).resolves.toEqual([attachment]);
  });

  it("removes a draft and its files after a successful send", async () => {
    const { metadata, rows, persistence } = createMemoryStores();
    const key = "task-message:T-1";
    const attachment: PendingAttachment = {
      key: "pa-2",
      dataUri: "data:text/plain;base64,SGk=",
      filename: "note.txt",
      size: 2,
      isImage: false,
    };

    persistence.saveText(key, "已送出的內容");
    await persistence.saveAttachments(key, [attachment]);
    await persistence.clear(key);

    expect(persistence.read(key)).toBeUndefined();
    expect(metadata.size).toBe(0);
    expect(rows.has(key)).toBe(false);
  });

  it("waits for submissions before flushing draft writes for reload", async () => {
    const { persistence } = createMemoryStores();
    const finishSubmission = persistence.beginSubmission();
    let flushed = false;
    const flush = persistence.flush().then(() => {
      flushed = true;
    });

    await Promise.resolve();
    expect(flushed).toBe(false);

    finishSubmission();
    await flush;
    expect(flushed).toBe(true);
  });

  it("does not restore cleared files when IndexedDB removal fails", async () => {
    const { metadata, rows, persistence, attachments } = createMemoryStores();
    const key = "chat:m-1";
    const attachment: PendingAttachment = {
      key: "sent-file",
      dataUri: "data:text/plain;base64,SGk=",
      filename: "sent.txt",
      size: 2,
      isImage: false,
    };

    persistence.saveText(key, "sent message");
    await persistence.saveAttachments(key, [attachment]);
    attachments.remove = async () => {
      throw new Error("IndexedDB delete failed");
    };
    persistence.saveText(key, "");
    await persistence.saveAttachments(key, []);

    expect(persistence.read(key)).toBeUndefined();
    expect(metadata.size).toBe(0);
    expect(rows.get(key)).toEqual([attachment]);
    await expect(persistence.loadAttachments(key)).resolves.toEqual([]);
  });

  it("isolates a metadata storage refusal to the affected draft", async () => {
    const metadata = new Map<string, string>();
    let rejectWritesFor: string | null = null;
    const storage = {
      getItem: (key: string) => metadata.get(key) ?? null,
      setItem: (key: string, value: string) => {
        if (key === rejectWritesFor) throw new Error("quota exceeded");
        metadata.set(key, value);
      },
      removeItem: (key: string) => metadata.delete(key),
    };
    const attachments = {
      load: async () => [],
      save: async () => {},
      remove: async () => {},
    };
    const persistence = createComposerDraftPersistence(() => storage, attachments);
    persistence.saveText("task-message:T-2", "保留的草稿");

    rejectWritesFor = "oc.composerDraft.v1.task-message%3AT-1";
    persistence.saveText("task-message:T-1", "無法保存的草稿");

    expect(persistence.read("task-message:T-2")?.text).toBe("保留的草稿");
    expect(persistence.read("task-message:T-1")).toBeUndefined();
  });

  it("keeps other drafts intact when attachment storage rejects a write", async () => {
    const metadata = new Map<string, string>();
    const rows = new Map<string, PendingAttachment[]>();
    const storage = {
      getItem: (key: string) => metadata.get(key) ?? null,
      setItem: (key: string, value: string) => metadata.set(key, value),
      removeItem: (key: string) => metadata.delete(key),
    };
    const attachment: PendingAttachment = {
      key: "pa-fail",
      dataUri: "data:image/png;base64,AA==",
      filename: "failed.png",
      size: 1,
      isImage: true,
    };
    const unavailable = createComposerDraftPersistence(
      () => storage,
      {
        load: async () => [],
        save: async (key, value) => {
          if (key === "chat:m-1") throw new Error("quota exceeded");
          rows.set(key, value);
        },
        remove: async () => {},
      },
    );

    const keptAttachment: PendingAttachment = {
      key: "pa-kept",
      dataUri: "data:image/png;base64,AA==",
      filename: "kept.png",
      size: 1,
      isImage: true,
    };
    unavailable.saveText("chat:m-2", "另一筆草稿");
    await unavailable.saveAttachments("chat:m-2", [keptAttachment]);
    unavailable.saveText("chat:m-1", "仍可輸入與送出的文字");
    await expect(unavailable.saveAttachments("chat:m-1", [attachment])).resolves.toBeUndefined();

    expect(unavailable.read("chat:m-1")?.text).toBe("仍可輸入與送出的文字");
    expect(JSON.parse(metadata.get("oc.composerDraft.v1.chat%3Am-1")!).attachmentKeys).toEqual([]);
    expect(unavailable.read("chat:m-2")?.text).toBe("另一筆草稿");
    expect(rows.get("chat:m-2")).toEqual([keptAttachment]);
  });
});
