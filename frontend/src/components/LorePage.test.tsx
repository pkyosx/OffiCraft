// LorePage — what an open section draws from the server's numbers, the card,
// its actions, and the filters.
//
//   界線. `firstDroppedId` and `capChars` are the SERVER'S answer over the
//   whole scope, so the fixture's TEXT LENGTHS ARE DELIBERATELY NOT A CLUE: the
//   entry named by `firstDroppedId` is one of the SHORTEST rows on screen. A
//   local sum cannot produce this answer. `capChars === 0` means no line AND no
//   dimming.
//
//   訊息框的收件人. A message to a peer who is gone reaches nobody, so the box
//   names and sends to Mira whenever the author cannot take it — but only once
//   the roster has answered: before that a live author looks gone too.
//
// Most specs open ONE section (Mira's) whose list request is stubbed: what the
// page does with the answer is under test here, not the mock's arithmetic.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
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
import { clearLocale, useEnglishLocale } from "../test/effortOptions";
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
});

afterEach(() => {
  vi.restoreAllMocks();
});


describe("LorePage — 界線", () => {
  // 🔴 The line falls where the SERVER says. `mid` is one of the SHORTEST
  // entries on screen and still the first dropped: any local title+body sum
  // would put the line somewhere else.
  const entries = [
    mkEntry({ id: "p1", state: "pinned", title: "很長很長的置頂標題".repeat(8) }),
    mkEntry({ id: "top", state: "active", title: "上面這一筆".repeat(20) }),
    mkEntry({ id: "mid", state: "active", title: "短", body: "短" }),
    mkEntry({ id: "low", state: "active" }),
    mkEntry({ id: "r1", state: "retired", retireReason: "重複" }),
  ];

  it("dims firstDroppedId and everything under it, and draws the line above it with used / cap and what it needs", async () => {
    stubList({
      ...page(entries, { capChars: 10000, firstDroppedId: "mid" }),
      usedChars: 9640,
      firstDroppedChars: 520,
    });
    const { container } = await renderOpen();

    await waitFor(() => expect(renderedIds(container)).toHaveLength(5));
    expect(renderedIds(container)).toEqual(["p1", "top", "mid", "low", "r1"]);

    // Above the line: NOT dimmed.
    expect(rowById(container, "p1").dataset.dimmed).toBe("false");
    expect(rowById(container, "top").dataset.dimmed).toBe("false");
    // The named entry itself is BELOW the line — it is the first one that does
    // not get loaded — and so is everything after it, groups included.
    expect(rowById(container, "mid").dataset.dimmed).toBe("true");
    expect(rowById(container, "low").dataset.dimmed).toBe("true");
    expect(rowById(container, "r1").dataset.dimmed).toBe("true");

    const lines = container.querySelectorAll('[data-testid="lore-cap-line"]');
    expect(lines).toHaveLength(1);
    expect(lines[0].textContent).toBe(
      "上限線　已用 9,640 / 10,000 字・mid 需要 520 字，從這裡以下讀不到",
    );

    // …and it sits BEFORE the row it names in document order.
    expect(
      lines[0].compareDocumentPosition(rowById(container, "mid")) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("draws no line and dims nothing when capChars is 0 (filter did not converge)", async () => {
    stubList(page(entries, { capChars: 0, firstDroppedId: "" }));
    const { container } = await renderOpen();

    await waitFor(() => expect(renderedIds(container)).toHaveLength(5));

    expect(container.querySelectorAll('[data-testid="lore-cap-line"]')).toHaveLength(
      0,
    );
    for (const id of ["p1", "top", "mid", "low", "r1"]) {
      expect(rowById(container, id).dataset.dimmed).toBe("false");
    }
  });

  it("draws no line when the whole scope fits (capChars set, firstDroppedId empty)", async () => {
    stubList(page(entries, { capChars: 10000, firstDroppedId: "" }));
    const { container } = await renderOpen();

    await waitFor(() => expect(renderedIds(container)).toHaveLength(5));

    expect(container.querySelectorAll('[data-testid="lore-cap-line"]')).toHaveLength(
      0,
    );
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

  it("goes to a live author unless the entry is pinned, and to Mira for a pinned entry, the owner's or a departed author's", async () => {
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
      ["L-1", "L-2", "L-3", "L-4"].map((id) => box(rowById(container, id)).placeholder),
    ).toEqual([
      "傳訊息給 Warden · mbp5…",
      "傳訊息給 Mira…",
      "傳訊息給 Mira…",
      "傳訊息給 Mira…",
    ]);

    const posted = capturePosts();
    for (const id of ["L-1", "L-2", "L-3", "L-4"]) {
      fireEvent.change(box(rowById(container, id)), { target: { value: "還適用嗎" } });
      fireEvent.click(sendButton(rowById(container, id)));
      await waitFor(() => expect(box(rowById(container, id)).value).toBe(""));
    }
    expect(posted.map((p) => p.to)).toEqual(["warden-mbp5", "mira", "mira", "mira"]);
  });

  it("picks nobody until the roster answers, then sends to the live author", async () => {
    const roster = await api.listMembers();
    let answerRoster: (v: typeof roster) => void = () => {};
    vi.spyOn(api, "listMembers").mockReturnValue(
      new Promise((resolve) => {
        answerRoster = resolve;
      }),
    );
    stubList(page([mkEntry({ id: "L-1", state: "active", authorId: "warden-mbp5" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const posted = capturePosts();
    const row = rowById(container, "L-1");

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

  it("sends nothing when the roster fails to load, and says the roster did not load", async () => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.spyOn(api, "listMembers").mockRejectedValue(new Error("boom"));
    stubList(page([mkEntry({ id: "L-1", state: "active", authorId: "warden-mbp5" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const posted = capturePosts();
    const row = rowById(container, "L-1");

    await waitFor(() => expect(box(row).placeholder).toBe("載入辦公室成員失敗，請稍後重試"));
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
  it("shows the id and copies it on click", async () => {
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

    // The card has no fold; a click on the id copies and does nothing else.
    expect(container.querySelector('[data-testid="lore-row"]')!.hasAttribute("aria-expanded")).toBe(false);
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

  function tick(container: HTMLElement, testId: string, value: string) {
    const trigger = container.querySelector<HTMLElement>(
      `[data-testid="${testId}"]`,
    );
    if (!trigger) throw new Error(`no ${testId} filter`);
    if (trigger.getAttribute("aria-expanded") !== "true") {
      fireEvent.click(trigger);
    }
    const box = container.querySelector<HTMLElement>(
      `[data-testid="${testId}-opt-${value}"] input`,
    );
    if (!box) throw new Error(`no ${testId} option ${value}`);
    fireEvent.click(box);
  }

  it("asks for 置頂 and 生效中 by default, and ticking 已失效 adds it to the same request", async () => {
    const spy = stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    expect(lastOpts(spy)).toEqual({
      states: ["pinned", "active"],
      scopeKinds: ["everyone", "agent"],
      scopeKeys: ["mira"],
      limit: 30,
      offset: 0,
    });

    tick(container, "lore-filter-state", "retired");

    await waitFor(() =>
      expect(lastOpts(spy)).toEqual({
        states: ["pinned", "active", "retired"],
        scopeKinds: ["everyone", "agent"],
        scopeKeys: ["mira"],
        limit: 30,
        offset: 0,
      }),
    );
  });

  it("sends the ticked types as loreTypes, lists all five in order, and 清除篩選 drops them", async () => {
    const spy = stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    expect(
      container.querySelector('[data-testid="lore-filter-clear"]'),
    ).toBeNull();

    fireEvent.click(
      container.querySelector<HTMLElement>('[data-testid="lore-filter-type"]')!,
    );
    const options = Array.from(
      container.querySelectorAll('[data-testid^="lore-filter-type-opt-"]'),
    ).map((el) => [el.getAttribute("data-testid"), el.textContent]);
    expect(options).toEqual([
      ["lore-filter-type-opt-instruction_conflict", "指示衝突"],
      ["lore-filter-type-opt-instruction_supplement", "指示補充"],
      ["lore-filter-type-opt-owner_decision", "Owner 決策"],
      ["lore-filter-type-opt-owner_preference", "Owner 偏好"],
      ["lore-filter-type-opt-other", "其他"],
    ]);

    const mira = { scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], limit: 30, offset: 0 };
    tick(container, "lore-filter-type", "owner_decision");
    await waitFor(() =>
      expect(lastOpts(spy)).toEqual({
        states: ["pinned", "active"],
        loreTypes: ["owner_decision"],
        ...mira,
      }),
    );
    tick(container, "lore-filter-type", "other");
    await waitFor(() =>
      expect(lastOpts(spy)).toEqual({
        states: ["pinned", "active"],
        loreTypes: ["owner_decision", "other"],
        ...mira,
      }),
    );

    fireEvent.click(
      container.querySelector<HTMLElement>('[data-testid="lore-filter-clear"]')!,
    );
    await waitFor(() =>
      expect(lastOpts(spy)).toEqual({ states: ["pinned", "active"], ...mira }),
    );
    expect(
      container.querySelector('[data-testid="lore-filter-clear"]'),
    ).toBeNull();
  });

  it("has the id box, the type and state filters, and no 撰寫人 filter: the section already names whose entries these are", async () => {
    const spy = stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    const filters = Array.from(container.querySelectorAll('[data-testid^="lore-filter-"]'))
      .map((el) => el.getAttribute("data-testid")!)
      .filter((id) => !/-opt-|-clear$|-fields$/.test(id) && id !== "lore-filter");
    expect(filters).toEqual(["lore-filter-id", "lore-filter-type", "lore-filter-state"]);
    expect(lastOpts(spy)).not.toHaveProperty("authorIds");
  });

  it("names an unconstrained filter 「所有…」 and shows the default state set as two ticks", async () => {
    stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    const labelOf = (testId: string) =>
      container
        .querySelector<HTMLElement>(`[data-testid="${testId}"]`)!
        .textContent!.replace(/\s+/g, "");

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

    const fields = ["lore-filter-id", "lore-filter-type", "lore-filter-state"];
    const idsInDomOrder = () =>
      Array.from(container.querySelectorAll('[data-testid^="lore-filter-"]'))
        .map((el) => el.getAttribute("data-testid")!)
        .filter((id) => fields.includes(id));

    expect(idsInDomOrder()).toEqual(fields);

    fireEvent.click(container.querySelector<HTMLElement>('[data-testid="lore-filter-type"]')!);
    fireEvent.click(
      container.querySelector<HTMLElement>('[data-testid="lore-filter-type-opt-other"] input')!,
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
describe("LorePage — 卡片底下一行小字", () => {
  it("shows 撰寫人 · 生效期 on one line, and the message box is the last thing on the card", async () => {
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
      "lore-author-row",
      "·",
      "lore-effective",
    ]);
    expect(meta.querySelector('[data-testid="lore-author"]')!.textContent).toBe("Mira");
    expect(row.lastElementChild!.getAttribute("data-testid")).toBe("lore-message-box");
  });

});

describe("LorePage — 卡片不折疊", () => {
  it("shows the whole body with no chevron, no clamp and no toggle, and a click changes nothing", async () => {
    stubList(page([mkEntry({ id: "L-7", authorId: "mira", body: "第一行\n第二行\n第三行\n第四行" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const row = rowById(container, "L-7");
    const body = row.querySelector<HTMLElement>('[data-testid="lore-body"]')!;
    const snapshot = () => [row.getAttribute("role"), row.hasAttribute("aria-expanded"), body.className];
    expect(snapshot()).toEqual([null, false, "lore-row__body"]);
    expect(
      Array.from(row.querySelector(".lore-row__head")!.children).map((c) => c.className),
    ).toEqual(["lore-row__badges", "lore-row__actions"]);
    expect(body.textContent).toContain("第四行");
    // Out of the tab order; focusable only from code (a highlighted entry).
    expect(row.getAttribute("tabindex")).toBe("-1");
    expect(row.querySelector('[data-testid="lore-author-row"]')).not.toBeNull();
    expect(row.querySelector('[data-testid="lore-effective"]')).not.toBeNull();
    fireEvent.click(body);
    fireEvent.keyDown(row, { key: "Enter" });
    expect(snapshot()).toEqual([null, false, "lore-row__body"]);
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
  /** Two rows, always. Acting on the SECOND one is what makes the id
   * assertion real — a call hardwired to the first entry would pass against a
   * one-row fixture. */
  function twoRows(secondState: LoreEntryView["state"] = "active") {
    return page([
      mkEntry({ id: "L-1", state: "active" }),
      mkEntry({ id: "L-2", state: secondState }),
    ]);
  }

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

  async function renderTwo(secondState: LoreEntryView["state"] = "active") {
    const list = stubList(twoRows(secondState));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));
    return { container, list };
  }

  it("失效 asks the server for retired, on the entry that was clicked", async () => {
    const set = vi
      .spyOn(api, "setLoreEntryState")
      .mockResolvedValue(undefined);
    const { container } = await renderTwo("active");

    chooseState(container, "L-2", "retired");

    await waitFor(() => expect(set).toHaveBeenCalledTimes(1));
    expect(set).toHaveBeenCalledWith("L-2", "retired");
  });

  it("生效 asks the server for active, on the entry that was clicked", async () => {
    const set = vi
      .spyOn(api, "setLoreEntryState")
      .mockResolvedValue(undefined);
    const { container } = await renderTwo("retired");

    chooseState(container, "L-2", "active");

    await waitFor(() => expect(set).toHaveBeenCalledTimes(1));
    expect(set).toHaveBeenCalledWith("L-2", "active");
  });

  it("置頂 asks the server for pinned on the entry that was clicked, and repaints that card where it stands without re-reading page one or moving the scroll", async () => {
    const set = vi
      .spyOn(api, "setLoreEntryState")
      .mockResolvedValue(undefined);
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      o?.entryIds ? page([mkEntry({ id: "L-2", state: "pinned" })]) : twoRows("active"),
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));
    const scroller = container.querySelector<HTMLElement>('[data-testid="lore-page"]')!;
    scroller.scrollTop = 240;

    chooseState(container, "L-2", "pinned");

    await waitFor(() => expect(set).toHaveBeenCalledTimes(1));
    expect(set).toHaveBeenCalledWith("L-2", "pinned");
    await waitFor(() =>
      expect(rowById(container, "L-2").querySelector('[data-testid="lore-state"]')!.textContent).toBe(
        zh.lore.statePinned,
      ),
    );
    expect(renderedIds(container)).toEqual(["L-1", "L-2"]);
    expect(list.mock.calls.map((c) => c[0])).toEqual([
      { states: ["pinned", "active"], scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], limit: 30, offset: 0 },
      { scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], entryIds: ["L-2"], limit: 1, offset: 0 },
    ]);
    expect(scroller.scrollTop).toBe(240);
  });

  it("失效 on a middle card, 所有人's in a member's section, keeps it on screen in its place, retired, although the default filter does not list 已失效", async () => {
    vi.spyOn(api, "setLoreEntryState").mockResolvedValue(undefined);
    const everyone = { scopeKind: "everyone" as const, scopeKey: "" };
    const three = page([
      mkEntry({ id: "L-1", state: "active" }),
      mkEntry({ id: "L-2", state: "active", ...everyone }),
      mkEntry({ id: "L-3", state: "active" }),
    ]);
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      o?.entryIds ? page([mkEntry({ id: "L-2", state: "retired", ...everyone })]) : three,
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(3));

    chooseState(container, "L-2", "retired");

    await waitFor(() =>
      expect(rowById(container, "L-2").querySelector('[data-testid="lore-state"]')!.textContent).toBe(
        zh.lore.stateRetired,
      ),
    );
    expect(renderedIds(container)).toEqual(["L-1", "L-2", "L-3"]);
    expect(rowById(container, "L-2").querySelector('[data-testid="lore-scope-name"]')!.textContent).toBe("所有人");
    expect(list.mock.calls.map((c) => c[0])).toEqual([
      { states: ["pinned", "active"], scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], limit: 30, offset: 0 },
      { scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], entryIds: ["L-2"], limit: 1, offset: 0 },
    ]);
  });

  // ── 403 必須讀起來像被拒絕 ────────────────────────────────────────────────
  it("a refused 置頂 SAYS SO — the error line is on screen and carries words", async () => {
    vi.spyOn(api, "setLoreEntryState").mockRejectedValue(
      new ApiError("http 403 for POST /api/lore/L-2/state", 403, "forbidden", ""),
    );
    const { container } = await renderTwo("active");
    expect(
      container.querySelector('[data-testid="lore-action-error"]'),
    ).toBeNull();

    chooseState(container, "L-2", "pinned");

    const line = await waitFor(() => {
      const el = container.querySelector<HTMLElement>(
        '[data-testid="lore-action-error"]',
      );
      if (!el) throw new Error("no action error line");
      return el;
    });
    // Presence alone is not the property: an empty red div reads as a glitch,
    // not as a refusal. The envelope carried no sentence here, so this is our
    // own copy — and it must be the REFUSAL copy, not the generic failure one.
    expect(line.textContent).toBe(zh.lore.forbidden);
    expect(line.textContent).not.toBe(zh.lore.actionFailed);
  });

  // ── 成功之後清單真的重讀 ──────────────────────────────────────────────────
  // ── 失敗之後畫面沒有假裝成功 ──────────────────────────────────────────────
  it("a refused action leaves the badge on its OLD value and re-asks nothing", async () => {
    // 🔴 READ, NOT ASSUMED: `runAction` writes NOTHING to `entries` before the
    // await — the badge is rendered from the server's `entry.state`, and the
    // only writer is the list load. So there is no optimistic value to roll
    // back, and the property to pin is that none appears. `setReloadNonce` is
    // inside the try AFTER the await, so a refusal must not re-read either —
    // a re-read on failure would repaint the row and read as "it went through,
    // then came back".
    vi.spyOn(api, "setLoreEntryState").mockRejectedValue(
      new ApiError("http 403 for POST /api/lore/L-2/state", 403, "forbidden", ""),
    );
    const { container, list } = await renderTwo("active");
    const badge = () =>
      rowById(container, "L-2").querySelector('[data-testid="lore-state"]')!
        .textContent;
    expect(badge()).toBe(zh.lore.stateActive);

    chooseState(container, "L-2", "pinned");

    await waitFor(() =>
      expect(
        container.querySelector('[data-testid="lore-action-error"]'),
      ).not.toBeNull(),
    );
    // Not 置頂. Not a row that moved into the 置頂 group.
    expect(badge()).toBe(zh.lore.stateActive);
    expect(renderedIds(container)).toEqual(["L-1", "L-2"]);
    expect(list).toHaveBeenCalledTimes(1);
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

  it("picking another scope sends it for that entry, closes the menu and updates that card in place without re-asking page one", async () => {
    const set = vi.spyOn(api, "setLoreEntryScope").mockResolvedValue(undefined);
    const { container, list } = await renderRows([taskBound, staffNoTask]);
    list.mockImplementation(async (o) =>
      o?.entryIds
        ? page([{ ...staffNoTask, scopeKind: "everyone", scopeKey: "" }])
        : page([taskBound, staffNoTask]),
    );

    const menu = openScopeMenu(container, "L-2");
    fireEvent.click(menu.querySelector('[data-testid="lore-scope-everyone"]')!);

    await waitFor(() => expect(list).toHaveBeenCalledTimes(2));
    await waitFor(() =>
      expect(
        readOptions(openScopeMenu(container, "L-2")).find((o) => o[3] === "true")?.[0],
      ).toBe("lore-scope-everyone"),
    );
    fireEvent.mouseDown(document.body);
    expect(set.mock.calls).toEqual([["L-2", "everyone"]]);
    // The card shows no scope, so the notice is what says the change landed.
    const toast = container.querySelector('[data-testid="lore-toast"]')!;
    const everyoneLabel = readOptions(openScopeMenu(container, "L-2")).find((o) => o[0] === "lore-scope-everyone")![1];
    fireEvent.mouseDown(document.body);
    expect(toast.textContent).toBe(`L-2 的適用範圍改成「${everyoneLabel}」`);
    expect(list.mock.calls).toEqual([
      [{ states: ["pinned", "active"], scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], limit: 30, offset: 0 }],
      [{ scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], entryIds: ["L-2"], limit: 1, offset: 0 }],
    ]);
    expect(renderedIds(container)).toEqual(["L-1", "L-2"]);
    expect(
      container.querySelector('[data-testid="lore-scope-options"]'),
    ).toBeNull();
  });

  it("picking the current scope closes the menu and sends nothing", async () => {
    const set = vi.spyOn(api, "setLoreEntryScope").mockResolvedValue(undefined);
    const { container } = await renderRows([staffNoTask]);

    const menu = openScopeMenu(container, "L-2");
    fireEvent.click(menu.querySelector('[data-testid="lore-scope-agent"]')!);

    expect(
      container.querySelector('[data-testid="lore-scope-options"]'),
    ).toBeNull();
    expect(set).not.toHaveBeenCalled();
  });

  it("a refused switch shows the server's reason and does not reload the list", async () => {
    vi.spyOn(api, "setLoreEntryScope").mockRejectedValue(
      new ApiError(
        "http 400 for POST /api/lore/L-2/scope",
        400,
        codeForStatus(400),
        "lore entry L-2 has no task type to key a manual scope to",
      ),
    );
    const { container, list } = await renderRows([staffNoTask]);
    expect(container.querySelector('[data-testid="lore-action-error"]')).toBeNull();

    fireEvent.click(
      openScopeMenu(container, "L-2").querySelector(
        '[data-testid="lore-scope-everyone"]',
      )!,
    );

    await waitFor(() =>
      expect(
        container.querySelector('[data-testid="lore-action-error"]')?.textContent,
      ).toBe("lore entry L-2 has no task type to key a manual scope to"),
    );
    expect(list).toHaveBeenCalledTimes(1);
    expect(
      readOptions(openScopeMenu(container, "L-2")).find((o) => o[3] === "true")?.[0],
    ).toBe("lore-scope-agent");
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

/** Click a directory row; the section page replaces the directory. */
function openSection(container: HTMLElement, key: string) {
  const target = container.querySelector<HTMLElement>(`[data-testid="lore-target"][data-section="${key}"]`);
  if (!target) throw new Error(`no directory row ${key}`);
  fireEvent.click(target);
}

function searchFor(container: HTMLElement, testId: string, text: string) {
  const box = container.querySelector<HTMLInputElement>(`[data-testid="${testId}"]`)!;
  fireEvent.change(box, { target: { value: text } });
  fireEvent.keyDown(box, { key: "Enter" });
}

/** Mira's section as the server would answer it: the 上限線 at L-212. */
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
    expect(
      Array.from(container.querySelectorAll(".lore__group-title")).map((el) => el.textContent),
    ).toEqual(["所有人 · 1", "成員 · 2", "任務手冊 · 1"]);
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

  it("a directory card's bar stops at full when a lowered cap leaves the target over it, and a zero cap shows only the size", async () => {
    stubTargets();
    vi.spyOn(api, "listLoreBudgets").mockResolvedValue([
      { scopeKind: "agent", scopeKey: "mira", capChars: 1000, usedChars: 1300, entryCount: 3, droppedCount: 1 },
      { scopeKind: "manual", scopeKey: "review-pr", capChars: 0, usedChars: 320, entryCount: 2, droppedCount: 0 },
    ]);
    stubList(page([]));
    const { container } = renderPage();

    await waitFor(() => expect(headerTexts(container)).toHaveLength(2));
    const cards = Array.from(container.querySelectorAll('[data-testid="lore-target"]'));
    expect((cards[0].querySelector(".lore-dir-card__bar-fill") as HTMLElement).style.width).toBe("100%");
    expect(cards[1].querySelector(".lore-dir-card__bar-fill")).toBeNull();
    expect(cards[1].querySelector('[data-testid="lore-section-usage"]')!.textContent).toBe("320 字");
  });

  it("a manual section's name opens that manual", async () => {
    stubTargets();
    stubList(page([mkEntry({ id: "L-8", scopeKind: "manual", scopeKey: "review-pr" })], { capChars: 10000, firstDroppedId: "" }));
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    openSection(container, "manual:review-pr");
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-8"]));
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

  it("opening a member's section lists 所有人's entries one by one among the member's own in the server's order, tags them 所有人, and draws the line above the first entry that cannot be read even when it is 所有人's", async () => {
    stubTargets();
    const everyone = { scopeKind: "everyone" as const, scopeKey: "" };
    const list = stubList(
      miraPage(
        [
          mkEntry({ id: "L-198", state: "pinned" }),
          mkEntry({ id: "L-240", ...everyone }),
          mkEntry({ id: "L-231" }),
          mkEntry({ id: "L-219", ...everyone }),
          mkEntry({ id: "L-212" }),
          mkEntry({ id: "L-205", ...everyone }),
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
    const head = container.querySelector<HTMLElement>('[data-testid="lore-section-head"]')!;
    expect(head.querySelector('[data-testid="lore-section-name"]')!.textContent).toBe("Mira");
    expect(head.querySelector('[data-testid="lore-section-count"]')!.textContent).toBe("11 筆");
    expect(head.querySelector('[data-testid="lore-section-dropped"]')!.textContent).toBe("4 筆讀不到");
    expect(list.mock.calls).toEqual([
      [{ states: ["pinned", "active"], scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], limit: 30, offset: 0 }],
    ]);
    expect(head.querySelector<HTMLElement>('[data-testid="lore-bar-everyone"]')!.style.width).toBe("60%");
    expect(head.querySelector<HTMLElement>('[data-testid="lore-bar-own"]')!.style.width).toBe("34%");
    expect(
      Array.from(head.querySelectorAll('[data-testid="lore-section-summary"] > *')).map((el) => el.textContent),
    ).toEqual([
      "所有人 14 筆，讀得到 12 筆 6,000 字",
      "Mira 11 筆，讀得到 7 筆 3,400 字",
      "上限 10,000 字，剩 600 字",
    ]);
    expect(container.querySelector('[data-testid="lore-everyone-ref"]')).toBeNull();
    const sequence = Array.from(
      body.querySelectorAll('[data-testid="lore-row"], [data-testid="lore-cap-line"]'),
    ).map((el) => el.getAttribute("data-entry-id") ?? el.getAttribute("data-testid"));
    expect(sequence).toEqual([
      "L-198",
      "L-240",
      "L-231",
      "lore-cap-line",
      "L-219",
      "L-212",
      "L-205",
    ]);
    expect(body.querySelector('[data-testid="lore-cap-line"]')!.textContent).toBe(
      "上限線　已用 9,400 / 10,000 字・L-219 需要 900 字，從這裡以下讀不到",
    );
    expect(
      renderedIds(body).map((id) => [
        id,
        rowById(body, id).querySelector('[data-testid="lore-scope-name"]')!.textContent,
        rowById(body, id).dataset.dimmed,
      ]),
    ).toEqual([
      ["L-198", "Mira", "false"],
      ["L-240", "所有人", "false"],
      ["L-231", "Mira", "false"],
      ["L-219", "所有人", "true"],
      ["L-212", "Mira", "true"],
      ["L-205", "所有人", "true"],
    ]);
    expect(body.querySelector('[data-testid="lore-cap-away"]')).toBeNull();
  });

  it("when the first entry that cannot be read is not among the rows, draws no line and says where the line is; the next page brings the line in", async () => {
    stubTargets();
    const first = Array.from({ length: 30 }, (_, i) => mkEntry({ id: `L-${100 + i}` }));
    const list = vi
      .spyOn(api, "listLoreEntries")
      .mockImplementation(async (o) =>
        (o?.offset ?? 0) === 0
          ? miraPage(first)
          : miraPage([mkEntry({ id: "L-212" }), mkEntry({ id: "L-213" })], { offset: 30 }),
      );
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    openSection(container, "agent:mira");
    await waitFor(() => expect(renderedIds(container)).toHaveLength(30));

    expect(container.querySelector('[data-testid="lore-cap-line"]')).toBeNull();
    expect(container.querySelector('[data-testid="lore-cap-away"]')!.textContent).toBe(
      "上限線在 L-212，不在目前的結果裡",
    );
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
    const line = container.querySelector('[data-testid="lore-cap-line"]')!;
    expect(line.compareDocumentPosition(rowById(container, "L-212")) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(rowById(container, "L-129").dataset.dimmed).toBe("false");
    expect(rowById(container, "L-212").dataset.dimmed).toBe("true");
    expect(container.querySelector('[data-testid="lore-cap-away"]')).toBeNull();
  });

  it("when 所有人's newer entries fill the cap, the bar is all 所有人 and every card from the line down is dimmed, 所有人's or the member's own", async () => {
    stubTargets();
    const everyone = { scopeKind: "everyone" as const, scopeKey: "" };
    stubList(
      miraPage([mkEntry({ id: "L-240", ...everyone }), mkEntry({ id: "L-231", ...everyone }), mkEntry({ id: "L-212" })], {
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
    expect(
      Array.from(head.querySelectorAll('[data-testid="lore-section-summary"] > *')).map((el) => el.textContent),
    ).toEqual([
      "所有人 20 筆，讀得到 14 筆 9,800 字",
      "Mira 2 筆，讀得到 0 筆 0 字",
      "上限 10,000 字，剩 200 字",
    ]);
    expect(head.querySelector<HTMLElement>('[data-testid="lore-bar-everyone"]')!.style.width).toBe("98%");
    expect(head.querySelector<HTMLElement>('[data-testid="lore-bar-own"]')!.style.width).toBe("0%");
    expect(
      Array.from(
        body.querySelectorAll('[data-testid="lore-row"], [data-testid="lore-cap-line"]'),
      ).map((el) => el.getAttribute("data-entry-id") ?? el.getAttribute("data-testid")),
    ).toEqual(["L-240", "lore-cap-line", "L-231", "L-212"]);
    expect(renderedIds(body).map((id) => rowById(body, id).dataset.dimmed)).toEqual(["false", "true", "true"]);
  });

  it("a title search whose results leave out the cap-line entry says where the line is and dims nothing", async () => {
    stubTargets();
    vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      o?.q === "審查" && o.scopeKeys?.[0] === "mira"
        ? miraPage([mkEntry({ id: "L-231" }), mkEntry({ id: "L-205" })])
        : page([]),
    );
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    openSection(container, "agent:mira");
    await waitFor(() => expect(container.querySelector('[data-testid="lore-filter-id"]')).not.toBeNull());

    searchFor(container, "lore-filter-id", "審查");

    await waitFor(() => expect(renderedIds(container)).toEqual(["L-231", "L-205"]));
    expect(container.querySelector('[data-testid="lore-cap-line"]')).toBeNull();
    expect(container.querySelector('[data-testid="lore-cap-away"]')!.textContent).toBe(
      "上限線在 L-212，不在目前的結果裡",
    );
    expect(renderedIds(container).map((id) => rowById(container, id).dataset.dimmed)).toEqual([
      "false",
      "false",
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

    fireEvent.click(container.querySelector<HTMLElement>('[data-testid="lore-filter-state"]')!);
    fireEvent.click(
      container.querySelector<HTMLElement>('[data-testid="lore-filter-state-opt-retired"] input')!,
    );

    await waitFor(() => expect(renderedIds(container)).toEqual(["L-1", "L-2", "L-9"]));
    // Its reason is its own line under the meta.
    expect(rowById(container, "L-9").querySelector('[data-testid="lore-retire-reason"]')!.textContent).toBe(
      "失效理由：併進 SOP",
    );
  });

  it("an id typed on the directory opens that entry's section page with the entry highlighted", async () => {
    stubTargets();
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      o?.scopeKeys === undefined
        ? page([mkEntry({ id: "L-212", scopeKind: "agent", scopeKey: "mira" })])
        : miraPage([mkEntry({ id: "L-198" }), mkEntry({ id: "L-212" })]),
    );
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));

    searchFor(container, "lore-directory-id", "212");

    await waitFor(() => expect(window.location.hash).toBe("#lore/agent/mira/entry/L-212"));
    expect(list.mock.calls[0][0]).toEqual({
      entryIds: ["L-212"], states: ["pinned", "active", "retired"], limit: 1, offset: 0,
    });
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-198", "L-212"]));
    const row = rowById(container, "L-212");
    expect(row.classList.contains("lore-row--highlight")).toBe(true);
    expect(rowById(container, "L-198").classList.contains("lore-row--highlight")).toBe(false);
    await waitFor(() => expect(document.activeElement).toBe(row));
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
    searchFor(container, "lore-directory-id", "L-9");

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
    searchFor(container, "lore-directory-id", "L-5");
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
    const { container } = renderPage();
    await waitFor(() => expect(list).toHaveBeenCalledTimes(2));

    fireEvent.click(container.querySelector<HTMLElement>('[data-testid="lore-filter-state"]')!);
    fireEvent.click(container.querySelector<HTMLElement>('[data-testid="lore-filter-state-opt-retired"] input')!);
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
    searchFor(container, "lore-directory-id", "L-9");

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

    searchFor(container, "lore-directory-id", "審查");
    expect(container.querySelector('[data-testid="lore-directory-note"]')!.textContent).toBe(
      "這裡只找條目編號（例如 L-212）；要用標題找，先進到一段裡",
    );
    expect(list).not.toHaveBeenCalled();

    searchFor(container, "lore-directory-id", "L-999");
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
      searchFor(container, "lore-filter-id", typed);
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
    await waitFor(() => expect(container.querySelector('[data-testid="lore-filter-id"]')).not.toBeNull());

    searchFor(container, "lore-filter-id", "L-999");

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

    searchFor(container, "lore-filter-id", "審查");
    await waitFor(() => expect(list).toHaveBeenCalledTimes(2));

    fireEvent.click(container.querySelector('[data-testid="lore-back"]')!);
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    expect(window.location.hash).toBe("#lore");

    openSection(container, "agent:mira");
    await waitFor(() => expect(list).toHaveBeenCalledTimes(3));
    expect(list.mock.calls[2][0]).toEqual({
      states: ["pinned", "active"], scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], limit: 30, offset: 0,
    });
    expect(container.querySelector<HTMLInputElement>('[data-testid="lore-filter-id"]')!.value).toBe("");
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
    mkEntry({ id: "L-1", title: "第一筆", body: "一" }),
    mkEntry({ id: "L-2", title: "AAAA", body: "BBBBBBBBBBBBBBBB" }),
    mkEntry({ id: "L-3", title: "第三筆", body: "三" }),
  ];
  const cap = { capChars: 1000, firstDroppedId: "L-3" };

  it("saving updates that card where it stands, keeps the others in place and re-reads only that entry", async () => {
    const edit = vi.spyOn(api, "editLoreEntry").mockResolvedValue(undefined);
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      o?.entryIds
        ? { ...page([mkEntry({ id: "L-2", title: "AAAA", body: "BB" })], cap), usedChars: 976, firstDroppedChars: 0, firstDroppedId: "" }
        : { ...page(rows(), cap), usedChars: 990, firstDroppedChars: 20 },
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-1", "L-2", "L-3"]));
    expect(container.querySelector('[data-testid="lore-cap-line"]')).not.toBeNull();

    fireEvent.click(rowById(container, "L-2").querySelector('[data-testid="lore-edit"]')!);
    const editor = rowById(container, "L-2").querySelector<HTMLElement>('[data-testid="lore-editor"]')!;
    expect(editor.querySelector<HTMLInputElement>('[data-testid="lore-edit-title"]')!.value).toBe("AAAA");
    fireEvent.change(editor.querySelector('[data-testid="lore-edit-body"]')!, { target: { value: "BB" } });

    fireEvent.click(editor.querySelector('[data-testid="lore-edit-save"]')!);

    await waitFor(() =>
      expect(rowById(container, "L-2").querySelector('[data-testid="lore-editor"]')).toBeNull(),
    );
    expect(edit.mock.calls).toEqual([["L-2", { body: "BB" }]]);
    expect(rowById(container, "L-2").querySelector('[data-testid="lore-body"]')!.textContent).toBe("BB");
    expect(renderedIds(container)).toEqual(["L-1", "L-2", "L-3"]);
    expect(list.mock.calls.map((c) => c[0])).toEqual([
      { states: ["pinned", "active"], scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], limit: 30, offset: 0 },
      { scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], entryIds: ["L-2"], limit: 1, offset: 0 },
    ]);
    await waitFor(() => expect(container.querySelector('[data-testid="lore-cap-line"]')).toBeNull());
  });

  it("edits title and body only: no type control, no character counters, no effect sentence", async () => {
    const edit = vi.spyOn(api, "editLoreEntry").mockResolvedValue(undefined);
    stubList({ ...page(rows(), cap), usedChars: 990, firstDroppedChars: 4 });
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(3));

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
    const longBody = "長".repeat(600);
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

    fireEvent.change(editor.querySelector('[data-testid="lore-edit-body"]')!, { target: { value: "長".repeat(550) } });
    expect(save.disabled).toBe(false);
    fireEvent.click(save);
    await waitFor(() => expect(edit).toHaveBeenCalled());
    expect(edit.mock.calls).toEqual([["L-2", { title: "BBBB", body: "長".repeat(550) }]]);
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

  it("choosing a type sends only loreType and re-reads that card where it stands", async () => {
    const edit = vi.spyOn(api, "editLoreEntry").mockResolvedValue(undefined);
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      o?.entryIds
        ? page([mkEntry({ id: "L-2", loreType: "owner_decision", effectiveTs: 2 })])
        : page(rows()),
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-1", "L-2", "L-3"]));
    const before = rowById(container, "L-2");

    fireEvent.click(before.querySelector('[data-testid="lore-type"]')!);
    fireEvent.click(before.querySelector('[data-testid="lore-type-owner_decision"]')!);

    await waitFor(() =>
      expect(rowById(container, "L-2").querySelector('[data-testid="lore-type"]')!.textContent).toBe(
        "Owner 決策",
      ),
    );
    expect(edit.mock.calls).toEqual([["L-2", { loreType: "owner_decision" }]]);
    expect(rowById(container, "L-2")).toBe(before);
    expect(renderedIds(container)).toEqual(["L-1", "L-2", "L-3"]);
    expect(list.mock.calls[1][0]).toEqual({
      scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], entryIds: ["L-2"], limit: 1, offset: 0,
    });
    expect(before.querySelector('[data-testid="lore-type-options"]')).toBeNull();
  });

  it("choosing the current type sends nothing", async () => {
    const edit = vi.spyOn(api, "editLoreEntry").mockResolvedValue(undefined);
    stubList(page(rows()));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(3));
    const row = rowById(container, "L-2");
    fireEvent.click(row.querySelector('[data-testid="lore-type"]')!);
    fireEvent.click(row.querySelector('[data-testid="lore-type-other"]')!);
    expect(row.querySelector('[data-testid="lore-type-options"]')).toBeNull();
    expect(edit).not.toHaveBeenCalled();
  });

  it("a refused type change says so on the action error line", async () => {
    vi.spyOn(api, "editLoreEntry").mockRejectedValue(
      new ApiError("http 403 for PATCH /api/lore/L-2", 403, "forbidden", ""),
    );
    stubList(page(rows()));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(3));
    const row = rowById(container, "L-2");
    fireEvent.click(row.querySelector('[data-testid="lore-type"]')!);
    fireEvent.click(row.querySelector('[data-testid="lore-type-instruction_conflict"]')!);
    await waitFor(() =>
      expect(container.querySelector('[data-testid="lore-action-error"]')?.textContent).toBe(
        zh.lore.forbidden,
      ),
    );
    expect(row.querySelector('[data-testid="lore-type"]')!.textContent).toBe("其他");
  });
});

/** The head's only action is 編輯, drawn as every other 編輯 in the app
 * (settings.css .doc-btn--edit: pencil + label), at every width. */
function expectOnlyEditAction(row: HTMLElement) {
  const actions = row.querySelector<HTMLElement>(".lore-row__actions")!;
  const buttons = Array.from(actions.querySelectorAll("button"));
  expect(buttons.map((b) => [b.getAttribute("data-testid"), b.className, b.textContent])).toEqual([
    ["lore-edit", "doc-btn doc-btn--edit", "編輯"],
  ]);
  expect(buttons[0].querySelector("svg")).not.toBeNull();
}

describe("LorePage — 卡片右上只有編輯", () => {
  it("on a desktop the head carries one 編輯 button and it opens the editor", async () => {
    stubList(
      page([mkEntry({ id: "L-1", scopeKind: "agent", scopeKey: "mira", scopeOptions: ["agent", "everyone"] })]),
    );
    const { container } = await renderOpen({ canSetScope: true });
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const row = rowById(container, "L-1");
    expectOnlyEditAction(row);
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

  it("shows the same single 編輯 button as a desktop, no 「⋯」 menu, and 編輯 opens the editor", async () => {
    stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = await renderOpen({ canSetScope: true });
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const row = rowById(container, "L-1");
    expectOnlyEditAction(row);
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

  it("is reached from the editor, shows the old title and body, compares them with the card, and a restore leaves edit mode with them back on that card in place", async () => {
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
      { scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], entryIds: ["L-2"], limit: 1, offset: 0 },
    ]);
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
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

function chooseStateOn(container: HTMLElement, entryId: string, next: LoreEntryView["state"]) {
  const row = rowById(container, entryId);
  fireEvent.click(row.querySelector('[data-testid="lore-state"]')!);
  fireEvent.click(row.querySelector(`[data-testid="lore-state-${next}"]`)!);
}

function search(container: HTMLElement, text: string) {
  const box = container.querySelector<HTMLInputElement>('[data-testid="lore-filter-id"]')!;
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
    await waitFor(() => expect(container.querySelector('[data-testid="lore-filter-id"]')).not.toBeNull());

    search(container, "bbb");
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

  it("an older refresh answering last does not bring back its cap line or its header numbers", async () => {
    vi.spyOn(api, "setLoreEntryState").mockResolvedValue(undefined);
    const firstRefresh = held<LoreEntryPageView>();
    const firstBudgets = held<LoreBudgetTargetView[]>();
    const rows = [mkEntry({ id: "L-1" }), mkEntry({ id: "L-2" })];
    const lineAtL2 = page(rows, { capChars: 1000, firstDroppedId: "L-2" });
    vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) => {
      if (o?.entryIds?.[0] === "L-1") return firstRefresh.promise;
      if (o?.entryIds?.[0] === "L-2") return page([mkEntry({ id: "L-2", state: "retired" })], { capChars: 1000, firstDroppedId: "" });
      return lineAtL2;
    });
    let budgetCalls = 0;
    vi.spyOn(api, "listLoreBudgets").mockImplementation(async () => {
      budgetCalls += 1;
      if (budgetCalls === 2) return firstBudgets.promise;
      return [{ ...MIRA_TARGET, entryCount: budgetCalls === 1 ? 2 : 5 }];
    });
    const { container } = await renderOpen();
    await waitFor(() => expect(container.querySelector('[data-testid="lore-cap-line"]')).not.toBeNull());

    chooseStateOn(container, "L-1", "retired");
    await waitFor(() => expect(budgetCalls).toBe(2));
    chooseStateOn(container, "L-2", "retired");
    await waitFor(() => expect(container.querySelector('[data-testid="lore-cap-line"]')).toBeNull());
    await waitFor(() => expect(sectionHead(container)).toContain("5 筆"));

    firstRefresh.resolve(lineAtL2);
    firstBudgets.resolve([{ ...MIRA_TARGET, entryCount: 2 }]);
    await new Promise((r) => setTimeout(r, 20));

    expect(container.querySelector('[data-testid="lore-cap-line"]')).toBeNull();
    expect(sectionHead(container)).toContain("5 筆");
  });

  it("a next page answering after a refresh adds its rows but keeps the refresh's cap", async () => {
    vi.spyOn(api, "setLoreEntryState").mockResolvedValue(undefined);
    const first = Array.from({ length: 30 }, (_, i) => mkEntry({ id: `L-${i + 1}` }));
    const nextPage = held<LoreEntryPageView>();
    vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) => {
      if (o?.entryIds) return page([mkEntry({ id: "L-1", state: "retired" })], { capChars: 1000, firstDroppedId: "" });
      if (o?.offset === 30) return nextPage.promise;
      return page(first, { capChars: 1000, firstDroppedId: "L-2" });
    });
    const { container } = await renderOpen();
    await waitFor(() => expect(container.querySelector('[data-testid="lore-cap-line"]')).not.toBeNull());

    fireEvent.scroll(container.querySelector('[data-testid="lore-page"]')!);
    chooseStateOn(container, "L-1", "retired");
    await waitFor(() => expect(container.querySelector('[data-testid="lore-cap-line"]')).toBeNull());
    nextPage.resolve(page([mkEntry({ id: "L-31" })], { capChars: 1000, firstDroppedId: "L-2" }));

    await waitFor(() => expect(renderedIds(container)).toHaveLength(31));
    expect(container.querySelector('[data-testid="lore-cap-line"]')).toBeNull();
  });
});

