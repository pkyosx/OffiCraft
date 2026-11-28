// LorePage — what an open section draws from the server's numbers, the card,
// its actions, and the filters.
//
//   變灰. Each entry's `dropped` is the SERVER'S answer over the whole
//   target, so the fixture's TEXT LENGTHS ARE DELIBERATELY NOT A CLUE: a
//   dropped entry is one of the SHORTEST rows on screen. A local sum cannot
//   produce this answer.
//
//   訊息框的收件人. A message to a peer who is gone reaches nobody, so a card
//   whose author cannot take it has no box at all — but only once the roster
//   has answered: before that a live author looks gone too.
//
// Most specs open ONE section (Mira's) whose list request is stubbed: what the
// page does with the answer is under test here, not the mock's arithmetic.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { render, fireEvent, waitFor, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { __resetHashRouteForTests } from "../lib/hashRoute";
import { LorePage } from "./LorePage";
import { __resetMock, mockApi } from "../api/mock";
import { api } from "../api";
// The 可變動作面 specs need to build a REFUSAL, and the refusal has to be the
// same class the adapters throw — `runAction` branches on `status` and reads
// `serverMessage`, neither of which a plain Error carries.
import { ApiError } from "../api/errors";
import { codeForStatus } from "../api/errorCodes";
// The action-error copy is asserted by VALUE, not by "is non-empty": an error
// line saying the wrong sentence is a line that passed a non-empty check.
import { zh } from "../i18n/locales/zh";
import { formatAbsolute } from "../lib/dateFormat";
import { clearLocale, useEnglishLocale } from "../test/effortOptions";
import { LORE_DIR_COLLAPSED_KEY, openEveryLoreDirGroup } from "../test/loreDirectory";
import { openFilters, openFiltersWhenReady } from "../test/tasksFilter";
import type {
  ChatAttachmentInput,
  LoreBudgetTargetView,
  LoreEntryPageView,
  LoreEntryView,
} from "../api/adapter";

function mkEntry(over: Partial<LoreEntryView> & { id: string }): LoreEntryView {
  return {
    seq: 1,
    scopeKind: "agent",
    scopeKey: "mira",
    taskTypeKey: "",
    scopeOptions: [],
    title: `標題 ${over.id}`,
    body: `內容 ${over.id}`,
    authorId: "mira",
    sourceTaskId: "",
    state: "active",
    retireReason: "",
    effectiveTs: 1788400000,
    createdTs: 1788400000,
    updatedTs: 1788400000,
    loreType: "other",
    ...over,
  };
}

/** One page answer. `entries.length < 30` ⇒ the page stops asking for more, so
 * these fixtures are also the "there is nothing after this" signal. */
function page(
  entries: LoreEntryView[],
  cap: { capChars: number; firstDroppedId: string } = {
    capChars: 0,
    firstDroppedId: "",
  },
): LoreEntryPageView {
  return {
    entries,
    limit: 30,
    offset: 0,
    total: entries.length,
    usedChars: 0,
    firstDroppedChars: 0,
    budgetSegments: [],
    ...cap,
  };
}

const MIRA_TARGET: LoreBudgetTargetView = {
  scopeKind: "agent",
  scopeKey: "mira",
  capChars: 10000,
  usedChars: 0,
  entryCount: 0,
  droppedCount: 0,
};

function stubList(answer: LoreEntryPageView) {
  return vi.spyOn(api, "listLoreEntries").mockResolvedValue(answer);
}

function renderPage(props: { canSetScope?: boolean } = {}) {
  return render(
    <I18nProvider>
      <LorePage {...props} />
    </I18nProvider>,
  );
}

/** Render with Mira's section as the only target and open its section page
 * from the directory. */
async function renderOpen(props: { canSetScope?: boolean } = {}) {
  if (!vi.isMockFunction(api.listLoreBudgets)) {
    vi.spyOn(api, "listLoreBudgets").mockResolvedValue([MIRA_TARGET]);
  }
  history.replaceState(null, "", window.location.pathname);
  const r = renderPage(props);
  const toggle = await waitFor(() => {
    const el = r.container.querySelector<HTMLElement>('[data-testid="lore-target"]');
    if (!el) throw new Error("no directory target");
    return el;
  }, { timeout: 3000 });
  fireEvent.click(toggle);
  return r;
}

/** The rendered rows, top to bottom, as entry ids. */
function renderedIds(container: HTMLElement): string[] {
  return Array.from(
    container.querySelectorAll('[data-testid="lore-row"]'),
  ).map((el) => el.getAttribute("data-entry-id") ?? "");
}

function rowById(container: HTMLElement, id: string): HTMLElement {
  const el = container.querySelector<HTMLElement>(
    `[data-testid="lore-row"][data-entry-id="${id}"]`,
  );
  if (!el) throw new Error(`no rendered row for ${id}`);
  return el;
}

beforeEach(() => {
  // The page reads its section from the hash; every spec starts on the directory.
  history.replaceState(null, "", window.location.pathname);
  __resetMock();
  clearLocale();
  openEveryLoreDirGroup();
});

afterEach(() => {
  __resetHashRouteForTests();
  vi.restoreAllMocks();
});


describe("LorePage — 變灰", () => {
  // 🔴 `mid` is one of the SHORTEST entries on screen and still dropped: any
  // local title+body sum would grey something else.
  const entries = [
    mkEntry({ id: "p1", state: "pinned", title: "很長很長的置頂標題".repeat(8), dropped: false }),
    mkEntry({ id: "top", state: "active", title: "上面這一筆".repeat(20), dropped: false }),
    mkEntry({ id: "mid", state: "active", title: "短", body: "短", dropped: true }),
    mkEntry({ id: "low", state: "active", dropped: true, authorId: "m-gone" }),
    mkEntry({ id: "r1", state: "retired", retireReason: "重複" }),
  ];

  it("greys out exactly the cards the server marks dropped; a retired card carries no dropped and keeps its own look", async () => {
    stubList({
      ...page(entries, { capChars: 10000, firstDroppedId: "mid" }),
      usedChars: 9640,
      firstDroppedChars: 520,
    });
    const { container } = await renderOpen();

    await waitFor(() => expect(renderedIds(container)).toHaveLength(5));
    expect(
      renderedIds(container).map((id) => [id, rowById(container, id).dataset.dimmed]),
    ).toEqual([
      ["p1", "false"],
      ["top", "false"],
      ["mid", "true"],
      ["low", "true"],
      ["r1", "false"],
    ]);
  });

  it("a dropped entry is one row of id, grey title and 讀不到; a click or Enter opens the full card in place and a click on it folds it back", async () => {
    stubList(page(entries, { capChars: 10000, firstDroppedId: "mid" }));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(5));

    const folded = (id: string) => rowById(container, id).dataset.folded === "true";
    expect(renderedIds(container).filter(folded)).toEqual(["mid", "low"]);
    const mid = rowById(container, "mid");
    expect(mid.getAttribute("role")).toBe("button");
    expect(mid.getAttribute("tabindex")).toBe("0");
    expect(mid.getAttribute("aria-expanded")).toBe("false");
    expect(
      Array.from(mid.children).map((el) => el.getAttribute("data-testid") ?? el.className),
    ).toEqual(["lore-entry-id", "lore-folded-title", "lore-dropped-tag", "lore-row__fold"]);
    expect(mid.querySelector('[data-testid="lore-entry-id"]')!.textContent).toBe("#mid");
    expect(mid.querySelector('[data-testid="lore-folded-title"]')!.textContent).toBe("短");
    expect(mid.querySelector('[data-testid="lore-dropped-tag"]')!.textContent).toBe("讀不到");
    expect(mid.querySelector('[data-testid="lore-body"]')).toBeNull();

    fireEvent.click(mid);
    const opened = rowById(container, "mid");
    expect(opened.dataset.folded).toBeUndefined();
    expect(opened.dataset.dimmed).toBe("true");
    expect(opened.querySelector('[data-testid="lore-body"]')!.textContent).toBe("短");
    expect(opened.getAttribute("aria-expanded")).toBe("true");
    expect(document.activeElement).toBe(opened);
    expect(renderedIds(container)).toEqual(["p1", "top", "mid", "low", "r1"]);
    expect(folded("low")).toBe(true);

    fireEvent.click(opened.querySelector('[data-testid="lore-fold-mark"]')!);
    expect(folded("mid")).toBe(true);
    expect(document.activeElement).toBe(rowById(container, "mid"));

    fireEvent.keyDown(rowById(container, "low"), { key: "Enter" });
    expect(folded("low")).toBe(false);
    expect(rowById(container, "low").querySelector('[data-testid="lore-fold-mark"]')).not.toBeNull();
    fireEvent.keyDown(rowById(container, "mid"), { key: " " });
    expect(folded("mid")).toBe(false);
  });

  it("an opened dropped entry with a staged change stays open, frame and ↺ showing, until the change is undone", async () => {
    stubList(page(entries, { capChars: 10000, firstDroppedId: "mid" }));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(5));

    fireEvent.click(rowById(container, "mid"));
    const row = rowById(container, "mid");
    fireEvent.click(row.querySelector('[data-testid="lore-state"]')!);
    fireEvent.click(row.querySelector('[data-testid="lore-state-pinned"]')!);

    const staged = rowById(container, "mid");
    expect(staged.dataset.staged).toBe("true");
    expect(staged.querySelector('[data-testid="lore-revert-state"]')).not.toBeNull();
    expect(staged.querySelector('[data-testid="lore-fold-mark"]')).toBeNull();
    expect(staged.getAttribute("role")).toBeNull();
    fireEvent.click(staged.querySelector('[data-testid="lore-body"]')!);
    expect(rowById(container, "mid").dataset.folded).toBeUndefined();

    fireEvent.click(staged.querySelector('[data-testid="lore-revert-state"]')!);
    expect(rowById(container, "mid").querySelector('[data-testid="lore-fold-mark"]')).not.toBeNull();
  });

  it("an opened dropped entry has no chevron while its editor is open or its message box holds text, and a click on it does not fold it, so neither can be folded away; one with no message box, or whose author leaves with text still in it, folds as usual", async () => {
    const eventSinks: ((topic: string) => void)[] = [];
    vi.spyOn(api, "subscribeEvents").mockImplementation((sink) => {
      eventSinks.push(sink);
      return () => {};
    });
    stubList(page(entries, { capChars: 10000, firstDroppedId: "mid" }));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(5));
    fireEvent.click(rowById(container, "mid"));
    const collapseOf = () => rowById(container, "mid").querySelector('[data-testid="lore-fold-mark"]');

    fireEvent.click(rowById(container, "mid").querySelector('[data-testid="lore-edit"]')!);
    fireEvent.change(rowById(container, "mid").querySelector('[data-testid="lore-edit-body"]')!, {
      target: { value: "改到一半" },
    });
    expect(collapseOf()).toBeNull();
    expect(rowById(container, "mid").getAttribute("role")).toBeNull();
    fireEvent.click(rowById(container, "mid").querySelector('[data-testid="lore-editor"]')!);
    fireEvent.keyDown(rowById(container, "mid"), { key: "Enter" });
    expect(rowById(container, "mid").querySelector('[data-testid="lore-editor"]')).not.toBeNull();
    fireEvent.click(rowById(container, "mid").querySelector('[data-testid="lore-edit-cancel"]')!);
    expect(collapseOf()).not.toBeNull();

    const box = rowById(container, "mid").querySelector('[data-testid="lore-msg-input"]')!;
    fireEvent.change(box, { target: { value: "還沒送" } });
    expect(collapseOf()).toBeNull();
    fireEvent.change(box, { target: { value: "" } });
    expect(collapseOf()).not.toBeNull();

    fireEvent.click(rowById(container, "low"));
    const low = rowById(container, "low");
    expect(low.querySelector('[data-testid="lore-message-box"]')).toBeNull();
    fireEvent.click(low.querySelector('[data-testid="lore-body"]')!);
    expect(rowById(container, "low").dataset.folded).toBe("true");

    fireEvent.change(box, { target: { value: "還沒送" } });
    expect(collapseOf()).toBeNull();
    const roster = await api.listMembers();
    vi.spyOn(api, "listMembers").mockResolvedValue(roster.filter((m) => m.id !== "mira"));
    for (const sink of eventSinks) sink("member");
    await waitFor(() =>
      expect(rowById(container, "mid").querySelector('[data-testid="lore-message-box"]')).toBeNull(),
    );
    expect(collapseOf()).not.toBeNull();
  });

  it("greys nothing when no entry carries dropped (the request did not converge on one target)", async () => {
    stubList(page(entries.map((e) => ({ ...e, dropped: undefined })), { capChars: 0, firstDroppedId: "" }));
    const { container } = await renderOpen();

    await waitFor(() => expect(renderedIds(container)).toHaveLength(5));
    for (const id of ["p1", "top", "mid", "low", "r1"]) {
      expect(rowById(container, id).dataset.dimmed).toBe("false");
    }
  });
});

