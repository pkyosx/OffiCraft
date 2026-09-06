// 換版交代單卡 (T-79) — the guard rails for the settings card.
//
// WHAT THESE PIN, and why each one is a failure nobody would notice:
//  1. The card is actually MOUNTED on 系統更新與備份. Everything else here
//     could pass with the card wired to nothing.
//  2. A FINISHED instruction stays in the list. Hiding it would make "she did
//     it" and "it was never written" look identical, and the finished row is
//     the only evidence the work was ever picked up.
//  3. The open count comes from the SERVER, not from counting rows. The two
//     agree today; a card that counts rows keeps agreeing right up until the
//     day either side pages the list, and then quietly disagrees.
//  4. An OPEN instruction never renders a date or an author. The wire says
//     "open" with 0/"" and the mapper narrows it — a card that printed those
//     shows 1970 and a blank name beside work nobody has done.
//  5. The draft survives a REJECTED write. Clearing it would throw away what
//     the owner typed with nothing to show for it, and the write that failed
//     is exactly the one he needs to retry.
//  6. Withdrawing shows the instruction's OWN WORDS, and only fires on
//     confirm. A confirmation naming an id is one nobody can check.
//  7. Both locales carry the two sentences that change what a reader does:
//     "no undo" and "this list does not update itself".

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";
import type { UpgradeInstructionView } from "../types";
import { __resetMock } from "../api/mock";

const state = {
  instructions: [] as UpgradeInstructionView[],
  openCount: 0,
  error: "",
  /** Whether the next write lands. Kept as data rather than by swapping the
      spy: the mocked hook is read at render time, and reassigning the spy
      mid-test leaves the mounted card holding the old one. */
  createOk: true,
  create: vi.fn(),
  markDone: vi.fn(),
  remove: vi.fn(),
};
vi.mock("../hooks/useUpgradeInstructions", () => ({
  useUpgradeInstructions: () => ({
    instructions: state.instructions,
    openCount: state.openCount,
    loading: false,
    busy: false,
    error: state.error,
    create: state.create,
    markDone: state.markDone,
    remove: state.remove,
  }),
}));

import { SettingsPage } from "./SettingsPage";

const s = zh.settings;
const u = zh.upgradeInstructions;

/** Two open and one already ticked — the shape that proves a finished
 * instruction is kept rather than hidden. */
function mixedSet(): UpgradeInstructionView[] {
  return [
    {
      id: "uin-open1",
      body: "換版後把 drift 檢查重跑一次",
      createdTs: 1788600000,
      createdBy: "owner",
      done: false,
      doneTs: null,
      doneBy: null,
    },
    {
      id: "uin-open2",
      body: "確認 T-33 的 migration 已經 land",
      createdTs: 1788640000,
      createdBy: "owner",
      done: false,
      doneTs: null,
      doneBy: null,
    },
    {
      id: "uin-done1",
      body: "把備份健康度的告警門檻調回 24 小時",
      createdTs: 1788520000,
      createdBy: "owner",
      done: true,
      doneTs: 1788530000,
      doneBy: "mira",
    },
  ];
}

async function openSection() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>,
  );
  fireEvent.click(utils.getByText(s.software));
  await utils.findByText(s.currentVersion);
  return utils;
}

beforeEach(() => {
  __resetMock();
  state.instructions = mixedSet();
  state.openCount = 2;
  state.error = "";
  state.createOk = true;
  state.create = vi.fn(() => Promise.resolve(state.createOk));
  state.markDone = vi.fn();
  state.remove = vi.fn();
});

describe("換版交代單卡 · 現況看得出來", () => {
  it("is mounted on 系統更新與備份, and lists every instruction including the finished one", async () => {
    await openSection();
    await waitFor(() => screen.getByTestId("set-upgrade-instructions"));

    expect(screen.getByTestId("set-upgrade-instruction-uin-open1")).toBeTruthy();
    expect(screen.getByTestId("set-upgrade-instruction-uin-open2")).toBeTruthy();
    // 🔴 The finished one is STILL HERE. Hide it and "she did it" becomes
    // indistinguishable from "it was never written".
    const finished = screen.getByTestId("set-upgrade-instruction-uin-done1");
    expect(finished).toBeTruthy();
    expect(finished.getAttribute("data-done")).toBe("yes");
    expect(finished.textContent).toContain(u.doneBadge);
  });

  it("reports the OPEN count the server gave, not the number of rows on screen", async () => {
    // Three rows, two open. A card that counts rows says three.
    await openSection();
    const count = await waitFor(() =>
      screen.getByTestId("set-upgrade-instructions-count"),
    );
    expect(count.textContent).toBe(u.openCountLabel(2));
    expect(count.textContent).not.toContain("3");
  });

  it("says 全部都完成了 rather than reporting a count of zero", async () => {
    // "0 still open" reads as a statistic; this is a state, and the state is
    // the thing the owner came to check.
    state.instructions = mixedSet().map((i) => ({
      ...i,
      done: true,
      doneTs: 1788530000,
      doneBy: "mira",
    }));
    state.openCount = 0;
    await openSection();
    const count = await waitFor(() =>
      screen.getByTestId("set-upgrade-instructions-count"),
    );
    expect(count.textContent).toBe(u.allDoneLabel);
  });

  it("never prints a tick time or a ticker beside an instruction nobody has touched", async () => {
    await openSection();
    const open = await waitFor(() =>
      screen.getByTestId("set-upgrade-instruction-uin-open1"),
    );
    // The wire says "open" with done_ts 0 / done_by ""; rendering those gives
    // 1970 and a blank author on work that has not happened.
    expect(open.textContent).not.toContain("1970");
    expect(open.textContent).not.toContain(u.doneLabel(""));
    expect(open.textContent).toContain(u.createdLabel);

    const done = screen.getByTestId("set-upgrade-instruction-uin-done1");
    expect(done.textContent).toContain(u.doneLabel("mira"));
  });
});

describe("換版交代單卡 · 寫一張", () => {
  it("keeps the draft when the write is REJECTED, and clears it only when it lands", async () => {
    state.createOk = false;
    await openSection();
    const input = (await waitFor(() =>
      screen.getByTestId("set-upgrade-instructions-input"),
    )) as HTMLTextAreaElement;

    fireEvent.change(input, { target: { value: "  換版後重跑 drift  " } });
    fireEvent.click(screen.getByTestId("set-upgrade-instructions-add"));

    // Trimmed on the way out — the server trims too, so sending the padding
    // would just make the two disagree about what was written.
    await waitFor(() =>
      expect(state.create).toHaveBeenCalledWith("換版後重跑 drift"),
    );
    // 🔴 Still there. The rejected write is exactly the one he has to retry.
    await waitFor(() => expect(input.value).toBe("  換版後重跑 drift  "));

    state.createOk = true;
    fireEvent.click(screen.getByTestId("set-upgrade-instructions-add"));
    await waitFor(() => expect(input.value).toBe(""));
  });

  it("refuses to send a blank one rather than letting the server say no", async () => {
    await openSection();
    const input = await waitFor(() =>
      screen.getByTestId("set-upgrade-instructions-input"),
    );
    const add = screen.getByTestId(
      "set-upgrade-instructions-add",
    ) as HTMLButtonElement;

    expect(add.disabled).toBe(true);
    fireEvent.change(input, { target: { value: "   " } });
    expect(add.disabled).toBe(true);
    fireEvent.change(input, { target: { value: "做點什麼" } });
    expect(add.disabled).toBe(false);
  });
});

describe("換版交代單卡 · 打勾與收回", () => {
  it("ticks straight through, because a tick is not destructive", async () => {
    await openSection();
    fireEvent.click(
      await waitFor(() => screen.getByTestId("set-upgrade-instruction-done-uin-open1")),
    );
    expect(state.markDone).toHaveBeenCalledWith("uin-open1");
    expect(screen.queryByTestId("set-upgrade-instruction-confirm")).toBeNull();
  });

  it("offers no tick button on an instruction that is already done", async () => {
    await openSection();
    await waitFor(() => screen.getByTestId("set-upgrade-instruction-uin-done1"));
    expect(
      screen.queryByTestId("set-upgrade-instruction-done-uin-done1"),
    ).toBeNull();
  });

  it("makes withdrawing quote the instruction itself, and fires only on confirm", async () => {
    await openSection();
    fireEvent.click(
      await waitFor(() =>
        screen.getByTestId("set-upgrade-instruction-remove-uin-open2"),
      ),
    );

    const modal = screen.getByTestId("set-upgrade-instruction-confirm");
    // 🔴 The words, not the id: a confirmation naming `uin-open2` is one
    // nobody can check before pressing it.
    expect(modal.textContent).toContain("確認 T-33 的 migration 已經 land");
    expect(modal.textContent).toContain(u.deleteConfirmTitle);
    // And it has to say what it costs — this is the only irreversible verb.
    expect(modal.textContent).toContain(u.deleteConfirmBody);
    expect(state.remove).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("set-upgrade-instruction-confirm-ok"));
    expect(state.remove).toHaveBeenCalledWith("uin-open2");
  });

  it("cancelling withdraws nothing", async () => {
    await openSection();
    fireEvent.click(
      await waitFor(() =>
        screen.getByTestId("set-upgrade-instruction-remove-uin-open2"),
      ),
    );
    fireEvent.click(screen.getByText(u.deleteConfirmCancel));
    expect(screen.queryByTestId("set-upgrade-instruction-confirm")).toBeNull();
    expect(state.remove).not.toHaveBeenCalled();
  });
});

describe("換版交代單卡 · 誠實", () => {
  it("shows the server's own refusal rather than swallowing it", async () => {
    state.error = "only the owner may write an upgrade instruction";
    await openSection();
    const line = await waitFor(() =>
      screen.getByTestId("set-upgrade-instructions-error"),
    );
    expect(line.textContent).toBe(
      "only the owner may write an upgrade instruction",
    );
  });

  it("says on screen that the list does not update itself", async () => {
    // 🔴 "she just ticked it" and "this list is stale" look identical here,
    // and only the second needs a reload. Without this sentence the owner has
    // no way to tell which one he is looking at.
    await openSection();
    const card = await waitFor(() =>
      screen.getByTestId("set-upgrade-instructions"),
    );
    expect(card.textContent).toContain(u.staleHint);
  });

  it("carries the two load-bearing sentences in BOTH locales", async () => {
    // The withdraw confirmation must say there is no undo, in either language.
    expect(zh.upgradeInstructions.deleteConfirmBody).toContain("永久刪除");
    expect(en.upgradeInstructions.deleteConfirmBody).toContain("no undo");
    // And the staleness caveat must survive translation rather than becoming
    // a vaguer sentence that no longer tells the reader to reload.
    expect(en.upgradeInstructions.staleHint).toContain("reload");
    expect(en.upgradeInstructions.deleteConfirmBody).not.toBe(
      zh.upgradeInstructions.deleteConfirmBody,
    );
  });
});