describe("LorePage — 訊息框", () => {
  type Posted = {
    to: string;
    body: string;
    meta?: Record<string, unknown>;
    attachments?: ChatAttachmentInput[];
  };
  function capturePosts(): Posted[] {
    const posted: Posted[] = [];
    vi.spyOn(api, "postChat").mockImplementation(async (m) => {
      posted.push({ to: m.to, body: m.body, meta: m.meta, attachments: m.attachments });
    });
    return posted;
  }
  const box = (row: HTMLElement) =>
    row.querySelector<HTMLTextAreaElement>('[data-testid="lore-msg-input"]')!;
  const sendButton = (row: HTMLElement) =>
    row.querySelector<HTMLButtonElement>('[data-testid="lore-msg-send"]')!;

  it("goes to the live author whether or not the entry is pinned; the owner's entry and a departed author's have no message box", async () => {
    stubList(
      page([
        mkEntry({ id: "L-1", state: "active", authorId: "warden-mbp5" }),
        mkEntry({ id: "L-2", state: "pinned", authorId: "warden-mbp5" }),
        mkEntry({ id: "L-3", state: "active", authorId: "owner" }),
        mkEntry({ id: "L-4", state: "active", authorId: "m-gone" }),
      ]),
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(4));
    await waitFor(() =>
      expect(box(rowById(container, "L-1")).placeholder).toBe("傳訊息給 Warden · mbp5…"),
    );
    expect(
      ["L-1", "L-2"].map((id) => {
        const b = box(rowById(container, id));
        return [b.placeholder, b.dataset.recipient];
      }),
    ).toEqual([
      ["傳訊息給 Warden · mbp5…", "warden-mbp5"],
      ["傳訊息給 Warden · mbp5…", "warden-mbp5"],
    ]);
    for (const id of ["L-3", "L-4"]) {
      expect(rowById(container, id).querySelector('[data-testid="lore-message-box"]')).toBeNull();
    }

    const posted = capturePosts();
    for (const id of ["L-1", "L-2"]) {
      fireEvent.change(box(rowById(container, id)), { target: { value: "還適用嗎" } });
      fireEvent.click(sendButton(rowById(container, id)));
      await waitFor(() => expect(box(rowById(container, id)).value).toBe(""));
    }
    expect(posted.map((p) => p.to)).toEqual(["warden-mbp5", "warden-mbp5"]);
  });

  it("picks nobody until the roster answers, then sends to the live author; the owner's entry has no message box even before it answers", async () => {
    const roster = await api.listMembers();
    let answerRoster: (v: typeof roster) => void = () => {};
    vi.spyOn(api, "listMembers").mockReturnValue(
      new Promise((resolve) => {
        answerRoster = resolve;
      }),
    );
    stubList(
      page([
        mkEntry({ id: "L-1", state: "active", authorId: "warden-mbp5" }),
        mkEntry({ id: "L-2", state: "active", authorId: "owner" }),
      ]),
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));
    const posted = capturePosts();
    const row = rowById(container, "L-1");
    expect(rowById(container, "L-2").querySelector('[data-testid="lore-message-box"]')).toBeNull();

    fireEvent.change(box(row), { target: { value: "還適用嗎" } });
    expect(box(row).placeholder).toBe("載入中…");
    expect(sendButton(row).disabled).toBe(true);
    fireEvent.click(sendButton(row));
    fireEvent.keyDown(box(row), { key: "Enter" });
    expect(posted).toEqual([]);

    answerRoster(roster);
    await waitFor(() => expect(box(row).placeholder).toBe("傳訊息給 Warden · mbp5…"));
    fireEvent.click(sendButton(row));
    await waitFor(() => expect(posted.map((p) => p.to)).toEqual(["warden-mbp5"]));
  });

  it("sends nothing when the roster fails to load, and says the roster did not load; the owner's entry has no message box", async () => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.spyOn(api, "listMembers").mockRejectedValue(new Error("boom"));
    stubList(
      page([
        mkEntry({ id: "L-1", state: "active", authorId: "warden-mbp5" }),
        mkEntry({ id: "L-2", state: "active", authorId: "owner" }),
      ]),
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));
    const posted = capturePosts();
    const row = rowById(container, "L-1");

    await waitFor(() => expect(box(row).placeholder).toBe("載入辦公室成員失敗，請稍後重試"));
    expect(rowById(container, "L-2").querySelector('[data-testid="lore-message-box"]')).toBeNull();
    fireEvent.change(box(row), { target: { value: "還適用嗎" } });
    expect(sendButton(row).disabled).toBe(true);
    fireEvent.click(sendButton(row));
    fireEvent.keyDown(box(row), { key: "Enter" });
    await Promise.resolve();
    expect(posted).toEqual([]);
  });

  it("sends this row's entry id ahead of the text, then empties the box", async () => {
    stubList(
      page([
        mkEntry({ id: "L-7", state: "active", authorId: "mira" }),
        mkEntry({ id: "L-31", state: "active", authorId: "mira" }),
      ]),
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));
    const posted = capturePosts();

    const row = rowById(container, "L-31");
    fireEvent.change(box(row), { target: { value: "  這條還適用嗎  " } });
    fireEvent.click(sendButton(row));

    await waitFor(() => expect(posted).toHaveLength(1));
    expect(posted[0]).toEqual({
      to: "mira",
      body: "[LoreID=L-31] 這條還適用嗎",
      meta: { lore_entry_id: "L-31" },
      attachments: undefined,
    });
    await waitFor(() => expect(box(row).value).toBe(""));
    expect(box(rowById(container, "L-7")).value).toBe("");
  });

  it("sends on Enter like the 任務卡 box", async () => {
    stubList(page([mkEntry({ id: "L-7", authorId: "mira" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const posted = capturePosts();
    const row = rowById(container, "L-7");
    fireEvent.change(box(row), { target: { value: "收到" } });
    fireEvent.keyDown(box(row), { key: "Enter" });
    await waitFor(() => expect(posted.map((p) => p.body)).toEqual(["[LoreID=L-7] 收到"]));
  });

  it("carries staged files on the same message and sends with no text at all", async () => {
    stubList(page([mkEntry({ id: "L-7", authorId: "mira" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const posted = capturePosts();
    const row = rowById(container, "L-7");
    expect(sendButton(row).disabled).toBe(true);

    const fileInput = row.querySelector<HTMLInputElement>(
      '[data-testid="lore-message-box"] input[type="file"]',
    )!;
    fireEvent.change(fileInput, {
      target: { files: [new File(["hello"], "note.txt", { type: "text/plain" })] },
    });
    await waitFor(() => expect(sendButton(row).disabled).toBe(false));
    fireEvent.click(sendButton(row));

    await waitFor(() => expect(posted).toHaveLength(1));
    expect(posted[0].body).toBe("[LoreID=L-7] ");
    expect(posted[0].attachments!.map((a) => a.filename)).toEqual(["note.txt"]);
  });

  it("keeps the draft and says so when the send fails", async () => {
    vi.spyOn(api, "postChat").mockRejectedValue(new Error("boom"));
    vi.spyOn(console, "warn").mockImplementation(() => {});
    stubList(page([mkEntry({ id: "L-7", authorId: "mira" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const row = rowById(container, "L-7");

    fireEvent.change(box(row), { target: { value: "這條還適用嗎" } });
    fireEvent.click(sendButton(row));

    await waitFor(() =>
      expect(row.querySelector('[data-testid="lore-msg-failed"]')?.textContent).toBe(
        "訊息送出失敗，請稍後重試",
      ),
    );
    expect(box(row).value).toBe("這條還適用嗎");
  });

  it("offers the 傳承 suggested replies, and a pick fills the box without sending", async () => {
    await mockApi.patchServerSettings({
      suggestedRepliesReplyCard: ["請示卡那一份"],
      suggestedRepliesTaskMessage: ["任務那一份"],
      suggestedRepliesLoreMessage: ["這條還適用嗎", "這條可以退場了"],
    });
    stubList(page([mkEntry({ id: "L-11", authorId: "mira" })]));
    const { container } = await renderOpen();
    const posted = capturePosts();
    const chips = await waitFor(() => {
      const el = container.querySelector<HTMLElement>('[data-testid="lore-suggested-replies"]');
      if (!el) throw new Error("no suggested replies under the box");
      return el;
    });
    expect(Array.from(chips.querySelectorAll("button")).map((b) => b.textContent?.trim())).toEqual([
      "這條還適用嗎",
      "這條可以退場了",
    ]);

    fireEvent.click(chips.querySelectorAll("button")[1]);
    await waitFor(() => expect(box(rowById(container, "L-11")).value).toBe("這條可以退場了"));
    expect(posted).toEqual([]);
  });
});

/** Open the 適用範圍 menu of the one row on screen and answer its option labels. */
async function scopeOptionLabels(
  scopeKind: LoreEntryView["scopeKind"],
  scopeKey: string,
): Promise<string[]> {
  stubList(
    page([
      mkEntry({
        id: "s1",
        scopeKind,
        scopeKey,
        taskTypeKey: scopeKind === "manual" ? scopeKey : "",
        scopeOptions: scopeKind === "manual" ? ["manual", "agent", "everyone"] : ["agent", "everyone"],
      }),
    ]),
  );
  const { container } = await renderOpen({ canSetScope: true });
  await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
  fireEvent.click(container.querySelector('[data-testid="lore-scope-name"]')!);
  return Array.from(container.querySelectorAll('[data-testid="lore-scope-option-label"]')).map(
    (el) => el.textContent ?? "",
  );
}

describe("LorePage — 適用範圍", () => {
  it("the scope rides the tag row as the fourth tag, naming 所有人, the member or the manual", async () => {
    stubList(
      page([
        mkEntry({ id: "s1", scopeKind: "agent", scopeKey: "mira", scopeOptions: ["agent", "everyone"] }),
      ]),
    );
    const { container } = await renderOpen({ canSetScope: true });
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const row = rowById(container, "s1");
    const tag = row.querySelector<HTMLElement>(".lore-row__badges [data-testid='lore-scope-name']")!;
    expect(tag).not.toBeNull();
    expect(tag.textContent).toBe("Mira");
    expect(tag.getAttribute("data-scope-kind")).toBe("agent");
    expect(tag.closest(".lore-row__actions")).toBeNull();
  });

  it("a viewer who may not switch scopes reads the scope tag as plain text; one who may gets a menu trigger on the tag", async () => {
    stubList(
      page([
        mkEntry({
          id: "s1",
          scopeKind: "everyone",
          scopeKey: "",
          scopeOptions: ["agent", "everyone"],
        }),
      ]),
    );
    const viewer = await renderOpen({ canSetScope: false });
    await waitFor(() => expect(renderedIds(viewer.container)).toHaveLength(1));
    const plain = viewer.container.querySelector<HTMLElement>('[data-testid="lore-scope-name"]')!;
    expect(plain.tagName).toBe("SPAN");
    expect(plain.textContent).toBe("所有人");
    expect(plain.hasAttribute("aria-haspopup")).toBe(false);
    expect(plain.closest(".lore-row__badges")).not.toBeNull();
    fireEvent.click(plain);
    expect(viewer.container.querySelector('[data-testid="lore-scope-options"]')).toBeNull();
    viewer.unmount();

    const owner = await renderOpen({ canSetScope: true });
    await waitFor(() => expect(renderedIds(owner.container)).toHaveLength(1));
    const trigger = owner.container.querySelector<HTMLElement>(
      '[data-testid="lore-scope-name"]',
    )!;
    expect(trigger.tagName).toBe("BUTTON");
    expect(trigger.textContent).toBe("所有人");
    expect(trigger.getAttribute("aria-haspopup")).toBe("menu");
    expect(trigger.closest(".lore-row__badges")).not.toBeNull();
    fireEvent.click(trigger);
    expect(owner.container.querySelector('[data-testid="lore-scope-options"]')).not.toBeNull();
  });

  it("names a built-in manual in the UI language inside the menu", async () => {
    useEnglishLocale();
    vi.spyOn(api, "listTaskManuals").mockResolvedValue([
      { typeKey: "builtin-role-design", displayName: "建立／修改角色", purpose: "", fields: [] },
    ] as never);
    const labels = await scopeOptionLabels("manual", "builtin-role-design");
    expect(labels.join("|")).toContain("Create / edit role");
  });

  it("falls back to the raw key for a manual that no longer exists rather than an empty label", async () => {
    vi.spyOn(api, "listTaskManuals").mockResolvedValue([] as never);
    const labels = await scopeOptionLabels("manual", "deleted-type");
    expect(labels.join("|")).toContain("deleted-type");
  });
});

describe("LorePage — 條目編號", () => {
  it("shows the id and copies it on click without folding the card", async () => {
    const copied: string[] = [];
    Object.assign(navigator, {
      clipboard: {
        writeText: (s: string) => {
          copied.push(s);
          return Promise.resolve();
        },
      },
    });
    stubList(page([mkEntry({ id: "L-7" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    const badge = container.querySelector<HTMLElement>(
      '[data-testid="lore-entry-id"]',
    )!;
    expect(badge).not.toBeNull();
    expect(badge.textContent).toContain("L-7");

    fireEvent.click(badge);
    await waitFor(() => expect(copied).toEqual(["L-7"]));

    expect(rowById(container, "L-7").dataset.folded).toBeUndefined();
  });
});

// ────────────────────────────────────────────────────────────────────────────
describe("LorePage — 篩選器複選", () => {
  /** The options of the most recent list request. */
  function lastOpts(spy: ReturnType<typeof stubList>) {
    const calls = spy.mock.calls;
    if (calls.length === 0) throw new Error("listLoreEntries was never called");
    return (calls[calls.length - 1][0] ?? {}) as Record<string, unknown>;
  }

  function tick(testId: string, value: string) {
    openFilters();
    const trigger = document.querySelector<HTMLElement>(
      `[data-testid="${testId}"]`,
    );
    if (!trigger) throw new Error(`no ${testId} filter`);
    if (trigger.getAttribute("aria-expanded") !== "true") {
      fireEvent.click(trigger);
    }
    const box = document.querySelector<HTMLElement>(
      `[data-testid="${testId}-opt-${value}"] input`,
    );
    if (!box) throw new Error(`no ${testId} option ${value}`);
    fireEvent.click(box);
  }

  it("asks for 置頂 and 生效中 by default, and ticking 已失效 adds it to the same request and shows that request's server total", async () => {
    const spy = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) => ({
      ...page([mkEntry({ id: "L-1" })]),
      total: o?.states?.includes("retired") ? 50 : 42,
    }));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    expect(screen.getByTestId("lore-filter-summary").textContent).toBe("42 筆傳承");
    expect(lastOpts(spy)).toEqual({
      states: ["pinned", "active"],
      scopeKinds: ["everyone", "agent"],
      scopeKeys: ["mira"],
      limit: 30,
      offset: 0,
    });

    expect(screen.queryByTestId("lore-filter-count")).toBeNull();

    tick("lore-filter-state", "retired");

    await waitFor(() =>
      expect(lastOpts(spy)).toEqual({
        states: ["pinned", "active", "retired"],
        scopeKinds: ["everyone", "agent"],
        scopeKeys: ["mira"],
        limit: 30,
        offset: 0,
      }),
    );
    expect(screen.getByTestId("lore-filter-count").textContent).toBe("1");
    expect(screen.getByTestId("lore-filter-clear")).not.toBeNull();
    await waitFor(() => expect(screen.getByTestId("lore-filter-summary").textContent).toBe("50 筆傳承"));
  });

  it("sends the ticked types as loreTypes, counts each narrowed field once on the funnel, and 清除篩選 drops them all", async () => {
    const spy = stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    openFilters();
    expect(screen.queryByTestId("lore-filter-clear")).toBeNull();

    fireEvent.click(screen.getByTestId("lore-filter-type"));
    const options = Array.from(
      document.querySelectorAll('[data-testid^="lore-filter-type-opt-"]'),
    ).map((el) => [el.getAttribute("data-testid"), el.textContent]);
    expect(options).toEqual([
      ["lore-filter-type-opt-instruction_conflict", "指示衝突"],
      ["lore-filter-type-opt-instruction_supplement", "指示補充"],
      ["lore-filter-type-opt-owner_decision", "Owner 決策"],
      ["lore-filter-type-opt-owner_preference", "Owner 偏好"],
      ["lore-filter-type-opt-other", "其他"],
    ]);

    const mira = { scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], limit: 30, offset: 0 };
    tick("lore-filter-type", "owner_decision");
    await waitFor(() =>
      expect(lastOpts(spy)).toEqual({
        states: ["pinned", "active"],
        loreTypes: ["owner_decision"],
        ...mira,
      }),
    );
    tick("lore-filter-type", "other");
    await waitFor(() =>
      expect(lastOpts(spy)).toEqual({
        states: ["pinned", "active"],
        loreTypes: ["owner_decision", "other"],
        ...mira,
      }),
    );
    expect(screen.getByTestId("lore-filter-count").textContent).toBe("1");
    searchFor("lore-filter-id", "審查");
    await waitFor(() => expect(lastOpts(spy)).toHaveProperty("q", "審查"));
    expect(screen.getByTestId("lore-filter-count").textContent).toBe("2");

    fireEvent.click(screen.getByTestId("lore-filter-clear"));
    await waitFor(() =>
      expect(lastOpts(spy)).toEqual({ states: ["pinned", "active"], ...mira }),
    );
    expect(screen.queryByTestId("lore-filter-clear")).toBeNull();
    expect(screen.queryByTestId("lore-filter-count")).toBeNull();
    expect(screen.getByTestId<HTMLInputElement>("lore-filter-id").value).toBe("");
  });

  it("keeps the id box, the type and state filters behind a closed funnel in the toolbar row between the section head and the list, labelled in the panel it opens, with no 撰寫人 filter", async () => {
    const spy = stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    const toggle = screen.getByTestId("lore-filter-toggle");
    const toolbar = screen.getByTestId("lore-filter");
    expect(toolbar.previousElementSibling!.getAttribute("data-testid")).toBe("lore-section-head");
    expect(toolbar.nextElementSibling!.getAttribute("data-testid")).toBe("lore-section");
    expect(toolbar.contains(toggle)).toBe(true);
    expect(screen.getByTestId("lore-filter-summary").textContent).toBe("1 筆傳承");
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(document.querySelector('[data-testid^="lore-filter-id"]')).toBeNull();
    expect(document.querySelector('[data-testid="lore-filter-type"]')).toBeNull();

    openFilters();
    const panel = screen.getByRole("dialog", { name: zh.lore.filterDialogTitle });
    const grid = screen.getByTestId("lore-filter-fields");
    expect(panel.contains(grid)).toBe(true);
    expect(
      Array.from(grid.querySelectorAll(".filter-panel__label")).map((el) => el.textContent),
    ).toEqual([zh.lore.idSearchLabel, zh.lore.filterTypeNoun, zh.lore.filterStateNoun]);
    const filters = Array.from(grid.querySelectorAll('[data-testid^="lore-filter-"]'))
      .map((el) => el.getAttribute("data-testid")!)
      .filter((id) => !/-opt-/.test(id));
    expect(filters).toEqual(["lore-filter-id", "lore-filter-type", "lore-filter-state"]);
    expect(lastOpts(spy)).not.toHaveProperty("authorIds");
  });

  it("names an unconstrained filter 「所有…」 and shows the default state set as two ticks", async () => {
    stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    openFilters();
    const labelOf = (testId: string) =>
      screen.getByTestId(testId).textContent!.replace(/\s+/g, "");

    expect(labelOf("lore-filter-type")).toBe("所有類型");
    expect(labelOf("lore-filter-state")).toBe("狀態·2");
  });

});

// ────────────────────────────────────────────────────────────────────────────
// 順序 (owner, 2026-09-07: 「The order of buttons / filters matters」
// 「make them consistent with task」「The consistency is very important」).
//
// 🔴 ORDER IS THE ONE PROPERTY NOTHING ELSE ON THIS PAGE PROTECTS. Every other
// spec here asks whether a thing is PRESENT and whether it CARRIES the right
// value; a row whose badges are shuffled passes all of them, renders cleanly,
// and is wrong only against the other page — which no test on this page ever
// looks at. So it drifts by the ordinary act of adding a field near the top of
// a JSX block, silently, and the person who notices is the reader who has to
// re-learn a layout they already knew.
//
// The two orders are copied FROM 任務頁, and this spec writes them out
// literally so a future change has to state the new order rather than arrive at
// one by accident:
//   篩選列  編號 → 類型 → 狀態                        (任務頁: 任務編號 → 負責人 → 類型 → 狀態；段落頁已經是某個人或某本手冊，沒有撰寫人)
//   列上    編號 → 類型 → 狀態 → 屬於                   (任務卡: 編號 → 優先權 → 狀態 → 類型)
describe("LorePage — 順序跟任務頁一致", () => {
  it("puts the id box and the three filters in the 任務頁 order, and keeps that order as they are used", async () => {
    stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    openFilters();
    const fields = ["lore-filter-id", "lore-filter-type", "lore-filter-state"];
    const idsInDomOrder = () =>
      Array.from(document.querySelectorAll('[data-testid^="lore-filter-"]'))
        .map((el) => el.getAttribute("data-testid")!)
        .filter((id) => fields.includes(id));

    expect(idsInDomOrder()).toEqual(fields);

    fireEvent.click(screen.getByTestId("lore-filter-type"));
    fireEvent.click(
      document.querySelector<HTMLElement>('[data-testid="lore-filter-type-opt-other"] input')!,
    );
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    expect(idsInDomOrder()).toEqual(fields);
  });

  it("reads 編號 → 類型 → 狀態 → 適用範圍 on the row", async () => {
    stubList(page([mkEntry({ id: "L-9", state: "pinned" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    const row = rowById(container, "L-9");
    const marks = Array.from(
      row.querySelectorAll(
        '.lore-row__badges [data-testid]',
      ),
    ).map((el) => el.getAttribute("data-testid"));

    // 🔴 The id LEADS, the way 任務卡's #T-1 does: it is what you quote to
    // somebody else.
    expect(marks).toEqual(["lore-entry-id", "lore-type", "lore-state", "lore-scope-name"]);
    expect(row.querySelector(".lore-row__badges")!.firstElementChild!.getAttribute("data-testid")).toBe(
      "lore-entry-id",
    );
  });

  it("shows the type tag right after the id on every row, ahead of the title", async () => {
    stubList(
      page([
        mkEntry({ id: "L-1", loreType: "instruction_conflict", effectiveTs: 6 }),
        mkEntry({ id: "L-2", loreType: "instruction_supplement", effectiveTs: 5 }),
        mkEntry({ id: "L-3", loreType: "owner_decision", effectiveTs: 4 }),
        mkEntry({ id: "L-4", loreType: "owner_preference", effectiveTs: 3 }),
        mkEntry({ id: "L-5", loreType: "other", effectiveTs: 2 }),
      ]),
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(5));

    const tags = ["L-1", "L-2", "L-3", "L-4", "L-5"].map((id) => {
      const row = rowById(container, id);
      const order = Array.from(
        row.querySelectorAll('[data-testid="lore-type"], [data-testid="lore-entry-id"], .lore-row__title'),
      ).map((el) => el.getAttribute("data-testid") ?? "title");
      const tag = row.querySelector('[data-testid="lore-type"]');
      return [id, order, tag?.textContent ?? null];
    });
    const order = ["lore-entry-id", "lore-type", "title"];
    expect(tags).toEqual([
      ["L-1", order, "指示衝突"],
      ["L-2", order, "指示補充"],
      ["L-3", order, "Owner 決策"],
      ["L-4", order, "Owner 偏好"],
      ["L-5", order, "其他"],
    ]);
  });

  it("labels the type tag in English under the en language", async () => {
    localStorage.setItem("oc.language", "en");
    try {
      stubList(
        page([
          mkEntry({ id: "L-1", loreType: "instruction_conflict", effectiveTs: 5 }),
          mkEntry({ id: "L-2", loreType: "instruction_supplement", effectiveTs: 4 }),
          mkEntry({ id: "L-3", loreType: "owner_decision", effectiveTs: 3 }),
          mkEntry({ id: "L-4", loreType: "owner_preference", effectiveTs: 2 }),
          mkEntry({ id: "L-5", loreType: "other", effectiveTs: 1 }),
        ]),
      );
      const { container } = await renderOpen();
      await waitFor(() => expect(renderedIds(container)).toHaveLength(5));
      expect(
        ["L-1", "L-2", "L-3", "L-4", "L-5"].map(
          (id) => rowById(container, id).querySelector('[data-testid="lore-type"]')?.textContent,
        ),
      ).toEqual([
        "Instruction conflict",
        "Instruction supplement",
        "Owner decision",
        "Owner preference",
        "Other",
      ]);
    } finally {
      localStorage.removeItem("oc.language");
    }
  });

  it("draws the id badge like 任務卡's — the # and a glyph, not a bare number", async () => {
    stubList(page([mkEntry({ id: "L-9" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    const badge = container.querySelector<HTMLElement>(
      '[data-testid="lore-entry-id"]',
    )!;
    // Same three parts 任務卡's badge has: a glyph, a `#`, the number.
    expect(badge.querySelector("svg")).not.toBeNull();
    expect(badge.textContent).toContain("#L-9");
  });
});

// ────────────────────────────────────────────────────────────────────────────
// 卡片版面 (owner 2026-09-08).
describe("LorePage — 卡片底下的撰寫人與生效期", () => {
  it("shows 撰寫人 with the author pill and 生效期 with the time as two label/value rows, and the message box is the last thing on the card", async () => {
    stubList(
      page([
        mkEntry({ id: "L-7", authorId: "mira", title: "標題", body: "四個字的", effectiveTs: 1788400000 }),
      ]),
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const row = rowById(container, "L-7");
    const meta = row.querySelector<HTMLElement>(".lore-row__meta")!;
    expect(Array.from(meta.children).map((el) => el.getAttribute("data-testid") ?? el.textContent)).toEqual([
      "撰寫人",
      "lore-author-row",
      "生效期",
      "lore-effective",
    ]);
    expect(meta.querySelector('[data-testid="lore-author"]')!.textContent).toBe("Mira");
    expect(meta.querySelector('[data-testid="lore-effective"]')!.textContent).toBe(
      formatAbsolute(1788400000, Date.now() / 1000),
    );
    expect(row.lastElementChild!.getAttribute("data-testid")).toBe("lore-message-box");
  });

});

describe("LorePage — 卡片收合", () => {
  it("an entry the server reads opens with its whole body; a click anywhere on the card that is not a control or the end of a text selection, Enter or Space folds only that card to one row of id, title, type and chevron, and the same opens it again", async () => {
    stubList(
      page([
        mkEntry({
          id: "L-7",
          authorId: "mira",
          body: "第一行\n第二行\n第三行\n[第四行](https://example.com/l7)",
          loreType: "owner_decision",
        }),
        mkEntry({ id: "L-8", authorId: "mira" }),
      ]),
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));
    const row = rowById(container, "L-7");
    expect(row.dataset.folded).toBeUndefined();
    expect([row.getAttribute("role"), row.getAttribute("tabindex"), row.getAttribute("aria-expanded"), row.getAttribute("aria-label")]).toEqual([
      "button",
      "0",
      "true",
      "收合",
    ]);
    expect(row.querySelector('[data-testid="lore-body"]')!.textContent).toBe("第一行第二行第三行第四行");
    const mark = row.querySelector<HTMLElement>('[data-testid="lore-fold-mark"]')!;
    expect([mark.tagName, mark.getAttribute("aria-hidden"), mark.title]).toEqual(["SPAN", "true", "收合"]);

    fireEvent.click(row.querySelector('[data-testid="lore-type"]')!);
    fireEvent.click(row.querySelector('[data-testid="lore-body"] a')!);
    fireEvent.click(row.querySelector('[data-testid="lore-msg-input"]')!);
    fireEvent.click(row.querySelector('[data-testid="lore-message-box"]')!);
    fireEvent.keyDown(row.querySelector('[data-testid="lore-msg-input"]')!, { key: "Enter" });
    expect(rowById(container, "L-7").dataset.folded).toBeUndefined();

    const title = row.querySelector<HTMLElement>(".lore-row__title")!;
    const range = document.createRange();
    range.selectNodeContents(title);
    window.getSelection()!.removeAllRanges();
    window.getSelection()!.addRange(range);
    fireEvent.click(title);
    expect(rowById(container, "L-7").dataset.folded).toBeUndefined();
    window.getSelection()!.removeAllRanges();

    fireEvent.click(row.querySelector('[data-testid="lore-body"]')!);
    const folded = rowById(container, "L-7");
    expect(folded.dataset.folded).toBe("true");
    expect(folded.dataset.dimmed).toBe("false");
    expect([folded.getAttribute("role"), folded.getAttribute("tabindex"), folded.getAttribute("aria-expanded")]).toEqual([
      "button",
      "0",
      "false",
    ]);
    expect(
      Array.from(folded.children).map((el) => [el.getAttribute("data-testid") ?? el.className, el.textContent]),
    ).toEqual([
      ["lore-entry-id", "#L-7"],
      ["lore-folded-title", "標題 L-7"],
      ["lore-folded-type", "Owner 決策"],
      ["lore-row__fold", ""],
    ]);
    expect(folded.querySelector<HTMLElement>(".lore-row__fold")!.title).toBe("展開");
    expect(folded.querySelector('[data-testid="lore-body"]')).toBeNull();
    expect(document.activeElement).toBe(folded);
    expect(renderedIds(container)).toEqual(["L-7", "L-8"]);
    expect(rowById(container, "L-8").dataset.folded).toBeUndefined();
    expect(rowById(container, "L-8").querySelector('[data-testid="lore-body"]')!.textContent).toBe("內容 L-8");

    fireEvent.click(folded);
    const reopened = rowById(container, "L-7");
    expect(reopened.dataset.folded).toBeUndefined();
    expect(reopened.querySelector('[data-testid="lore-body"]')!.textContent).toBe("第一行第二行第三行第四行");
    expect(document.activeElement).toBe(reopened);

    fireEvent.keyDown(rowById(container, "L-8"), { key: "Enter" });
    expect(rowById(container, "L-8").dataset.folded).toBe("true");
    fireEvent.keyDown(rowById(container, "L-8"), { key: " " });
    expect(rowById(container, "L-8").dataset.folded).toBeUndefined();
    fireEvent.keyDown(rowById(container, "L-7"), { key: " " });
    expect(rowById(container, "L-7").dataset.folded).toBe("true");
  });
});

// ────────────────────────────────────────────────────────────────────────────
// 內容走 markdown (owner 2026-09-08: 「要支援 md format」).
describe("LorePage — 內容是 markdown", () => {
  it("renders markdown structure instead of printing the source characters", async () => {
    stubList(
      page([
        mkEntry({
          id: "L-1",
          body: "**重點** 和 `code`\n\n- 一\n- 二",
        }),
      ]),
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const body = container.querySelector<HTMLElement>(
      '[data-testid="lore-body"]',
    )!;

    // 🔴 STRUCTURE, NOT TEXT. `.textContent` contains 「重點」 both before and
    // after this change — a plain-text <div> would pass any assertion written
    // against the words. The ELEMENTS are what only a renderer can produce.
    expect(body.querySelector("strong")?.textContent).toBe("重點");
    expect(body.querySelector("code")?.textContent).toBe("code");
    expect(body.querySelectorAll("li")).toHaveLength(2);
    // And the source markers are gone from the reading surface.
    expect(body.textContent).not.toContain("**");
  });

  it("does not inject markup — the renderer builds elements, never HTML", async () => {
    // 🔴 A 傳承 body is AGENT-authored free text. The shared renderer never uses
    // dangerouslySetInnerHTML, and this pins that the wiring did not route
    // around it: the tag must survive as TEXT and must not become an element.
    stubList(page([mkEntry({ id: "L-1", body: "<img src=x onerror=1>" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const body = container.querySelector<HTMLElement>(
      '[data-testid="lore-body"]',
    )!;
    expect(body.querySelector("img")).toBeNull();
    expect(body.textContent).toContain("<img src=x onerror=1>");
  });

  it("leaves an underscore-bearing identifier alone", async () => {
    // The corpus that already exists was written as PLAIN TEXT, so the worry is
    // that a stored body's punctuation is now read as syntax. `_` is not inline
    // syntax in this renderer, and this is the case that prompted the question
    // (a live entry carrying `note_size_chars`), pinned so a future switch to a
    // fuller markdown engine has to notice it.
    stubList(page([mkEntry({ id: "L-1", body: "看 note_size_chars 這個欄位" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    expect(
      container.querySelector('[data-testid="lore-body"]')!.textContent,
    ).toContain("note_size_chars");
  });
});

// ────────────────────────────────────────────────────────────────────────────
// 可變動作面 — 失效 / 生效 / 置頂, and the 403 path `runAction` exists for.
//
// 🔴 WHY THESE ARE SEPARATE SPECS AND NOT ONE. The actions share `runAction`
// but each one names its OWN target value. A single spec that drove all of
// them would go green while the others were gutted, because the last one's
// call would satisfy it. Split, a gutted action reddens exactly
// its own line.
//
// 🔴 AND WHY THE 403 SPEC IS THE ONE THAT MATTERS MOST. 置頂 is admin-only and
// 失效/生效 is author-or-admin, so a refusal is a REACHABLE answer, not
// a bug. A swallowed 403 and 「什麼都沒發生」 are the same pixels: the row keeps
// its old badge either way. `lore-action-error` carrying WORDS is the only
// thing that tells those two apart, so the spec asserts the element exists AND
// that it says something.
describe("LorePage — 可變動作面", () => {
  /** Open the 狀態 menu ON ONE ROW and click one of its items. */
  function chooseState(
    container: HTMLElement,
    entryId: string,
    next: LoreEntryView["state"],
  ) {
    const row = rowById(container, entryId);
    fireEvent.click(row.querySelector('[data-testid="lore-state"]')!);
    const item = row.querySelector(`[data-testid="lore-state-${next}"]`);
    if (!item) throw new Error(`no ${next} menu item on ${entryId}`);
    fireEvent.click(item);
  }

  function chooseType(container: HTMLElement, entryId: string, next: LoreEntryView["loreType"]) {
    const row = rowById(container, entryId);
    fireEvent.click(row.querySelector('[data-testid="lore-type"]')!);
    fireEvent.click(row.querySelector(`[data-testid="lore-type-${next}"]`)!);
  }

  const stateOf = (container: HTMLElement, id: string) =>
    rowById(container, id).querySelector('[data-testid="lore-state"]')!.textContent;
  const unsavedIds = (container: HTMLElement) =>
    renderedIds(container).filter((id) => rowById(container, id).dataset.staged === "true");
  const revertMarks = (container: HTMLElement, id: string) =>
    Array.from(rowById(container, id).querySelectorAll<HTMLElement>('[data-testid^="lore-revert-"]')).map(
      (el) => [el.dataset.testid, el.getAttribute("aria-label")],
    );
  // jsdom has no PointerEvent, so fireEvent.pointerDown drops pointerType.
  function press(el: Element, pointerType: "mouse" | "touch") {
    const ev = new MouseEvent("pointerdown", { bubbles: true });
    Object.defineProperty(ev, "pointerType", { value: pointerType });
    fireEvent(el, ev);
    fireEvent.click(el);
  }
  const mark = (container: HTMLElement, id: string, field: string) =>
    rowById(container, id).querySelector(`[data-testid="lore-revert-${field}"]`)!;
  const bar = (container: HTMLElement) => container.querySelector<HTMLElement>('[data-testid="lore-staged-bar"]');
  const clickSave = (container: HTMLElement) =>
    fireEvent.click(container.querySelector('[data-testid="lore-staged-save"]')!);

  const MIRA_OPTS = { states: ["pinned", "active"], scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], limit: 30, offset: 0 };

  function spyWrites() {
    return {
      state: vi.spyOn(api, "setLoreEntryState").mockResolvedValue(undefined),
      scope: vi.spyOn(api, "setLoreEntryScope").mockResolvedValue(undefined),
      edit: vi.spyOn(api, "editLoreEntry").mockResolvedValue(undefined),
    };
  }

  it("changing state, type and scope stages them on the card: new values, a ↺ on each changed tag, the bar's count, and nothing sent", async () => {
    const w = spyWrites();
    const list = stubList(
      page([
        mkEntry({ id: "L-1", state: "active" }),
        mkEntry({ id: "L-2", state: "active", scopeOptions: ["agent", "everyone"] }),
        mkEntry({ id: "L-3", state: "active" }),
      ]),
    );
    vi.spyOn(api, "listLoreBudgets").mockResolvedValue([{ ...MIRA_TARGET, entryCount: 3, usedChars: 1200 }]);
    const { container } = await renderOpen({ canSetScope: true });
    await waitFor(() => expect(renderedIds(container)).toHaveLength(3));
    expect(bar(container)).toBeNull();

    chooseState(container, "L-1", "retired");
    chooseType(container, "L-2", "owner_decision");
    fireEvent.click(rowById(container, "L-2").querySelector('[data-testid="lore-scope-name"]')!);
    fireEvent.click(rowById(container, "L-2").querySelector('[data-testid="lore-scope-everyone"]')!);

    expect(stateOf(container, "L-1")).toBe("已失效");
    expect(rowById(container, "L-2").querySelector('[data-testid="lore-type"]')!.textContent).toBe("Owner 決策");
    expect(rowById(container, "L-2").querySelector('[data-testid="lore-scope-name"]')!.textContent).toBe("所有人");
    expect(unsavedIds(container)).toEqual(["L-1", "L-2"]);
    expect(revertMarks(container, "L-1")).toEqual([["lore-revert-state", "還原為「生效中」"]]);
    expect(revertMarks(container, "L-2")).toEqual([
      ["lore-revert-loreType", "還原為「其他」"],
      ["lore-revert-scope", "還原為「Mira」"],
    ]);
    expect(revertMarks(container, "L-3")).toEqual([]);
    expect(sectionHead(container)).toBe("Mira1,200 / 10,000 字");
    expect(container.querySelector('[data-testid="lore-staged-count"]')!.textContent).toBe("3 項變更還沒儲存");
    expect(
      Array.from(bar(container)!.querySelectorAll("button")).map((b) => b.textContent),
    ).toEqual(["全部捨棄", "儲存變更"]);
    expect(renderedIds(container)).toEqual(["L-1", "L-2", "L-3"]);
    await new Promise((r) => setTimeout(r, 20));
    expect(w.state).not.toHaveBeenCalled();
    expect(w.scope).not.toHaveBeenCalled();
    expect(w.edit).not.toHaveBeenCalled();
    expect(list).toHaveBeenCalledTimes(1);
  });

  it("choosing the stored value again takes the card's frame, the ↺ and the bar away", async () => {
    stubList(page([mkEntry({ id: "L-1", state: "active" }), mkEntry({ id: "L-2", state: "active" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));

    chooseState(container, "L-2", "pinned");
    expect(unsavedIds(container)).toEqual(["L-2"]);
    chooseState(container, "L-2", "active");

    expect(unsavedIds(container)).toEqual([]);
    expect(revertMarks(container, "L-2")).toEqual([]);
    expect(stateOf(container, "L-2")).toBe("生效中");
    expect(bar(container)).toBeNull();
  });

  it("a click on a tag's ↺ puts only that field back and sends nothing; the card loses its frame once its last field is back", async () => {
    const w = spyWrites();
    const list = stubList(
      page([mkEntry({ id: "L-1", state: "active" }), mkEntry({ id: "L-2", state: "active" }), mkEntry({ id: "L-3" })]),
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(3));

    chooseState(container, "L-1", "retired");
    chooseState(container, "L-2", "pinned");
    chooseType(container, "L-2", "owner_preference");
    chooseType(container, "L-3", "instruction_conflict");
    press(mark(container, "L-2", "state"), "mouse");

    expect(stateOf(container, "L-2")).toBe("生效中");
    expect(rowById(container, "L-2").querySelector('[data-testid="lore-type"]')!.textContent).toBe("Owner 偏好");
    expect(revertMarks(container, "L-2")).toEqual([["lore-revert-loreType", "還原為「其他」"]]);
    expect(unsavedIds(container)).toEqual(["L-1", "L-2", "L-3"]);
    expect(stateOf(container, "L-1")).toBe("已失效");
    expect(container.querySelector('[data-testid="lore-staged-count"]')!.textContent).toBe("3 項變更還沒儲存");

    press(mark(container, "L-2", "loreType"), "mouse");

    expect(rowById(container, "L-2").querySelector('[data-testid="lore-type"]')!.textContent).toBe("其他");
    expect(unsavedIds(container)).toEqual(["L-1", "L-3"]);
    expect(container.querySelector('[data-testid="lore-staged-count"]')!.textContent).toBe("2 項變更還沒儲存");
    await new Promise((r) => setTimeout(r, 20));
    expect(w.state).not.toHaveBeenCalled();
    expect(w.edit).not.toHaveBeenCalled();
    expect(list).toHaveBeenCalledTimes(1);
  });

  it("hovering a ↺ says which value it reverts to", async () => {
    stubList(page([mkEntry({ id: "L-1", state: "active" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    chooseState(container, "L-1", "pinned");
    fireEvent.mouseEnter(mark(container, "L-1", "state"));

    expect(screen.getByRole("tooltip").textContent).toBe("還原為「生效中」");
  });

  it("a tap on a ↺ puts only that field back at once", async () => {
    stubList(page([mkEntry({ id: "L-1", state: "active" }), mkEntry({ id: "L-2", state: "active" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));

    chooseState(container, "L-1", "pinned");
    chooseType(container, "L-1", "owner_decision");
    press(mark(container, "L-1", "state"), "touch");

    expect(stateOf(container, "L-1")).toBe("生效中");
    expect(rowById(container, "L-1").querySelector('[data-testid="lore-type"]')!.textContent).toBe("Owner 決策");
    expect(unsavedIds(container)).toEqual(["L-1"]);
  });

  it("儲存變更 sends every staged change once, then re-reads the whole section in the server's order, folds what the new answer newly marks dropped and keeps an opened one open", async () => {
    const w = spyWrites();
    const before = page(
      [
        mkEntry({ id: "P-1", state: "pinned", dropped: false }),
        mkEntry({ id: "A-1", dropped: false }),
        mkEntry({ id: "A-2", dropped: false }),
        mkEntry({ id: "A-3", dropped: true }),
      ],
      { capChars: 1000, firstDroppedId: "A-3" },
    );
    const after = page(
      [
        mkEntry({ id: "A-1", loreType: "owner_decision", dropped: false }),
        mkEntry({ id: "A-2", dropped: false }),
        mkEntry({ id: "P-1", state: "active", dropped: true }),
        mkEntry({ id: "A-3", dropped: true }),
      ],
      { capChars: 1000, firstDroppedId: "P-1" },
    );
    let saved = false;
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async () => (saved ? after : before));
    const budgets = vi
      .spyOn(api, "listLoreBudgets")
      .mockResolvedValueOnce([{ ...MIRA_TARGET, entryCount: 4, droppedCount: 1 }])
      .mockResolvedValue([{ ...MIRA_TARGET, entryCount: 4, droppedCount: 2 }]);
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toEqual(["P-1", "A-1", "A-2", "A-3"]));
    expect(sectionHead(container)).toContain("1 筆讀不到");

    chooseState(container, "P-1", "active");
    chooseType(container, "A-1", "owner_decision");
    fireEvent.click(rowById(container, "A-3"));
    const look = () =>
      renderedIds(container).map((id) => [
        id,
        rowById(container, id).getAttribute("data-dimmed"),
        rowById(container, id).getAttribute("data-folded") === "true",
      ]);
    expect(look()).toEqual([
      ["P-1", "false", false],
      ["A-1", "false", false],
      ["A-2", "false", false],
      ["A-3", "true", false],
    ]);
    saved = true;
    clickSave(container);

    await waitFor(() => expect(renderedIds(container)).toEqual(["A-1", "A-2", "P-1", "A-3"]));
    expect(w.state.mock.calls).toEqual([["P-1", "active"]]);
    expect(w.edit.mock.calls).toEqual([["A-1", { loreType: "owner_decision" }]]);
    expect(w.scope).not.toHaveBeenCalled();
    expect(list.mock.calls.map((c) => c[0])).toEqual([MIRA_OPTS, MIRA_OPTS]);
    expect(look()).toEqual([
      ["A-1", "false", false],
      ["A-2", "false", false],
      ["P-1", "true", true],
      ["A-3", "true", false],
    ]);
    expect(unsavedIds(container)).toEqual([]);
    expect(bar(container)).toBeNull();
    expect(budgets).toHaveBeenCalledTimes(2);
    await waitFor(() => expect(sectionHead(container)).toContain("2 筆讀不到"));
  });

  it("儲存變更 re-reads as many rows as were loaded, past the first page, and keeps them all", async () => {
    spyWrites();
    const rows = Array.from({ length: 45 }, (_, i) => mkEntry({ id: `L-${i + 1}` }));
    const list = vi
      .spyOn(api, "listLoreEntries")
      .mockImplementation(async (o) => page(rows.slice(o?.offset ?? 0, (o?.offset ?? 0) + (o?.limit ?? 30))));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(30));
    fireEvent.scroll(container.querySelector('[data-testid="lore-page"]')!);
    await waitFor(() => expect(renderedIds(container)).toHaveLength(45));

    chooseState(container, "L-40", "pinned");
    clickSave(container);

    await waitFor(() => expect(bar(container)).toBeNull());
    expect(list.mock.calls.map((c) => [c[0]?.limit, c[0]?.offset])).toEqual([
      [30, 0],
      [30, 30],
      [60, 0],
    ]);
    expect(renderedIds(container)).toEqual(rows.map((e) => e.id));
  });

  it("when the save goes through but the re-read fails, the cards show the saved values, a refused field stays staged against its stored value, and the page asks for a reload", async () => {
    const w = spyWrites();
    w.state.mockImplementation(async (id) => {
      if (id === "L-2") throw new ApiError("http 403 for POST /api/lore/L-2/state", 403, "forbidden", "");
    });
    let saved = false;
    vi.spyOn(api, "listLoreEntries").mockImplementation(async () => {
      if (saved) throw new Error("network down");
      return page([mkEntry({ id: "L-1", state: "active" }), mkEntry({ id: "L-2", state: "active" })]);
    });
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));
    expect(container.querySelector('[data-testid="lore-action-error"]')).toBeNull();

    chooseState(container, "L-1", "retired");
    chooseType(container, "L-2", "owner_decision");
    chooseState(container, "L-2", "pinned");
    saved = true;
    clickSave(container);

    const failed = await waitFor(() => {
      const el = container.querySelector<HTMLElement>('[data-testid="lore-staged-failed"]');
      if (!el) throw new Error("no failure line");
      return el;
    });
    expect(failed.textContent).toBe(`沒有成功：L-2（${zh.lore.forbidden}）`);
    expect(w.state.mock.calls).toEqual([
      ["L-1", "retired"],
      ["L-2", "pinned"],
    ]);
    expect(w.edit.mock.calls).toEqual([["L-2", { loreType: "owner_decision" }]]);
    expect(container.querySelector('[data-testid="lore-action-error"]')!.textContent).toBe(
      "已儲存，但重新讀取失敗，請重新整理頁面",
    );
    expect(stateOf(container, "L-1")).toBe("已失效");
    expect(rowById(container, "L-2").querySelector('[data-testid="lore-type"]')!.textContent).toBe("Owner 決策");
    expect(stateOf(container, "L-2")).toBe("置頂");
    expect(unsavedIds(container)).toEqual(["L-2"]);
    expect(revertMarks(container, "L-2")).toEqual([["lore-revert-state", "還原為「生效中」"]]);
    fireEvent.mouseEnter(mark(container, "L-2", "state"));
    expect(screen.getByRole("tooltip").textContent).toBe("還原為「生效中」");
    expect(renderedIds(container)).toEqual(["L-1", "L-2"]);
  });

  it("全部捨棄 puts every card back to its stored value and sends nothing", async () => {
    const w = spyWrites();
    const list = stubList(page([mkEntry({ id: "L-1", state: "active" }), mkEntry({ id: "L-2", state: "pinned" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));

    chooseState(container, "L-1", "retired");
    chooseType(container, "L-2", "instruction_conflict");
    fireEvent.click(container.querySelector('[data-testid="lore-staged-discard"]')!);

    expect(stateOf(container, "L-1")).toBe("生效中");
    expect(rowById(container, "L-2").querySelector('[data-testid="lore-type"]')!.textContent).toBe("其他");
    expect(unsavedIds(container)).toEqual([]);
    expect(bar(container)).toBeNull();
    await new Promise((r) => setTimeout(r, 20));
    expect(w.state).not.toHaveBeenCalled();
    expect(w.edit).not.toHaveBeenCalled();
    expect(list).toHaveBeenCalledTimes(1);
  });

  it("a refused change stays staged and the bar says which and why; what went through is saved", async () => {
    vi.spyOn(api, "setLoreEntryState").mockImplementation(async (id) => {
      if (id === "L-2") {
        throw new ApiError("http 403 for POST /api/lore/L-2/state", 403, "forbidden", "");
      }
    });
    let saved = false;
    vi.spyOn(api, "listLoreEntries").mockImplementation(async () =>
      page([mkEntry({ id: "L-1", state: saved ? "retired" : "active" }), mkEntry({ id: "L-2", state: "active" })]),
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));

    chooseState(container, "L-1", "retired");
    chooseState(container, "L-2", "pinned");
    saved = true;
    clickSave(container);

    const failed = await waitFor(() => {
      const el = container.querySelector<HTMLElement>('[data-testid="lore-staged-failed"]');
      if (!el) throw new Error("no failure line");
      return el;
    });
    expect(failed.textContent).toBe(`沒有成功：L-2（${zh.lore.forbidden}）`);
    expect(unsavedIds(container)).toEqual(["L-2"]);
    expect(stateOf(container, "L-2")).toBe("置頂");
    expect(stateOf(container, "L-1")).toBe("已失效");
    expect(container.querySelector('[data-testid="lore-staged-count"]')!.textContent).toBe("1 項變更還沒儲存");
  });

  it("while 儲存變更 runs, the chips, their ↺ and both buttons are locked", async () => {
    const sent = held<void>();
    vi.spyOn(api, "setLoreEntryState").mockReturnValue(sent.promise);
    stubList(page([mkEntry({ id: "L-1", state: "active" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    chooseState(container, "L-1", "pinned");
    clickSave(container);

    const row = rowById(container, "L-1");
    await waitFor(() =>
      expect(container.querySelector('[data-testid="lore-staged-save"]')!.textContent).toBe("儲存中…"),
    );
    expect(
      ["lore-state", "lore-revert-state", "lore-type", "lore-staged-save", "lore-staged-discard"].map(
        (id) => (container.querySelector(`[data-testid="${id}"]`) as HTMLButtonElement).disabled,
      ),
    ).toEqual([true, true, true, true, true]);
    press(mark(container, "L-1", "state"), "touch");
    expect(stateOf(container, "L-1")).toBe("置頂");
    expect(row.querySelector<HTMLButtonElement>('[data-testid="lore-edit"]')!.disabled).toBe(false);
    sent.resolve();
    await waitFor(() => expect(bar(container)).toBeNull());
  });

  it("with staged changes, leaving the section asks first: staying keeps them, leaving goes", async () => {
    stubList(page([mkEntry({ id: "L-1", state: "active" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const ask = vi.spyOn(window, "confirm").mockReturnValue(false);

    fireEvent.click(container.querySelector('[data-testid="lore-back"]')!);
    expect(ask).toHaveBeenCalledTimes(0);
    await waitFor(() => expect(window.location.hash).toBe("#lore"));

    openSection(container, "agent:mira");
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    chooseState(container, "L-1", "retired");

    const unload = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(unload);
    expect(unload.defaultPrevented).toBe(true);

    fireEvent.click(container.querySelector('[data-testid="lore-back"]')!);
    expect(ask.mock.calls).toEqual([[zh.lore.stagedLeaveConfirm]]);
    expect(window.location.hash).toBe("#lore/agent/mira");
    expect(unsavedIds(container)).toEqual(["L-1"]);

    ask.mockReturnValue(true);
    fireEvent.click(container.querySelector('[data-testid="lore-back"]')!);
    await waitFor(() => expect(window.location.hash).toBe("#lore"));
    await waitFor(() => expect(headerTexts(container)).toHaveLength(1));
  });

  it("while 儲存變更 runs, leaving is refused without asking and the bar says so", async () => {
    const sent = held<void>();
    vi.spyOn(api, "setLoreEntryState").mockReturnValue(sent.promise);
    stubList(page([mkEntry({ id: "L-1", state: "active" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const ask = vi.spyOn(window, "confirm").mockReturnValue(true);

    chooseState(container, "L-1", "retired");
    clickSave(container);
    fireEvent.click(container.querySelector('[data-testid="lore-back"]')!);

    expect(ask).not.toHaveBeenCalled();
    expect(window.location.hash).toBe("#lore/agent/mira");
    await waitFor(() =>
      expect(container.querySelector('[data-testid="lore-staged-leave-blocked"]')?.textContent).toBe(
        "儲存中，完成後才能離開",
      ),
    );
    sent.resolve();
    await waitFor(() => expect(bar(container)).toBeNull());
  });
});

describe("LorePage — 適用範圍選單", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.resetModules();
    localStorage.clear();
  });

  const MANUALS = [
    { typeKey: "review-pr", displayName: "PR 審查", purpose: "", fields: [] },
  ];

  function openScopeMenu(container: HTMLElement, entryId: string): HTMLElement {
    const row = rowById(container, entryId);
    fireEvent.click(row.querySelector('[data-testid="lore-scope-name"]')!);
    const menu = row.querySelector<HTMLElement>(
      '[data-testid="lore-scope-options"]',
    );
    if (!menu) throw new Error(`no scope menu on ${entryId}`);
    return menu;
  }

  /** Every option as [kind, label, tag, checked, full text]. */
  function readOptions(menu: HTMLElement) {
    return Array.from(
      menu.querySelectorAll<HTMLElement>('[role="menuitemradio"]'),
    ).map((item) => [
      item.getAttribute("data-testid"),
      item.querySelector('[data-testid="lore-scope-option-label"]')!.textContent,
      item.querySelector('[data-testid="lore-scope-option-tag"]')?.textContent ??
        "",
      item.getAttribute("aria-checked"),
      item.textContent,
    ]);
  }

  const taskBound = mkEntry({
    id: "L-1",
    scopeKind: "manual",
    scopeKey: "review-pr",
    taskTypeKey: "review-pr",
    sourceTaskId: "T-5",
    scopeOptions: ["manual", "agent", "everyone"],
  });
  const staffNoTask = mkEntry({
    id: "L-2",
    scopeKind: "agent",
    scopeKey: "mira",
    scopeOptions: ["agent", "everyone"],
  });
  const everyoneNow = mkEntry({
    id: "L-3",
    scopeKind: "everyone",
    scopeKey: "",
    scopeOptions: ["agent", "everyone"],
  });

  async function renderRows(entries: LoreEntryView[]) {
    vi.spyOn(api, "listTaskManuals").mockResolvedValue(MANUALS as never);
    const list = stubList(page(entries));
    const { container } = await renderOpen({ canSetScope: true });
    await waitFor(() =>
      expect(renderedIds(container)).toHaveLength(entries.length),
    );
    // The menu's labels need the roster and the manuals; wait until one reads
    // a resolved name rather than a raw key.
    await waitFor(() => {
      const menu = openScopeMenu(container, entries[0].id);
      const text = menu.textContent ?? "";
      fireEvent.mouseDown(document.body);
      expect(text).not.toMatch(/review-pr|：mira/);
    });
    return { container, list };
  }

  it("a task-bound entry's menu lists task, author, everyone with the task option checked and default", async () => {
    const { container } = await renderRows([taskBound]);
    const menu = openScopeMenu(container, "L-1");

    expect(menu.getAttribute("role")).toBe("menu");
    expect(menu.querySelector(".lore-row__scope-pop-title")!.textContent).toBe(
      "適用範圍",
    );
    expect(readOptions(menu)).toEqual([
      [
        "lore-scope-manual",
        "任務：PR 審查",
        "預設",
        "true",
        "任務：PR 審查預設",
      ],
      [
        "lore-scope-agent",
        "建立者：Mira",
        "",
        "false",
        "建立者：Mira",
      ],
      [
        "lore-scope-everyone",
        "所有人",
        "",
        "false",
        "所有人",
      ],
    ]);
    expect(
      Array.from(menu.querySelectorAll(".lore-row__menu-item--active")).map((el) =>
        el.getAttribute("data-testid"),
      ),
    ).toEqual(["lore-scope-manual"]);
    expect(menu.textContent).toBe(
      "適用範圍任務：PR 審查預設建立者：Mira所有人開啟任務手冊 PR 審查",
    );
  });

  it("a staff entry without a task offers author and everyone only", async () => {
    const { container } = await renderRows([staffNoTask]);
    const menu = openScopeMenu(container, "L-2");
    expect(readOptions(menu)).toEqual([
      [
        "lore-scope-agent",
        "建立者：Mira",
        "預設",
        "true",
        "建立者：Mira預設",
      ],
      [
        "lore-scope-everyone",
        "所有人",
        "",
        "false",
        "所有人",
      ],
    ]);
    expect(menu.textContent).toBe("適用範圍建立者：Mira預設所有人");
    expect(menu.querySelector('[data-testid="lore-scope-open-manual"]')).toBeNull();
  });

  it("a current scope that is not the default is tagged 目前 while the default keeps 預設", async () => {
    const { container } = await renderRows([everyoneNow]);
    const menu = openScopeMenu(container, "L-3");
    expect(readOptions(menu).map((o) => [o[0], o[2], o[3]])).toEqual([
      ["lore-scope-agent", "預設", "false"],
      ["lore-scope-everyone", "目前", "true"],
    ]);
  });

  it("picking another scope stages it on that card; 儲存變更 sends it, re-reads the section and says where the entry went", async () => {
    const set = vi.spyOn(api, "setLoreEntryScope").mockResolvedValue(undefined);
    const { container, list } = await renderRows([taskBound, staffNoTask]);
    let saved = false;
    list.mockImplementation(async () =>
      saved ? page([taskBound, { ...staffNoTask, scopeKind: "everyone", scopeKey: "" }]) : page([taskBound, staffNoTask]),
    );

    const menu = openScopeMenu(container, "L-2");
    const everyoneLabel = readOptions(menu).find((o) => o[0] === "lore-scope-everyone")![1];
    fireEvent.click(menu.querySelector('[data-testid="lore-scope-everyone"]')!);

    expect(container.querySelector('[data-testid="lore-scope-options"]')).toBeNull();
    expect(set).not.toHaveBeenCalled();
    expect(rowById(container, "L-2").dataset.staged).toBe("true");
    expect(readOptions(openScopeMenu(container, "L-2")).find((o) => o[3] === "true")?.[0]).toBe("lore-scope-everyone");
    fireEvent.mouseDown(document.body);

    saved = true;
    fireEvent.click(container.querySelector('[data-testid="lore-staged-save"]')!);

    await waitFor(() => expect(container.querySelector('[data-testid="lore-staged-bar"]')).toBeNull());
    expect(set.mock.calls).toEqual([["L-2", "everyone"]]);
    expect(container.querySelector('[data-testid="lore-toast"]')!.textContent).toBe(`L-2 的適用範圍改成「${everyoneLabel}」`);
    expect(list.mock.calls).toEqual([
      [{ states: ["pinned", "active"], scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], limit: 30, offset: 0 }],
      [{ states: ["pinned", "active"], scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], limit: 30, offset: 0 }],
    ]);
    expect(rowById(container, "L-2").dataset.staged).toBe("false");
    expect(readOptions(openScopeMenu(container, "L-2")).find((o) => o[3] === "true")?.[0]).toBe("lore-scope-everyone");
  });

  it("picking the current scope closes the menu and stages nothing", async () => {
    const set = vi.spyOn(api, "setLoreEntryScope").mockResolvedValue(undefined);
    const { container } = await renderRows([staffNoTask]);

    const menu = openScopeMenu(container, "L-2");
    fireEvent.click(menu.querySelector('[data-testid="lore-scope-agent"]')!);

    expect(
      container.querySelector('[data-testid="lore-scope-options"]'),
    ).toBeNull();
    expect(container.querySelector('[data-testid="lore-staged-bar"]')).toBeNull();
    expect(set).not.toHaveBeenCalled();
  });

  it("a refused switch keeps the scope staged and shows the server's reason on the bar", async () => {
    vi.spyOn(api, "setLoreEntryScope").mockRejectedValue(
      new ApiError(
        "http 400 for POST /api/lore/L-2/scope",
        400,
        codeForStatus(400),
        "lore entry L-2 has no task type to key a manual scope to",
      ),
    );
    const { container } = await renderRows([staffNoTask]);

    fireEvent.click(
      openScopeMenu(container, "L-2").querySelector(
        '[data-testid="lore-scope-everyone"]',
      )!,
    );
    fireEvent.click(container.querySelector('[data-testid="lore-staged-save"]')!);

    await waitFor(() =>
      expect(container.querySelector('[data-testid="lore-staged-failed"]')?.textContent).toBe(
        "沒有成功：L-2（lore entry L-2 has no task type to key a manual scope to）",
      ),
    );
    expect(container.querySelector('[data-testid="lore-toast"]')).toBeNull();
    expect(rowById(container, "L-2").dataset.staged).toBe("true");
    expect(
      readOptions(openScopeMenu(container, "L-2")).find((o) => o[3] === "true")?.[0],
    ).toBe("lore-scope-everyone");
  });

  it("a mousedown outside the badge closes the menu; one inside keeps it open", async () => {
    const { container } = await renderRows([staffNoTask]);
    const menu = openScopeMenu(container, "L-2");

    fireEvent.mouseDown(menu);
    expect(
      container.querySelector('[data-testid="lore-scope-options"]'),
    ).not.toBeNull();

    fireEvent.mouseDown(document.body);
    expect(
      container.querySelector('[data-testid="lore-scope-options"]'),
    ).toBeNull();
  });

  it("for the owner the menu of a manual entry ends with a row that opens the manual", async () => {
    const { container } = await renderRows([taskBound]);
    window.location.hash = "";

    const menu = openScopeMenu(container, "L-1");
    expect(window.location.hash).toBe("");

    const jump = menu.querySelector<HTMLElement>(
      '[data-testid="lore-scope-open-manual"]',
    )!;
    expect(jump.textContent).toBe("開啟任務手冊 PR 審查");
    fireEvent.click(jump);
    expect(window.location.hash).toBe("#settings/manuals/review-pr");
    expect(
      container.querySelector('[data-testid="lore-scope-options"]'),
    ).toBeNull();
  });

  function jwt(claims: Record<string, unknown>): string {
    const b64url = (v: unknown) =>
      btoa(String.fromCharCode(...new TextEncoder().encode(JSON.stringify(v)))).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
    return `${b64url({ alg: "HS256", typ: "JWT" })}.${b64url(claims)}.sig`;
  }

  async function renderAs(token: string) {
    vi.stubEnv("VITE_USE_MOCK", "false");
    vi.stubEnv("VITE_OC_TOKEN", "");
    localStorage.setItem("oc_token", token);
    vi.resetModules();
    const real = await import("../api");
    const { LorePage: RealLorePage } = await import("./LorePage");
    const { I18nProvider: RealI18nProvider } = await import("../i18n");
    expect(real.USE_MOCK).toBe(false);
    vi.spyOn(real.api, "subscribeEvents").mockReturnValue(() => {});
    vi.spyOn(real.api, "getServerSettings").mockReturnValue(new Promise(() => {}));
    vi.spyOn(real.api, "listThemes").mockReturnValue(new Promise(() => {}));
    vi.spyOn(real.api, "listOutsourceWorkers").mockResolvedValue([]);
    vi.spyOn(real.api, "listTaskManuals").mockResolvedValue([]);
    vi.spyOn(real.api, "listMembers").mockResolvedValue([
      { id: "mira", name: "Mira", role: "assistant", kind: "staff" },
      { id: "m-kyle", name: "Kyle", role: "officraft-developer", kind: "staff" },
    ] as never);
    vi.spyOn(real.api, "listLoreBudgets").mockResolvedValue([MIRA_TARGET]);
    vi.spyOn(real.api, "listLoreEntries").mockResolvedValue(
      page([
        mkEntry({
          id: "L-2",
          scopeKind: "agent",
          scopeKey: "mira",
          scopeOptions: ["agent", "everyone"],
        }),
      ]),
    );
    const { container } = render(
      <RealI18nProvider>
        <RealLorePage />
      </RealI18nProvider>,
    );
    fireEvent.click(
      await waitFor(() => {
        const el = container.querySelector<HTMLElement>('[data-testid="lore-target"]');
        if (!el) throw new Error("no directory target");
        return el;
      }),
    );
    await waitFor(() => rowById(container, "L-2"));
    return container;
  }

  it.each([
    ["the owner", { scope: "owner", sub: "owner" }],
    ["the assistant", { scope: "agent", sub: "mira" }],
  ])("%s opens the menu from the scope tag", async (_label, claims) => {
    const container = await renderAs(jwt(claims));
    await waitFor(() =>
      expect(
        rowById(container, "L-2")
          .querySelector('[data-testid="lore-scope-name"]')!
          .getAttribute("aria-haspopup"),
      ).toBe("menu"),
    );
    fireEvent.click(
      rowById(container, "L-2").querySelector('[data-testid="lore-scope-name"]')!,
    );
    await waitFor(() =>
      expect(
        container.querySelector('[data-testid="lore-scope-options"]')?.textContent,
      ).toBe("適用範圍建立者：Mira預設所有人"),
    );
  });

  it("switching sections asks for the roster, workers and manuals once, and the scope tag stays a menu", async () => {
    const container = await renderAs(jwt({ scope: "agent", sub: "mira" }));
    const real = await import("../api");
    await waitFor(() =>
      expect(
        rowById(container, "L-2").querySelector('[data-testid="lore-scope-name"]')!.getAttribute("aria-haspopup"),
      ).toBe("menu"),
    );
    const counts = () =>
      [real.api.listMembers, real.api.listOutsourceWorkers, real.api.listTaskManuals].map(
        (f) => vi.mocked(f).mock.calls.length,
      );
    const before = counts();
    // A second read would never answer: a page that re-asked would sit without
    // a roster and lose the 適用範圍 menu.
    vi.mocked(real.api.listMembers).mockReturnValue(new Promise(() => {}));
    vi.mocked(real.api.listLoreBudgets).mockResolvedValue([
      MIRA_TARGET,
      { ...MIRA_TARGET, scopeKind: "everyone", scopeKey: "" },
    ]);

    fireEvent.click(container.querySelector('[data-testid="lore-back"]')!);
    await waitFor(() => expect(container.querySelector('[data-testid="lore-target"]')).not.toBeNull());
    fireEvent.click(container.querySelector('[data-testid="lore-target"][data-section="agent:mira"]')!);
    await waitFor(() => expect(container.querySelector('[data-testid="lore-row"]')).not.toBeNull());

    expect(
      rowById(container, "L-2").querySelector('[data-testid="lore-scope-name"]')!.getAttribute("aria-haspopup"),
    ).toBe("menu");
    expect(counts()).toEqual(before);
  });

  it("a plain member reads the scope tag as text and gets no 適用範圍 menu", async () => {
    const container = await renderAs(jwt({ scope: "agent", sub: "m-kyle" }));
    const tag = rowById(container, "L-2").querySelector<HTMLElement>('[data-testid="lore-scope-name"]')!;
    expect(tag.tagName).toBe("SPAN");
    expect(tag.textContent).toBe("Mira");
    expect(tag.hasAttribute("aria-haspopup")).toBe(false);
    fireEvent.click(tag);
    expect(container.querySelector('[data-testid="lore-scope-options"]')).toBeNull();
    expect(container.querySelector('[data-testid="lore-scope-agent"]')).toBeNull();
    expect(container.querySelector('[data-testid="lore-scope-everyone"]')).toBeNull();
  });
});

// ────────────────────────────────────────────────────────────────────────────
// Sections by boot-document target.

const TARGETS: LoreBudgetTargetView[] = [
  { scopeKind: "everyone", scopeKey: "", capChars: 10000, usedChars: 6240, entryCount: 14, droppedCount: 0 },
  { scopeKind: "agent", scopeKey: "mira", capChars: 10000, usedChars: 9640, entryCount: 11, droppedCount: 4 },
  { scopeKind: "agent", scopeKey: "ow-7d8ad859dd9b", capChars: 10000, usedChars: 7120, entryCount: 6, droppedCount: 0 },
  { scopeKind: "manual", scopeKey: "review-pr", capChars: 10000, usedChars: 3210, entryCount: 8, droppedCount: 0 },
];

const MIRA_SEGMENTS = [
  { scopeKind: "everyone" as const, entryCount: 14, chars: 6240, fittedCount: 14, fittedChars: 6240 },
  { scopeKind: "agent" as const, entryCount: 11, chars: 5380, fittedCount: 7, fittedChars: 3400 },
];

function stubTargets() {
  vi.spyOn(api, "listTaskManuals").mockResolvedValue([
    { typeKey: "review-pr", displayName: "PR 審查", purpose: "", fields: [] },
  ] as never);
  vi.spyOn(api, "listOutsourceWorkers").mockResolvedValue([
    { id: "ow-7d8ad859dd9b", codename: "O-179" },
  ] as never);
  return vi.spyOn(api, "listLoreBudgets").mockResolvedValue(TARGETS);
}

function sectionEl(container: HTMLElement, key: string): HTMLElement {
  const el = container.querySelector<HTMLElement>(`[data-testid="lore-section"][data-section="${key}"]`);
  if (!el) throw new Error(`no section ${key}`);
  return el;
}

/** The directory rows' text, top to bottom. */
function headerTexts(container: HTMLElement): string[] {
  return Array.from(container.querySelectorAll('[data-testid="lore-target"]')).map(
    (el) => el.textContent ?? "",
  );
}

/** Each directory group as [heading, open, its cards' section keys]. */
function dirGroups(container: HTMLElement): [string, boolean, string[]][] {
  return Array.from(container.querySelectorAll<HTMLElement>(".lore__group")).map((g) => {
    const heading = g.querySelector<HTMLElement>("button.chat-groups__heading")!;
    return [
      heading.textContent ?? "",
      heading.getAttribute("aria-expanded") === "true",
      Array.from(g.querySelectorAll('[data-testid="lore-target"]')).map((el) => el.getAttribute("data-section") ?? ""),
    ];
  });
}

/** Click a directory row; the section page replaces the directory. */
function openSection(container: HTMLElement, key: string) {
  const target = container.querySelector<HTMLElement>(`[data-testid="lore-target"][data-section="${key}"]`);
  if (!target) throw new Error(`no directory row ${key}`);
  fireEvent.click(target);
}

function searchFor(testId: string, text: string) {
  openFilters();
  const box = document.querySelector<HTMLInputElement>(`[data-testid="${testId}"]`)!;
  fireEvent.change(box, { target: { value: text } });
  fireEvent.keyDown(box, { key: "Enter" });
}

/** Mira's section as the server would answer it, L-212 the first entry it cannot read. */
function miraPage(entries: LoreEntryView[], over: Partial<LoreEntryPageView> = {}): LoreEntryPageView {
  return {
    ...page(entries, { capChars: 10000, firstDroppedId: "L-212" }),
    usedChars: 9640,
    firstDroppedChars: 520,
    budgetSegments: MIRA_SEGMENTS,
    ...over,
  };
}

describe("LorePage — 目錄與段落頁", () => {
  it("lands on the directory: every target with its count, usage and how many cannot be read, and no entries asked for", async () => {
    stubTargets();
    const list = stubList(page([]));
    const { container } = renderPage();

    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    await waitFor(() =>
      expect(headerTexts(container)).toEqual([
        "所有人14 筆全部讀得到6,240 字・每位成員都會讀",
        "Mira11 筆4 筆讀不到9,640 / 10,000 字",
        "O-1796 筆全部讀得到7,120 / 10,000 字",
        "PR 審查8 筆全部讀得到3,210 / 10,000 字",
      ]),
    );
    expect(dirGroups(container)).toEqual([
      ["所有人 · 1", true, ["everyone:"]],
      ["正職成員 · 1", true, ["agent:mira"]],
      ["外包成員 · 1", true, ["agent:ow-7d8ad859dd9b"]],
      ["任務手冊 · 1", true, ["manual:review-pr"]],
    ]);
    // The bar is used / cap of the target's own cap; 所有人 has no cap of its own, so no bar.
    expect(
      Array.from(container.querySelectorAll('[data-testid="lore-target"]')).map(
        (el) => (el.querySelector(".lore-dir-card__bar-fill") as HTMLElement | null)?.style.width ?? null,
      ),
    ).toEqual([null, "96%", "71%", "32%"]);
    // Each row is a link to its own section page.
    expect(
      Array.from(container.querySelectorAll('[data-testid="lore-target"]')).map((el) => el.getAttribute("href")),
    ).toEqual(["#lore/everyone", "#lore/agent/mira", "#lore/agent/ow-7d8ad859dd9b", "#lore/manual/review-pr"]);
    expect(container.querySelector('[data-testid="lore-section-dropped"]')!.textContent).toBe(
      "4 筆讀不到",
    );
    // Filters belong to a section page.
    expect(container.querySelector('[data-testid="lore-filter"]')).toBeNull();
    expect(renderedIds(container)).toEqual([]);
    expect(list).not.toHaveBeenCalled();
  });

  it("a directory card's bar stops at full when a lowered cap leaves the target over it, and a zero cap shows only the size; 全部收合 folds only the groups shown and then reads 全部展開", async () => {
    stubTargets();
    vi.spyOn(api, "listLoreBudgets").mockResolvedValue([
      { scopeKind: "agent", scopeKey: "mira", capChars: 1000, usedChars: 1300, entryCount: 3, droppedCount: 1 },
      { scopeKind: "manual", scopeKey: "review-pr", capChars: 0, usedChars: 320, entryCount: 2, droppedCount: 0 },
    ]);
    stubList(page([]));
    const { container } = renderPage();

    await waitFor(() => expect(headerTexts(container)).toHaveLength(2));
    expect(dirGroups(container).map(([title]) => title)).toEqual(["正職成員 · 1", "任務手冊 · 1"]);
    const cards = Array.from(container.querySelectorAll('[data-testid="lore-target"]'));
    expect((cards[0].querySelector(".lore-dir-card__bar-fill") as HTMLElement).style.width).toBe("100%");
    expect(cards[1].querySelector(".lore-dir-card__bar-fill")).toBeNull();
    expect(cards[1].querySelector('[data-testid="lore-section-usage"]')!.textContent).toBe("320 字");

    const button = container.querySelector<HTMLElement>('[data-testid="lore-collapse-all"]')!;
    fireEvent.click(button);

    expect(dirGroups(container).map(([, open]) => open)).toEqual([false, false]);
    expect(button.getAttribute("aria-label")).toBe("全部展開");
    expect(JSON.parse(localStorage.getItem(LORE_DIR_COLLAPSED_KEY)!)).toEqual(["staff", "manual"]);
  });

  it("a first visit opens only 所有人, with 正職成員, 外包成員 and 任務手冊 folded", async () => {
    localStorage.removeItem(LORE_DIR_COLLAPSED_KEY);
    stubTargets();
    stubList(page([]));
    const { container } = renderPage();

    await waitFor(() => expect(dirGroups(container)).toHaveLength(4));
    expect(dirGroups(container)).toEqual([
      ["所有人 · 1", true, ["everyone:"]],
      ["正職成員 · 1", false, []],
      ["外包成員 · 1", false, []],
      ["任務手冊 · 1", false, []],
    ]);
    expect(container.querySelector('[data-testid="lore-collapse-all"]')!.getAttribute("aria-label")).toBe("全部收合");
  });

  it("a group folded or opened from its heading stays that way on the next visit", async () => {
    localStorage.removeItem(LORE_DIR_COLLAPSED_KEY);
    stubTargets();
    stubList(page([]));
    const first = renderPage();
    await waitFor(() => expect(dirGroups(first.container)).toHaveLength(4));

    fireEvent.click(first.container.querySelector('[data-testid="lore-group-heading-outsource"]')!);
    fireEvent.click(first.container.querySelector('[data-testid="lore-group-heading-everyone"]')!);

    expect(dirGroups(first.container).map(([, open]) => open)).toEqual([false, false, true, false]);
    expect(first.container.querySelectorAll('[data-testid="lore-target"]')).toHaveLength(1);
    expect(JSON.parse(localStorage.getItem(LORE_DIR_COLLAPSED_KEY)!)).toEqual(["everyone", "staff", "manual"]);
    first.unmount();

    const again = renderPage();
    await waitFor(() => expect(dirGroups(again.container)).toHaveLength(4));
    expect(dirGroups(again.container)).toEqual([
      ["所有人 · 1", false, []],
      ["正職成員 · 1", false, []],
      ["外包成員 · 1", true, ["agent:ow-7d8ad859dd9b"]],
      ["任務手冊 · 1", false, []],
    ]);
  });

  it("全部收合 folds every group, then 全部展開 opens them all, and both are remembered", async () => {
    localStorage.removeItem(LORE_DIR_COLLAPSED_KEY);
    stubTargets();
    stubList(page([]));
    const { container } = renderPage();
    await waitFor(() => expect(dirGroups(container)).toHaveLength(4));
    const button = container.querySelector<HTMLElement>('[data-testid="lore-collapse-all"]')!;

    fireEvent.click(button);

    expect(dirGroups(container).map(([, open]) => open)).toEqual([false, false, false, false]);
    expect(headerTexts(container)).toEqual([]);
    expect(button.getAttribute("aria-label")).toBe("全部展開");
    expect(JSON.parse(localStorage.getItem(LORE_DIR_COLLAPSED_KEY)!)).toEqual(["everyone", "staff", "outsource", "manual"]);

    fireEvent.click(button);

    expect(dirGroups(container).map(([, open]) => open)).toEqual([true, true, true, true]);
    expect(headerTexts(container)).toHaveLength(4);
    expect(button.getAttribute("aria-label")).toBe("全部收合");
    expect(JSON.parse(localStorage.getItem(LORE_DIR_COLLAPSED_KEY)!)).toEqual([]);
  });

  it("with storage refusing every read and write, the directory opens on the first-visit folding, still folds, and keeps it across a trip to a section page", async () => {
    const denied = () => {
      throw new Error("denied");
    };
    vi.spyOn(window.localStorage, "getItem").mockImplementation(denied);
    vi.spyOn(window.localStorage, "setItem").mockImplementation(denied);
    stubTargets();
    stubList(page([]));
    const { container } = renderPage();
    await waitFor(() => expect(dirGroups(container)).toHaveLength(4));
    expect(dirGroups(container).map(([, open]) => open)).toEqual([true, false, false, false]);

    const thrown: unknown[] = [];
    const onError = (e: ErrorEvent) => thrown.push(e.error);
    window.addEventListener("error", onError);
    try {
      fireEvent.click(container.querySelector('[data-testid="lore-group-heading-staff"]')!);
    } finally {
      window.removeEventListener("error", onError);
    }

    expect(thrown).toEqual([]);
    expect(dirGroups(container)[1]).toEqual(["正職成員 · 1", true, ["agent:mira"]]);

    openSection(container, "agent:mira");
    await waitFor(() => expect(container.querySelector('[data-testid="lore-back"]')).not.toBeNull());
    fireEvent.click(container.querySelector('[data-testid="lore-back"]')!);
    await waitFor(() => expect(dirGroups(container)).toHaveLength(4));
    expect(dirGroups(container).map(([, open]) => open)).toEqual([true, true, false, false]);
  });

  it("a manual section's head shows its chars, its cap and only its own count, and its name opens that manual", async () => {
    stubTargets();
    stubList({
      ...page([mkEntry({ id: "L-8", scopeKind: "manual", scopeKey: "review-pr" })], { capChars: 1150, firstDroppedId: "" }),
      usedChars: 1131,
      budgetSegments: [{ scopeKind: "manual", entryCount: 5, chars: 1400, fittedCount: 3, fittedChars: 1131 }],
    });
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    openSection(container, "manual:review-pr");
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-8"]));
    const head = container.querySelector<HTMLElement>('[data-testid="lore-section-head"]')!;
    expect(head.querySelector('[data-testid="lore-section-used"]')!.textContent).toBe("1,131 字");
    expect(head.querySelector('[data-testid="lore-section-cap"]')!.textContent).toBe("上限 1,150");
    expect(head.querySelector('[data-testid="lore-section-summary"]')!.textContent).toBe("PR 審查 3/5");
    fireEvent.click(container.querySelector('[data-testid="lore-section-name"]')!);
    expect(window.location.hash).toBe("#settings/manuals/review-pr");
  });

  it("the 所有人 section page shows only its size, never a cap or a bar it does not have", async () => {
    stubTargets();
    stubList({
      ...page([mkEntry({ id: "L-3", scopeKind: "everyone", scopeKey: "" })], { capChars: 10000, firstDroppedId: "" }),
      usedChars: 6240,
      budgetSegments: [
        { scopeKind: "everyone", entryCount: 14, chars: 6240, fittedCount: 14, fittedChars: 6240 },
      ],
    });
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));

    openSection(container, "everyone:");

    await waitFor(() => expect(renderedIds(container)).toEqual(["L-3"]));
    const head = container.querySelector<HTMLElement>('[data-testid="lore-section-head"]')!;
    expect(head.querySelector('[data-testid="lore-section-bar"]')).toBeNull();
    expect(head.querySelector('[data-testid="lore-section-summary"]')).toBeNull();
    expect(head.querySelector('[data-testid="lore-section-usage"]')!.textContent).toBe(
      "6,240 字・每位成員都會讀",
    );
  });

  it("opening a member's section lists 所有人's entries one by one among the member's own in the server's order, tags them 所有人, and greys every card the server marks dropped, 所有人's included", async () => {
    stubTargets();
    const everyone = { scopeKind: "everyone" as const, scopeKey: "" };
    const list = stubList(
      miraPage(
        [
          mkEntry({ id: "L-198", state: "pinned", dropped: false }),
          mkEntry({ id: "L-240", ...everyone, dropped: false }),
          mkEntry({ id: "L-231", dropped: false }),
          mkEntry({ id: "L-219", ...everyone, dropped: true }),
          mkEntry({ id: "L-212", dropped: true }),
          mkEntry({ id: "L-205", ...everyone, dropped: true }),
        ],
        {
          usedChars: 9400,
          firstDroppedId: "L-219",
          firstDroppedChars: 900,
          budgetSegments: [
            { scopeKind: "everyone", entryCount: 14, chars: 6240, fittedCount: 12, fittedChars: 6000 },
            { scopeKind: "agent", entryCount: 11, chars: 5380, fittedCount: 7, fittedChars: 3400 },
          ],
        },
      ),
    );
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));

    openSection(container, "agent:mira");

    const body = await waitFor(() => {
      const el = sectionEl(container, "agent:mira").querySelector<HTMLElement>(
        '[data-testid="lore-section-body"]',
      );
      if (!el || renderedIds(el).length === 0) throw new Error("section not loaded");
      return el;
    });
    expect(window.location.hash).toBe("#lore/agent/mira");
    expect(container.querySelector('[data-testid="lore-target"]')).toBeNull();
    expect(container.querySelector('[data-testid="lore-back"]')!.getAttribute("href")).toBe("#lore");
    expect(
      Array.from(container.querySelector('[data-testid="lore-page"]')!.children).map((el) => el.getAttribute("data-testid")),
    ).toEqual(["lore-back", "lore-section-head", "lore-filter", "lore-section"]);
    const head = container.querySelector<HTMLElement>('[data-testid="lore-section-head"]')!;
    expect(head.querySelector('[data-testid="lore-filter-toggle"]')).toBeNull();
    expect(head.querySelector('[data-testid="lore-section-name"]')!.textContent).toBe("Mira");
    expect(head.querySelector('[data-testid="lore-section-count"]')).toBeNull();
    expect(screen.getByTestId("lore-filter-summary").textContent).toBe("6 筆傳承");
    expect(head.querySelector('[data-testid="lore-section-dropped"]')!.textContent).toBe("4 筆讀不到");
    expect(list.mock.calls).toEqual([
      [{ states: ["pinned", "active"], scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], limit: 30, offset: 0 }],
    ]);
    expect(head.querySelector<HTMLElement>('[data-testid="lore-bar-everyone"]')!.style.width).toBe("60%");
    expect(head.querySelector<HTMLElement>('[data-testid="lore-bar-own"]')!.style.width).toBe("34%");
    expect(head.querySelector('[data-testid="lore-bar-everyone"]')!.className).toBe("lore-dir-card__seg-everyone");
    expect(head.querySelector('[data-testid="lore-bar-own"]')!.className).toBe("lore-dir-card__bar-fill");
    expect(head.querySelector('[data-testid="lore-legend-everyone"] i')!.className).toBe(
      "lore-dir-card__swatch lore-dir-card__swatch--everyone",
    );
    expect(head.querySelector('[data-testid="lore-legend-own"] i')!.className).toBe("lore-dir-card__swatch");
    const css = readFileSync(join(__dirname, "lore.css"), "utf8");
    const ruleOf = (selector: string) => {
      const start = css.indexOf(selector);
      return css.slice(start, css.indexOf("}", start));
    };
    expect(ruleOf(".lore-dir-card__seg-everyone,")).toContain(".lore-dir-card__swatch.lore-dir-card__swatch--everyone {");
    expect(ruleOf(".lore-dir-card__bar-fill {")).toContain("background: var(--color-accent);");
    expect(ruleOf(".lore-dir-card__swatch {")).toContain("background: var(--color-accent);");
    expect(head.querySelector('[data-testid="lore-section-used"]')!.textContent).toBe("9,400 字");
    expect(head.querySelector('[data-testid="lore-section-used"] b')!.textContent).toBe("9,400");
    expect(head.querySelector('[data-testid="lore-section-cap"]')!.textContent).toBe("上限 10,000");
    expect(head.querySelector('[data-testid="lore-section-summary"]')!.textContent).toBe("所有人 12/14 · Mira 7/11");
    expect(container.querySelector('[data-testid="lore-everyone-ref"]')).toBeNull();
    expect(
      renderedIds(body).map((id) => [
        id,
        rowById(body, id).querySelector('[data-testid="lore-scope-name"]')?.textContent ?? null,
        rowById(body, id).dataset.dimmed,
      ]),
    ).toEqual([
      ["L-198", "Mira", "false"],
      ["L-240", "所有人", "false"],
      ["L-231", "Mira", "false"],
      ["L-219", null, "true"],
      ["L-212", null, "true"],
      ["L-205", null, "true"],
    ]);
  });

  it("a card on a later page is greyed by its own dropped", async () => {
    stubTargets();
    const first = Array.from({ length: 30 }, (_, i) => mkEntry({ id: `L-${100 + i}`, dropped: false }));
    const list = vi
      .spyOn(api, "listLoreEntries")
      .mockImplementation(async (o) =>
        (o?.offset ?? 0) === 0
          ? miraPage(first)
          : miraPage([mkEntry({ id: "L-212", dropped: true }), mkEntry({ id: "L-213", dropped: true })], { offset: 30 }),
      );
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    openSection(container, "agent:mira");
    await waitFor(() => expect(renderedIds(container)).toHaveLength(30));
    expect(
      Array.from(container.querySelectorAll('[data-testid="lore-row"]')).filter(
        (el) => (el as HTMLElement).dataset.dimmed === "true",
      ),
    ).toEqual([]);

    fireEvent.scroll(container.querySelector('[data-testid="lore-page"]')!);

    await waitFor(() => expect(renderedIds(container)).toHaveLength(32));
    expect(list.mock.calls[1]).toEqual([
      { states: ["pinned", "active"], scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], limit: 30, offset: 30 },
    ]);
    expect(rowById(container, "L-129").dataset.dimmed).toBe("false");
    expect(rowById(container, "L-212").dataset.dimmed).toBe("true");
    expect(rowById(container, "L-213").dataset.dimmed).toBe("true");
  });

  it("when 所有人's newer entries fill the cap, the bar is all 所有人 and every card the server marks dropped is greyed, 所有人's or the member's own", async () => {
    stubTargets();
    const everyone = { scopeKind: "everyone" as const, scopeKey: "" };
    stubList(
      miraPage([
        mkEntry({ id: "L-240", ...everyone, dropped: false }),
        mkEntry({ id: "L-231", ...everyone, dropped: true }),
        mkEntry({ id: "L-212", dropped: true }),
      ], {
        usedChars: 9800,
        firstDroppedId: "L-231",
        firstDroppedChars: 300,
        budgetSegments: [
          { scopeKind: "everyone", entryCount: 20, chars: 12400, fittedCount: 14, fittedChars: 9800 },
          { scopeKind: "agent", entryCount: 2, chars: 600, fittedCount: 0, fittedChars: 0 },
        ],
      }),
    );
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));

    openSection(container, "agent:mira");

    const body = await waitFor(() => {
      const el = sectionEl(container, "agent:mira").querySelector<HTMLElement>(
        '[data-testid="lore-section-body"]',
      );
      if (!el || renderedIds(el).length === 0) throw new Error("section not loaded");
      return el;
    });
    const head = container.querySelector<HTMLElement>('[data-testid="lore-section-head"]')!;
    expect(head.querySelector('[data-testid="lore-section-usage-row"]')!.textContent).toBe("9,800 字上限 10,000");
    expect(head.querySelector('[data-testid="lore-section-summary"]')!.textContent).toBe("所有人 14/20 · Mira 0/2");
    expect(head.querySelector<HTMLElement>('[data-testid="lore-bar-everyone"]')!.style.width).toBe("98%");
    expect(head.querySelector<HTMLElement>('[data-testid="lore-bar-own"]')!.style.width).toBe("0%");
    expect(renderedIds(body).map((id) => [id, rowById(body, id).dataset.dimmed])).toEqual([
      ["L-240", "false"],
      ["L-231", "true"],
      ["L-212", "true"],
    ]);
  });

  it("a title search greys the results the server marks dropped though the first entry it cannot read is not among them", async () => {
    stubTargets();
    vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      o?.q === "審查" && o.scopeKeys?.[0] === "mira"
        ? miraPage([mkEntry({ id: "L-231", dropped: false }), mkEntry({ id: "L-205", dropped: true })])
        : page([]),
    );
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    openSection(container, "agent:mira");
    await openFiltersWhenReady();

    searchFor("lore-filter-id", "審查");

    await waitFor(() => expect(renderedIds(container)).toEqual(["L-231", "L-205"]));
    expect(renderedIds(container).map((id) => rowById(container, id).dataset.dimmed)).toEqual([
      "false",
      "true",
    ]);
  });

  it("hides 已失效 entries by default and shows them at the end of the section once 已失效 is ticked", async () => {
    stubTargets();
    vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      miraPage(
        [
          mkEntry({ id: "L-1", state: "pinned" }),
          mkEntry({ id: "L-2" }),
          mkEntry({ id: "L-9", state: "retired", retireReason: "併進 SOP" }),
        ].filter((e) => o?.states?.includes(e.state) ?? true),
        { firstDroppedId: "", firstDroppedChars: 0 },
      ),
    );
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    openSection(container, "agent:mira");
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-1", "L-2"]));

    openFilters();
    fireEvent.click(document.querySelector<HTMLElement>('[data-testid="lore-filter-state"]')!);
    fireEvent.click(
      document.querySelector<HTMLElement>('[data-testid="lore-filter-state-opt-retired"] input')!,
    );

    await waitFor(() => expect(renderedIds(container)).toEqual(["L-1", "L-2", "L-9"]));
    // Its reason is its own line under the meta.
    expect(rowById(container, "L-9").querySelector('[data-testid="lore-retire-reason"]')!.textContent).toBe(
      "失效理由：併進 SOP",
    );
  });

  it("an id typed on the directory opens that entry's section page with the entry highlighted, as a full card even when it cannot be read", async () => {
    stubTargets();
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      o?.scopeKeys === undefined
        ? page([mkEntry({ id: "L-212", scopeKind: "agent", scopeKey: "mira" })])
        : miraPage([mkEntry({ id: "L-198", dropped: true }), mkEntry({ id: "L-212", dropped: true })]),
    );
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));

    searchFor("lore-directory-id", "212");

    await waitFor(() => expect(window.location.hash).toBe("#lore/agent/mira/entry/L-212"));
    expect(list.mock.calls[0][0]).toEqual({
      entryIds: ["L-212"], states: ["pinned", "active", "retired"], limit: 1, offset: 0,
    });
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-198", "L-212"]));
    const row = rowById(container, "L-212");
    expect(row.classList.contains("lore-row--highlight")).toBe(true);
    expect(row.dataset.folded).toBeUndefined();
    expect(row.querySelector('[data-testid="lore-fold-mark"]')).not.toBeNull();
    expect(rowById(container, "L-198").classList.contains("lore-row--highlight")).toBe(false);
    expect(rowById(container, "L-198").dataset.folded).toBe("true");
    await waitFor(() => expect(document.activeElement).toBe(row));

    window.location.hash = "#lore/agent/mira/entry/L-198";
    await waitFor(() => expect(rowById(container, "L-198").dataset.folded).toBeUndefined());
    await waitFor(() => expect(document.activeElement).toBe(rowById(container, "L-198")));
  });

  it("a highlighted entry beyond the first page is paged in", async () => {
    stubTargets();
    const first = Array.from({ length: 30 }, (_, i) => mkEntry({ id: `L-${100 + i}` }));
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      (o?.offset ?? 0) === 0 ? miraPage(first) : miraPage([mkEntry({ id: "L-212" })], { offset: 30 }),
    );
    window.location.hash = "#lore/agent/mira/entry/L-212";
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(31));
    expect(list.mock.calls.map((c) => c[0]?.offset)).toEqual([0, 30]);
    expect(rowById(container, "L-212").classList.contains("lore-row--highlight")).toBe(true);
    expect(container.querySelector('[data-testid="lore-highlight-missing"]')).toBeNull();
  });

  it("a retired entry found from the directory opens its section with 已失效 ticked, so it is listed without paging", async () => {
    stubTargets();
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      o?.scopeKeys === undefined
        ? page([mkEntry({ id: "L-9", state: "retired" })])
        : miraPage(
            [mkEntry({ id: "L-198" }), mkEntry({ id: "L-9", state: "retired" })].filter((e) =>
              o?.states?.includes(e.state),
            ),
          ),
    );
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    searchFor("lore-directory-id", "L-9");

    await waitFor(() => expect(renderedIds(container)).toEqual(["L-198", "L-9"]));
    expect(list.mock.calls.slice(1).map((c) => c[0])).toEqual([
      { states: ["pinned", "active", "retired"], scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], limit: 30, offset: 0 },
    ]);
    expect(rowById(container, "L-9").classList.contains("lore-row--highlight")).toBe(true);
  });

  it("an id whose section is gone says so once and stays on the directory", async () => {
    stubTargets();
    stubList(page([mkEntry({ id: "L-5", scopeKind: "agent", scopeKey: "m-left" })]));
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    searchFor("lore-directory-id", "L-5");
    await waitFor(() =>
      expect(container.querySelector('[data-testid="lore-directory-note"]')?.textContent).toBe(
        "L-5 所在的段落已經不在了（成員已離開或手冊已刪除）",
      ),
    );
    expect(window.location.hash).toBe("");
  });

  it("paging for a highlight stops at the cap and says the entry is not there", async () => {
    stubTargets();
    let n = 0;
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      miraPage(Array.from({ length: 30 }, () => mkEntry({ id: `L-${1000 + n++}` })), { offset: o?.offset ?? 0 }),
    );
    window.location.hash = "#lore/agent/mira/entry/L-9";
    const { container } = renderPage();
    // Eleven reads in a row; give a loaded machine room.
    await waitFor(() => expect(container.querySelector('[data-testid="lore-highlight-missing"]')).not.toBeNull(), {
      timeout: 10000,
    });
    // The first page and ten more, then it gives up.
    expect(list).toHaveBeenCalledTimes(11);
    await new Promise((r) => setTimeout(r, 20));
    expect(list).toHaveBeenCalledTimes(11);
  }, 15000);

  it("paging for a highlight stops once the reader changes a filter", async () => {
    stubTargets();
    const page2 = held<LoreEntryPageView>();
    let n = 0;
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      (o?.offset ?? 0) === 30
        ? page2.promise
        : miraPage(Array.from({ length: 30 }, () => mkEntry({ id: `L-${1000 + n++}` }))),
    );
    window.location.hash = "#lore/agent/mira/entry/L-9";
    renderPage();
    await waitFor(() => expect(list).toHaveBeenCalledTimes(2));

    openFilters();
    fireEvent.click(document.querySelector<HTMLElement>('[data-testid="lore-filter-state"]')!);
    fireEvent.click(document.querySelector<HTMLElement>('[data-testid="lore-filter-state-opt-retired"] input')!);
    await waitFor(() => expect(list).toHaveBeenCalledTimes(3));
    page2.resolve(miraPage([]));
    await new Promise((r) => setTimeout(r, 30));

    expect(list.mock.calls.map((c) => c[0]?.offset)).toEqual([0, 30, 0]);
  });

  it("a pasted link that gave up does not stop the directory search for the same entry from paging to it", async () => {
    stubTargets();
    const thirty = (from: number) => Array.from({ length: 30 }, (_, i) => mkEntry({ id: `L-${from + i}` }));
    vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) => {
      if (o?.scopeKeys === undefined) return page([mkEntry({ id: "L-9", state: "retired" })]);
      const withRetired = o.states?.includes("retired");
      if (!withRetired) return miraPage([mkEntry({ id: "L-198" })]);
      return (o.offset ?? 0) === 0 ? miraPage(thirty(100)) : miraPage([mkEntry({ id: "L-9", state: "retired" })]);
    });
    window.location.hash = "#lore/agent/mira/entry/L-9";
    const { container } = renderPage();
    await waitFor(() => expect(container.querySelector('[data-testid="lore-highlight-missing"]')).not.toBeNull());

    fireEvent.click(container.querySelector('[data-testid="lore-back"]')!);
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    searchFor("lore-directory-id", "L-9");

    await waitFor(() => expect(renderedIds(container)).toHaveLength(31));
    expect(rowById(container, "L-9").classList.contains("lore-row--highlight")).toBe(true);
    expect(container.querySelector('[data-testid="lore-highlight-missing"]')).toBeNull();
  });

  it("a page that fails to load while paging for a highlight stops and says loading failed", async () => {
    stubTargets();
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) => {
      if ((o?.offset ?? 0) === 30) throw new Error("network");
      return miraPage(Array.from({ length: 30 }, (_, i) => mkEntry({ id: `L-${100 + i}` })));
    });
    window.location.hash = "#lore/agent/mira/entry/L-9";
    const { container } = renderPage();
    await waitFor(() =>
      expect(container.querySelector('[data-testid="lore-highlight-failed"]')?.textContent).toBe(
        "載入更多條目失敗，還沒找到 L-9；往下捲可以再試一次",
      ),
    );
    await new Promise((r) => setTimeout(r, 30));
    expect(list).toHaveBeenCalledTimes(2);
    expect(container.querySelector('[data-testid="lore-highlight-missing"]')).toBeNull();
  });

  it("a highlighted entry the section does not list says so", async () => {
    stubTargets();
    stubList(miraPage([mkEntry({ id: "L-198" })]));
    window.location.hash = "#lore/agent/mira/entry/L-9";
    const { container } = renderPage();
    await waitFor(() =>
      expect(container.querySelector('[data-testid="lore-highlight-missing"]')?.textContent).toBe(
        "L-9 不在目前的篩選結果裡（已失效的條目要勾「已失效」才會列出）",
      ),
    );
  });

  it("the directory search takes ids only: a title asks nothing and says where title search is; an unknown id says so", async () => {
    stubTargets();
    const list = stubList(page([]));
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));

    searchFor("lore-directory-id", "審查");
    expect(container.querySelector('[data-testid="lore-directory-note"]')!.textContent).toBe(
      "這裡只找條目編號（例如 L-212）；要用標題找，先進到一段裡",
    );
    expect(list).not.toHaveBeenCalled();

    searchFor("lore-directory-id", "L-999");
    await waitFor(() =>
      expect(container.querySelector('[data-testid="lore-directory-note"]')!.textContent).toBe("找不到 L-999"),
    );
    expect(window.location.hash).toBe("");
  });

  it("the section page's search box sends an id as entryIds and any other text as a title search q, within that section", async () => {
    stubTargets();
    const list = stubList(page([]));
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    openSection(container, "agent:mira");
    await waitFor(() => expect(list).toHaveBeenCalled());
    const sentFor = async (typed: string) => {
      list.mockClear();
      searchFor("lore-filter-id", typed);
      await waitFor(() => expect(list).toHaveBeenCalled());
      const { entryIds, q, scopeKeys } = list.mock.calls[0][0]!;
      return { entryIds, q, scopeKeys };
    };

    expect(await sentFor("L-212")).toEqual({ entryIds: ["L-212"], q: undefined, scopeKeys: ["mira"] });
    expect(await sentFor(" l-213 ")).toEqual({ entryIds: ["L-213"], q: undefined, scopeKeys: ["mira"] });
    expect(await sentFor("214")).toEqual({ entryIds: ["L-214"], q: undefined, scopeKeys: ["mira"] });
    expect(await sentFor(" 審查 PR ")).toEqual({ entryIds: undefined, q: "審查 PR", scopeKeys: ["mira"] });
    expect(await sentFor("L-2a")).toEqual({ entryIds: undefined, q: "L-2a", scopeKeys: ["mira"] });
  });

  it("a search nothing matches says so", async () => {
    stubTargets();
    stubList(page([]));
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    openSection(container, "agent:mira");
    await openFiltersWhenReady();

    searchFor("lore-filter-id", "L-999");

    await waitFor(() =>
      expect(container.querySelector('[data-testid="lore-empty"]')?.textContent).toBe(
        "沒有符合篩選條件的傳承",
      ),
    );
    expect(renderedIds(container)).toEqual([]);
  });

  it("a section's hash opens its page directly, loads only that section, and the back link returns to the directory with filters reset", async () => {
    stubTargets();
    const list = stubList(page([mkEntry({ id: "L-1", scopeKind: "manual", scopeKey: "review-pr" })]));
    window.location.hash = "#lore/manual/review-pr";
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-1"]));
    expect(list.mock.calls.map((c) => c[0])).toEqual([
      { states: ["pinned", "active"], scopeKinds: ["manual"], scopeKeys: ["review-pr"], limit: 30, offset: 0 },
    ]);
    expect(container.querySelector('[data-testid="lore-section-name"]')!.textContent).toBe("PR 審查");

    searchFor("lore-filter-id", "審查");
    await waitFor(() => expect(list).toHaveBeenCalledTimes(2));

    fireEvent.click(container.querySelector('[data-testid="lore-back"]')!);
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    expect(window.location.hash).toBe("#lore");

    openSection(container, "agent:mira");
    await waitFor(() => expect(list).toHaveBeenCalledTimes(3));
    expect(list.mock.calls[2][0]).toEqual({
      states: ["pinned", "active"], scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], limit: 30, offset: 0,
    });
    openFilters();
    expect(document.querySelector<HTMLInputElement>('[data-testid="lore-filter-id"]')!.value).toBe("");
  });

  it("a page re-render between the URL change and its hashchange does not load the new section into the old page", async () => {
    stubTargets();
    const list = stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    // The URL moves first; the hashchange comes later. Typing re-renders the
    // directory in between.
    history.pushState(null, "", window.location.pathname + "#lore/agent/mira");
    fireEvent.change(container.querySelector('[data-testid="lore-directory-id"]')!, { target: { value: "2" } });
    await new Promise((r) => setTimeout(r, 20));
    expect(list).not.toHaveBeenCalled();

    window.dispatchEvent(new HashChangeEvent("hashchange"));
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-1"]));
    expect(list).toHaveBeenCalledTimes(1);
  });

  it("a section the server does not list says so and offers the way back", async () => {
    stubTargets();
    const list = stubList(page([]));
    window.location.hash = "#lore/agent/nobody";
    const { container } = renderPage();
    await waitFor(() => expect(container.querySelector('[data-testid="lore-section-missing"]')).not.toBeNull());
    expect(container.querySelector('[data-testid="lore-back"]')).not.toBeNull();
    expect(list).not.toHaveBeenCalled();
  });
});

describe("LorePage — 就地編輯", () => {
  const rows = () => [
    mkEntry({ id: "L-1", title: "第一筆", body: "一", dropped: false }),
    mkEntry({ id: "L-2", title: "AAAA", body: "BBBBBBBBBBBBBBBB", dropped: false }),
    mkEntry({ id: "L-3", title: "第三筆", body: "三", dropped: true }),
  ];
  const cap = { capChars: 1000, firstDroppedId: "L-3" };

  it("saving sends at once, re-reads the whole section with its numbers, and a change staged on another card stays staged", async () => {
    const edit = vi.spyOn(api, "editLoreEntry").mockResolvedValue(undefined);
    const state = vi.spyOn(api, "setLoreEntryState").mockResolvedValue(undefined);
    let edited = false;
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async () =>
      edited
        ? {
            ...page([rows()[0], mkEntry({ id: "L-2", title: "AAAA", body: "BB", dropped: false }), { ...rows()[2], dropped: false }], cap),
            usedChars: 976,
            firstDroppedChars: 0,
            firstDroppedId: "",
          }
        : { ...page(rows(), cap), usedChars: 990, firstDroppedChars: 20 },
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-1", "L-2", "L-3"]));
    expect(rowById(container, "L-3").dataset.dimmed).toBe("true");
    fireEvent.click(rowById(container, "L-1").querySelector('[data-testid="lore-state"]')!);
    fireEvent.click(rowById(container, "L-1").querySelector('[data-testid="lore-state-retired"]')!);

    fireEvent.click(rowById(container, "L-2").querySelector('[data-testid="lore-edit"]')!);
    const editor = rowById(container, "L-2").querySelector<HTMLElement>('[data-testid="lore-editor"]')!;
    expect(editor.querySelector<HTMLInputElement>('[data-testid="lore-edit-title"]')!.value).toBe("AAAA");
    fireEvent.change(editor.querySelector('[data-testid="lore-edit-body"]')!, { target: { value: "BB" } });
    edited = true;
    fireEvent.click(editor.querySelector('[data-testid="lore-edit-save"]')!);

    await waitFor(() =>
      expect(rowById(container, "L-2").querySelector('[data-testid="lore-editor"]')).toBeNull(),
    );
    expect(edit.mock.calls).toEqual([["L-2", { body: "BB" }]]);
    expect(rowById(container, "L-2").querySelector('[data-testid="lore-body"]')!.textContent).toBe("BB");
    expect(renderedIds(container)).toEqual(["L-1", "L-2", "L-3"]);
    expect(list.mock.calls.map((c) => c[0])).toEqual([
      { states: ["pinned", "active"], scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], limit: 30, offset: 0 },
      { states: ["pinned", "active"], scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], limit: 30, offset: 0 },
    ]);
    await waitFor(() => expect(rowById(container, "L-3").dataset.dimmed).toBe("false"));
    expect(state).not.toHaveBeenCalled();
    expect(rowById(container, "L-1").querySelector('[data-testid="lore-state"]')!.textContent).toBe("已失效");
    expect(rowById(container, "L-1").dataset.staged).toBe("true");
    expect(container.querySelector('[data-testid="lore-staged-count"]')!.textContent).toBe("1 項變更還沒儲存");
  });

  it("edits title and body only: no type control, no character counters, no effect sentence", async () => {
    const edit = vi.spyOn(api, "editLoreEntry").mockResolvedValue(undefined);
    stubList({ ...page(rows(), cap), usedChars: 990, firstDroppedChars: 4 });
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(3));

    fireEvent.click(rowById(container, "L-3"));
    fireEvent.click(rowById(container, "L-3").querySelector('[data-testid="lore-edit"]')!);
    const editor = rowById(container, "L-3").querySelector<HTMLElement>('[data-testid="lore-editor"]')!;
    expect(
      Array.from(editor.querySelectorAll("input, textarea")).map((el) => el.getAttribute("data-testid")),
    ).toEqual(["lore-edit-title", "lore-edit-body"]);
    expect(editor.querySelector('[role="radiogroup"], [data-testid^="lore-edit-type"]')).toBeNull();
    expect(editor.querySelector('[data-testid$="-count"], [data-testid="lore-edit-effect"]')).toBeNull();
    expect(editor.textContent).not.toMatch(/\d+ \/ \d+/);

    fireEvent.change(editor.querySelector('[data-testid="lore-edit-title"]')!, { target: { value: "新標題" } });
    fireEvent.click(editor.querySelector('[data-testid="lore-edit-save"]')!);
    await waitFor(() => expect(edit).toHaveBeenCalled());
    expect(edit.mock.calls).toEqual([["L-3", { title: "新標題" }]]);
  });

  it("an entry already over a lowered cap saves a shorter or untouched field and refuses one that grows", async () => {
    const edit = vi.spyOn(api, "editLoreEntry").mockResolvedValue(undefined);
    const longBody = "長".repeat(1300);
    stubList(page([mkEntry({ id: "L-2", title: "AAAA", body: longBody })], cap));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-2"]));

    fireEvent.click(rowById(container, "L-2").querySelector('[data-testid="lore-edit"]')!);
    const editor = rowById(container, "L-2").querySelector<HTMLElement>('[data-testid="lore-editor"]')!;
    const save = editor.querySelector<HTMLButtonElement>('[data-testid="lore-edit-save"]')!;
    fireEvent.change(editor.querySelector('[data-testid="lore-edit-title"]')!, { target: { value: " BBBB " } });
    expect(save.disabled).toBe(false);
    expect(editor.querySelector('[data-testid="lore-edit-error"]')).toBeNull();

    fireEvent.change(editor.querySelector('[data-testid="lore-edit-body"]')!, { target: { value: longBody + "長" } });
    expect(save.disabled).toBe(true);
    expect(editor.querySelector('[data-testid="lore-edit-error"]')!.textContent).toBe("超過字數上限");

    fireEvent.change(editor.querySelector('[data-testid="lore-edit-body"]')!, { target: { value: "長".repeat(1280) } });
    expect(save.disabled).toBe(false);
    fireEvent.click(save);
    await waitFor(() => expect(edit).toHaveBeenCalled());
    expect(edit.mock.calls).toEqual([["L-2", { title: "BBBB", body: "長".repeat(1280) }]]);
  });

  it("when the save goes through but the re-read fails, the card shows the new title and body and the page asks for a reload", async () => {
    vi.spyOn(api, "editLoreEntry").mockResolvedValue(undefined);
    let edited = false;
    vi.spyOn(api, "listLoreEntries").mockImplementation(async () => {
      if (edited) throw new Error("network down");
      return page(rows(), cap);
    });
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-1", "L-2", "L-3"]));

    fireEvent.click(rowById(container, "L-2").querySelector('[data-testid="lore-edit"]')!);
    const editor = rowById(container, "L-2").querySelector<HTMLElement>('[data-testid="lore-editor"]')!;
    fireEvent.change(editor.querySelector('[data-testid="lore-edit-title"]')!, { target: { value: "新標題" } });
    fireEvent.change(editor.querySelector('[data-testid="lore-edit-body"]')!, { target: { value: "新內容" } });
    edited = true;
    fireEvent.click(editor.querySelector('[data-testid="lore-edit-save"]')!);

    await waitFor(() =>
      expect(rowById(container, "L-2").querySelector('[data-testid="lore-editor"]')).toBeNull(),
    );
    expect(container.querySelector('[data-testid="lore-action-error"]')!.textContent).toBe(
      "已儲存，但重新讀取失敗，請重新整理頁面",
    );
    expect(rowById(container, "L-2").querySelector(".lore-row__title")!.textContent).toBe("新標題");
    expect(rowById(container, "L-2").querySelector('[data-testid="lore-body"]')!.textContent).toBe("新內容");
    expect(rowById(container, "L-1").querySelector(".lore-row__title")!.textContent).toBe("第一筆");
  });

  it("an older re-read that fails after a newer one answered asks for no reload", async () => {
    vi.spyOn(api, "editLoreEntry").mockResolvedValue(undefined);
    const first = held<never>();
    let calls = 0;
    vi.spyOn(api, "listLoreEntries").mockImplementation(async () => {
      calls += 1;
      if (calls === 2) return first.promise;
      if (calls === 3) {
        return page([mkEntry({ id: "L-1", title: "一改" }), mkEntry({ id: "L-2", title: "二改" }), rows()[2]], cap);
      }
      return page(rows(), cap);
    });
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-1", "L-2", "L-3"]));

    for (const [id, title] of [["L-1", "一改"], ["L-2", "二改"]]) {
      fireEvent.click(rowById(container, id).querySelector('[data-testid="lore-edit"]')!);
      const editor = rowById(container, id).querySelector<HTMLElement>('[data-testid="lore-editor"]')!;
      fireEvent.change(editor.querySelector('[data-testid="lore-edit-title"]')!, { target: { value: title } });
      fireEvent.click(editor.querySelector('[data-testid="lore-edit-save"]')!);
      await waitFor(() => expect(calls).toBe(id === "L-1" ? 2 : 3));
    }
    await waitFor(() =>
      expect(rowById(container, "L-2").querySelector(".lore-row__title")!.textContent).toBe("二改"),
    );
    first.reject(new Error("network down"));

    await waitFor(() =>
      expect(rowById(container, "L-1").querySelector('[data-testid="lore-editor"]')).toBeNull(),
    );
    expect(container.querySelector('[data-testid="lore-action-error"]')).toBeNull();
    expect(rowById(container, "L-1").querySelector(".lore-row__title")!.textContent).toBe("一改");
  });

  it("a refused save keeps the editor and the draft and says why", async () => {
    vi.spyOn(api, "editLoreEntry").mockRejectedValue(
      new ApiError("http 403 for PATCH /api/lore/L-2", 403, "forbidden", "置頂的傳承只有管理員能改"),
    );
    const list = stubList({ ...page(rows(), cap), usedChars: 990, firstDroppedChars: 20 });
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(3));

    fireEvent.click(rowById(container, "L-2").querySelector('[data-testid="lore-edit"]')!);
    const editor = rowById(container, "L-2").querySelector<HTMLElement>('[data-testid="lore-editor"]')!;
    fireEvent.change(editor.querySelector('[data-testid="lore-edit-title"]')!, { target: { value: "新標題" } });
    fireEvent.click(editor.querySelector('[data-testid="lore-edit-save"]')!);

    await waitFor(() =>
      expect(editor.querySelector('[data-testid="lore-edit-error"]')?.textContent).toBe(
        "置頂的傳承只有管理員能改",
      ),
    );
    expect(editor.querySelector<HTMLInputElement>('[data-testid="lore-edit-title"]')!.value).toBe("新標題");
    expect(list).toHaveBeenCalledTimes(1);
  });
});

describe("LorePage — 類型選單", () => {
  const rows = () => [
    mkEntry({ id: "L-1", effectiveTs: 3 }),
    mkEntry({ id: "L-2", loreType: "other", effectiveTs: 2 }),
    mkEntry({ id: "L-3", effectiveTs: 1 }),
  ];

  it("the type tag opens a menu of the five types with the current one marked", async () => {
    stubList(page(rows()));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(3));
    const row = rowById(container, "L-2");
    const tag = row.querySelector<HTMLElement>('[data-testid="lore-type"]')!;
    expect(tag.tagName).toBe("BUTTON");
    expect(tag.getAttribute("aria-haspopup")).toBe("menu");
    expect(row.querySelector('[data-testid="lore-type-options"]')).toBeNull();

    fireEvent.click(tag);
    const menu = row.querySelector<HTMLElement>('[data-testid="lore-type-options"]')!;
    expect(menu.getAttribute("role")).toBe("menu");
    const items = Array.from(menu.querySelectorAll<HTMLElement>('[role="menuitem"]'));
    expect(items.map((el) => el.textContent)).toEqual([
      "指示衝突",
      "指示補充",
      "Owner 決策",
      "Owner 偏好",
      "其他",
    ]);
    expect(
      items.filter((el) => el.classList.contains("lore-row__menu-item--active")).map((el) => el.textContent),
    ).toEqual(["其他"]);

    fireEvent.mouseDown(document.body);
    expect(row.querySelector('[data-testid="lore-type-options"]')).toBeNull();
  });

  it("choosing a type stages it on that card; 儲存變更 sends only loreType", async () => {
    const edit = vi.spyOn(api, "editLoreEntry").mockResolvedValue(undefined);
    let saved = false;
    vi.spyOn(api, "listLoreEntries").mockImplementation(async () =>
      saved
        ? page([rows()[0], mkEntry({ id: "L-2", loreType: "owner_decision", effectiveTs: 2 }), rows()[2]])
        : page(rows()),
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-1", "L-2", "L-3"]));
    const before = rowById(container, "L-2");

    fireEvent.click(before.querySelector('[data-testid="lore-type"]')!);
    fireEvent.click(before.querySelector('[data-testid="lore-type-owner_decision"]')!);

    expect(before.querySelector('[data-testid="lore-type"]')!.textContent).toBe("Owner 決策");
    expect(before.querySelector('[data-testid="lore-type-options"]')).toBeNull();
    expect(edit).not.toHaveBeenCalled();

    saved = true;
    fireEvent.click(container.querySelector('[data-testid="lore-staged-save"]')!);
    await waitFor(() => expect(container.querySelector('[data-testid="lore-staged-bar"]')).toBeNull());
    expect(edit.mock.calls).toEqual([["L-2", { loreType: "owner_decision" }]]);
    expect(rowById(container, "L-2")).toBe(before);
    expect(before.querySelector('[data-testid="lore-type"]')!.textContent).toBe("Owner 決策");
  });

  it("choosing the current type stages nothing", async () => {
    const edit = vi.spyOn(api, "editLoreEntry").mockResolvedValue(undefined);
    stubList(page(rows()));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(3));
    const row = rowById(container, "L-2");
    fireEvent.click(row.querySelector('[data-testid="lore-type"]')!);
    fireEvent.click(row.querySelector('[data-testid="lore-type-other"]')!);
    expect(row.querySelector('[data-testid="lore-type-options"]')).toBeNull();
    expect(container.querySelector('[data-testid="lore-staged-bar"]')).toBeNull();
    expect(edit).not.toHaveBeenCalled();
  });

  it("a refused type change stays staged and the bar says so", async () => {
    vi.spyOn(api, "editLoreEntry").mockRejectedValue(
      new ApiError("http 403 for PATCH /api/lore/L-2", 403, "forbidden", ""),
    );
    stubList(page(rows()));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(3));
    const row = rowById(container, "L-2");
    fireEvent.click(row.querySelector('[data-testid="lore-type"]')!);
    fireEvent.click(row.querySelector('[data-testid="lore-type-instruction_conflict"]')!);
    fireEvent.click(container.querySelector('[data-testid="lore-staged-save"]')!);
    await waitFor(() =>
      expect(container.querySelector('[data-testid="lore-staged-failed"]')?.textContent).toBe(
        `沒有成功：L-2（${zh.lore.forbidden}）`,
      ),
    );
    expect(row.querySelector('[data-testid="lore-type"]')!.textContent).toBe("指示衝突");
    expect(row.dataset.staged).toBe("true");
  });
});

/** The head's actions are 編輯, drawn as every other 編輯 in the app
 * (settings.css .doc-btn--edit: pencil + label), then the 收合 chevron, at
 * every width. */
function expectHeadActions(row: HTMLElement) {
  const actions = row.querySelector<HTMLElement>(".lore-row__actions")!;
  expect(
    Array.from(actions.children).map((b) => [
      b.tagName,
      b.getAttribute("data-testid"),
      b.className,
      b.textContent,
      b.getAttribute("aria-hidden"),
    ]),
  ).toEqual([
    ["BUTTON", "lore-edit", "doc-btn doc-btn--edit", "編輯", null],
    ["SPAN", "lore-fold-mark", "lore-row__fold lore-row__fold--open", "", "true"],
  ]);
  expect(Array.from(actions.children).map((b) => b.querySelector("svg") !== null)).toEqual([true, true]);
}

describe("LorePage — 卡片右上的動作", () => {
  it("on a desktop the head carries 編輯 and 收合, and 編輯 opens the editor", async () => {
    stubList(
      page([mkEntry({ id: "L-1", scopeKind: "agent", scopeKey: "mira", scopeOptions: ["agent", "everyone"] })]),
    );
    const { container } = await renderOpen({ canSetScope: true });
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const row = rowById(container, "L-1");
    expectHeadActions(row);
    fireEvent.click(row.querySelector('[data-testid="lore-edit"]')!);
    expect(row.querySelector('[data-testid="lore-editor"]')).not.toBeNull();
  });
});

describe("LorePage — 手機寬度", () => {
  beforeEach(() => {
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      writable: true,
      value: (query: string) => ({
        matches: /max-width/.test(query),
        media: query,
        onchange: null,
        addEventListener: () => {},
        removeEventListener: () => {},
        addListener: () => {},
        removeListener: () => {},
        dispatchEvent: () => false,
      }),
    });
  });
  afterEach(() => {
    delete (window as unknown as { matchMedia?: unknown }).matchMedia;
  });

  it("shows the same 編輯 and 收合 as a desktop, no 「⋯」 menu, and 編輯 opens the editor", async () => {
    stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = await renderOpen({ canSetScope: true });
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const row = rowById(container, "L-1");
    expectHeadActions(row);
    fireEvent.click(row.querySelector('[data-testid="lore-edit"]')!);
    expect(row.querySelector('[data-testid="lore-editor"]')).not.toBeNull();
  });

  it("the scope tag opens the 適用範圍 menu on a phone too", async () => {
    stubList(
      page([mkEntry({ id: "L-1", scopeKind: "agent", scopeKey: "mira", scopeOptions: ["agent", "everyone"] })]),
    );
    const { container } = await renderOpen({ canSetScope: true });
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const row = rowById(container, "L-1");
    fireEvent.click(row.querySelector('.lore-row__badges [data-testid="lore-scope-name"]')!);
    await waitFor(() =>
      expect(row.querySelector('[data-testid="lore-scope-options"]')?.textContent).toBe(
        "適用範圍建立者：Mira預設所有人",
      ),
    );
  });
});

describe("LorePage — 版本紀錄", () => {
  async function openMiraWithEditedL2() {
    await api.editLoreEntry("L-2", { title: "改過的標題", body: "改過的內容" });
    const list = vi.spyOn(api, "listLoreEntries");
    const r = renderPage();
    const toggle = await waitFor(() => {
      const el = r.container.querySelector<HTMLElement>(
        '[data-testid="lore-target"][data-section="agent:mira"]',
      );
      if (!el) throw new Error("no section for Mira");
      return el;
    });
    fireEvent.click(toggle);
    await waitFor(() => expect(renderedIds(r.container)).toEqual(["L-1", "L-7", "L-2"]));
    return { ...r, list };
  }

  it("is reached from the editor, shows the old title and body, compares them with the card, and a restore leaves edit mode with them back on that card after the section is re-read", async () => {
    const { container, list } = await openMiraWithEditedL2();
    const titleOf = (id: string) =>
      rowById(container, id).querySelector(".lore-row__title")!.textContent;
    expect(titleOf("L-2")).toBe("改過的標題");

    expect(rowById(container, "L-2").querySelector('[data-testid="doc-history-entry-lore_entry"]')).toBeNull();
    fireEvent.click(rowById(container, "L-2").querySelector('[data-testid="lore-edit"]')!);
    const editor = rowById(container, "L-2").querySelector<HTMLElement>('[data-testid="lore-editor"]')!;
    expect(
      Array.from(editor.querySelectorAll(".lore-row__editor-buttons button")).map((b) => b.textContent),
    ).toEqual(["版本紀錄", "取消", "儲存"]);
    fireEvent.click(editor.querySelector('[data-testid="doc-history-entry-lore_entry"]')!);
    const rowOpen = await waitFor(() => {
      const el = document.querySelector<HTMLElement>('[data-testid^="doc-history-open-"]');
      if (!el) throw new Error("no retained version listed");
      return el;
    });
    expect(document.querySelectorAll('[data-testid^="doc-history-open-"]')).toHaveLength(1);
    fireEvent.click(rowOpen);

    const modal = await waitFor(() => {
      const el = document.querySelector<HTMLElement>('[data-testid="doc-history-modal"]');
      if (!el || !el.textContent?.includes("零命中的預設解讀是查法寫錯了")) {
        throw new Error("old version not shown yet");
      }
      return el;
    });
    expect(
      Array.from(modal.querySelectorAll(".doc-hist-modal__field-name")).map((h) => h.textContent),
    ).toEqual(["標題", "內容"]);

    fireEvent.click(modal.querySelector('[data-testid="doc-history-pane-diff"]')!);
    expect(modal.querySelector('[data-testid="doc-history-diff-title"]')).not.toBeNull();
    expect(modal.querySelector('[data-testid="doc-history-diff-body"]')).not.toBeNull();

    fireEvent.click(modal.querySelector('[data-testid="doc-history-modal-restore"]')!);
    fireEvent.click(
      await waitFor(() => {
        const el = document.querySelector<HTMLElement>('[data-testid="doc-history-restore-confirm-btn"]');
        if (!el) throw new Error("no confirm");
        return el;
      }),
    );

    await waitFor(() => expect(titleOf("L-2")).toBe("零命中的預設解讀是查法寫錯了"));
    expect(rowById(container, "L-2").querySelector('[data-testid="lore-editor"]')).toBeNull();
    expect(rowById(container, "L-2").querySelector('[data-testid="lore-body"]')!.textContent).toBe(
      "掃描回空的時候先跑一次陽性對照，確認量具本身還會命中，再去解釋那個零。",
    );
    expect(renderedIds(container)).toEqual(["L-1", "L-7", "L-2"]);
    expect(list.mock.calls.map((c) => c[0])).toEqual([
      { states: ["pinned", "active"], scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], limit: 30, offset: 0 },
      { states: ["pinned", "active"], scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], limit: 30, offset: 0 },
    ]);
  });

  it("when a restore lands but the re-read fails, the card shows the restored title and body, another card keeps its own, and the page asks for a reload", async () => {
    const { container, list } = await openMiraWithEditedL2();
    const l1Title = rowById(container, "L-1").querySelector(".lore-row__title")!.textContent;
    const l1Body = rowById(container, "L-1").querySelector('[data-testid="lore-body"]')!.textContent;
    chooseStateOn(container, "L-1", "retired");
    list.mockRejectedValue(new Error("network down"));

    fireEvent.click(rowById(container, "L-2").querySelector('[data-testid="lore-edit"]')!);
    fireEvent.click(
      rowById(container, "L-2").querySelector('[data-testid="doc-history-entry-lore_entry"]')!,
    );
    fireEvent.click(
      await waitFor(() => {
        const el = document.querySelector<HTMLElement>('[data-testid^="doc-history-open-"]');
        if (!el) throw new Error("no retained version listed");
        return el;
      }),
    );
    const modal = await waitFor(() => {
      const el = document.querySelector<HTMLElement>('[data-testid="doc-history-modal"]');
      if (!el || !el.textContent?.includes("零命中的預設解讀是查法寫錯了")) {
        throw new Error("old version not shown yet");
      }
      return el;
    });
    fireEvent.click(modal.querySelector('[data-testid="doc-history-modal-restore"]')!);
    fireEvent.click(
      await waitFor(() => {
        const el = document.querySelector<HTMLElement>('[data-testid="doc-history-restore-confirm-btn"]');
        if (!el) throw new Error("no confirm");
        return el;
      }),
    );

    await waitFor(() =>
      expect(rowById(container, "L-2").querySelector('[data-testid="lore-editor"]')).toBeNull(),
    );
    expect(container.querySelector('[data-testid="lore-action-error"]')!.textContent).toBe(
      "已儲存，但重新讀取失敗，請重新整理頁面",
    );
    expect(rowById(container, "L-2").querySelector(".lore-row__title")!.textContent).toBe(
      "零命中的預設解讀是查法寫錯了",
    );
    expect(rowById(container, "L-2").querySelector('[data-testid="lore-body"]')!.textContent).toBe(
      "掃描回空的時候先跑一次陽性對照，確認量具本身還會命中，再去解釋那個零。",
    );
    expect(rowById(container, "L-1").querySelector(".lore-row__title")!.textContent).toBe(l1Title);
    expect(rowById(container, "L-1").querySelector('[data-testid="lore-body"]')!.textContent).toBe(l1Body);
    expect(rowById(container, "L-1").querySelector('[data-testid="lore-state"]')!.textContent).toBe("已失效");
    expect(rowById(container, "L-1").dataset.staged).toBe("true");
  });

  it("an entry never edited lists no retained version", async () => {
    const { container } = await openMiraWithEditedL2();

    fireEvent.click(rowById(container, "L-1").querySelector('[data-testid="lore-edit"]')!);
    fireEvent.click(
      rowById(container, "L-1").querySelector('[data-testid="doc-history-entry-lore_entry"]')!,
    );

    await waitFor(() =>
      expect(document.querySelector('[data-testid="doc-history-list"]')?.textContent).toContain(
        "還沒有保留任何版本",
      ),
    );
    expect(document.querySelectorAll('[data-testid^="doc-history-open-"]')).toHaveLength(0);
  });
});

function held<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function chooseStateOn(container: HTMLElement, entryId: string, next: LoreEntryView["state"]) {
  const row = rowById(container, entryId);
  fireEvent.click(row.querySelector('[data-testid="lore-state"]')!);
  fireEvent.click(row.querySelector(`[data-testid="lore-state-${next}"]`)!);
}

function search(text: string) {
  openFilters();
  const box = document.querySelector<HTMLInputElement>('[data-testid="lore-filter-id"]')!;
  fireEvent.change(box, { target: { value: text } });
  fireEvent.keyDown(box, { key: "Enter" });
}

function sectionHead(container: HTMLElement): string {
  return container.querySelector('[data-testid="lore-section-head"]')?.textContent ?? "";
}

describe("LorePage — 晚到的回應", () => {
  it("a first page answered after a newer search does not replace the newer answer", async () => {
    const slow = held<LoreEntryPageView>();
    vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      o?.q === "bbb" ? page([mkEntry({ id: "L-2" })]) : slow.promise,
    );
    const { container } = await renderOpen();
    await openFiltersWhenReady();

    search("bbb");
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-2"]));
    slow.resolve(page([mkEntry({ id: "L-1" })]));
    await new Promise((r) => setTimeout(r, 20));

    expect(renderedIds(container)).toEqual(["L-2"]);
  });

  it("leaving a section while its first page is held: the answer lands nowhere", async () => {
    vi.spyOn(api, "listLoreBudgets").mockResolvedValue([MIRA_TARGET]);
    const first = held<LoreEntryPageView>();
    const list = vi.spyOn(api, "listLoreEntries").mockReturnValue(first.promise);
    const { container } = await renderOpen();
    await waitFor(() => expect(list).toHaveBeenCalledTimes(1));

    fireEvent.click(container.querySelector('[data-testid="lore-back"]')!);
    await waitFor(() => expect(headerTexts(container)).toHaveLength(1));
    first.resolve(page([mkEntry({ id: "L-1" })]));
    await new Promise((r) => setTimeout(r, 20));

    expect(renderedIds(container)).toEqual([]);
    expect(container.textContent).not.toContain(zh.lore.loadingMore);
    expect(list).toHaveBeenCalledTimes(1);
  });

  it("an older re-read answering last does not bring back its rows, their greying or its header numbers", async () => {
    vi.spyOn(api, "editLoreEntry").mockResolvedValue(undefined);
    vi.spyOn(api, "setLoreEntryState").mockResolvedValue(undefined);
    const firstReread = held<LoreEntryPageView>();
    const firstBudgets = held<LoreBudgetTargetView[]>();
    const rows = [mkEntry({ id: "L-1", dropped: true }), mkEntry({ id: "L-2", dropped: true })];
    const lineAtL2 = page(rows, { capChars: 1000, firstDroppedId: "L-1" });
    let listCalls = 0;
    vi.spyOn(api, "listLoreEntries").mockImplementation(async () => {
      listCalls += 1;
      if (listCalls === 2) return firstReread.promise;
      if (listCalls === 3) return page([mkEntry({ id: "L-1", body: "改過", dropped: false })], { capChars: 1000, firstDroppedId: "" });
      return lineAtL2;
    });
    let budgetCalls = 0;
    vi.spyOn(api, "listLoreBudgets").mockImplementation(async () => {
      budgetCalls += 1;
      if (budgetCalls === 2) return firstBudgets.promise;
      return [{ ...MIRA_TARGET, entryCount: 5, droppedCount: budgetCalls === 1 ? 2 : 5 }];
    });
    const { container } = await renderOpen();
    await waitFor(() => expect(rowById(container, "L-1").dataset.dimmed).toBe("true"));
    fireEvent.click(rowById(container, "L-1"));
    fireEvent.click(rowById(container, "L-2"));

    fireEvent.click(rowById(container, "L-1").querySelector('[data-testid="lore-edit"]')!);
    fireEvent.change(rowById(container, "L-1").querySelector('[data-testid="lore-edit-body"]')!, { target: { value: "改過" } });
    fireEvent.click(rowById(container, "L-1").querySelector('[data-testid="lore-edit-save"]')!);
    await waitFor(() => expect(budgetCalls).toBe(2));
    chooseStateOn(container, "L-2", "retired");
    fireEvent.click(container.querySelector('[data-testid="lore-staged-save"]')!);
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-1"]));
    await waitFor(() => expect(sectionHead(container)).toContain("5 筆讀不到"));
    expect(rowById(container, "L-1").dataset.dimmed).toBe("false");
    expect(screen.getByTestId("lore-filter-summary").textContent).toBe("1 筆傳承");

    firstReread.resolve(lineAtL2);
    firstBudgets.resolve([{ ...MIRA_TARGET, entryCount: 5, droppedCount: 2 }]);
    await new Promise((r) => setTimeout(r, 20));

    expect(renderedIds(container)).toEqual(["L-1"]);
    expect(rowById(container, "L-1").dataset.dimmed).toBe("false");
    expect(sectionHead(container)).toContain("5 筆讀不到");
    expect(screen.getByTestId("lore-filter-summary").textContent).toBe("1 筆傳承");
  });

  it("a next page still in flight when the section is re-read is dropped: the re-read's rows and cap stand", async () => {
    vi.spyOn(api, "setLoreEntryState").mockResolvedValue(undefined);
    const first = Array.from({ length: 30 }, (_, i) => mkEntry({ id: `L-${i + 1}` }));
    const nextPage = held<LoreEntryPageView>();
    let saved = false;
    vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) => {
      if (o?.offset === 30) return nextPage.promise;
      return saved
        ? page(first.slice(1), { capChars: 1000, firstDroppedId: "" })
        : page(first, { capChars: 1000, firstDroppedId: "L-2" });
    });
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(30));

    fireEvent.scroll(container.querySelector('[data-testid="lore-page"]')!);
    chooseStateOn(container, "L-1", "retired");
    saved = true;
    fireEvent.click(container.querySelector('[data-testid="lore-staged-save"]')!);
    await waitFor(() => expect(renderedIds(container)).toHaveLength(29));
    nextPage.resolve(page([mkEntry({ id: "L-31" })], { capChars: 1000, firstDroppedId: "L-2" }));
    await new Promise((r) => setTimeout(r, 20));

    expect(renderedIds(container)).toEqual(first.slice(1).map((e) => e.id));
  });
});
