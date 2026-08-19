// The journal cards must show WHY a save failed, not just THAT it failed.
//
// Both cards used to hold `saveError` as a boolean and render one fixed i18n
// string. The server's refusals are not shaped like that: the doc-cap guard
// answers with real instructions (how far over the limit this write is, what
// the cap is, that what is already stored has NOT been truncated, that stale
// content should be deleted first). Structurally, none of it could reach the
// screen — the reason was thrown away at `catch {}` and the person was told
// only「儲存判準失敗」, which does not tell them what to do next.
//
// This file is the discrimination the boolean version could never pass: it
// asserts the SERVER'S OWN WORDS appear. A card that renders the fixed string
// fails here, and that is the whole point — reverting the render to
// `{t.mp.insightSaveError}` turns these two "reason" cases red on exactly the
// `getByText(REASON)` line.
//
// The fallback cases are the other half, and they are not optional: an error
// path that shows the server message must still say SOMETHING when there is no
// server message (a network throw, a proxy error page with no envelope). An
// empty red line would be a worse regression than the boolean it replaced.
//
// Insight and Learning are asserted together because they are the same editor
// over different documents — fixing one and not the other is how the two drift.

import { describe, it, expect, beforeEach, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { InsightCard } from "./InsightCard";
import { LessonsCard } from "./LessonsCard";
import { __resetMock, mockApi } from "../api/mock";
import { mockApiError } from "../api/errorCodes";

const s = zh.settings;
const mp = zh.mp;

/** A refusal shaped like the server's doc-cap guard: the instructions are the
 * payload, and they are what must survive the trip to the screen. */
const REASON =
  "判準超過上限 42 字（上限 1000 字）。已存的內容不會被截斷；請先刪掉過時的段落再寫入。";

const apiError = (msg: string) =>
  mockApiError("http 400 for POST /api/insight/assistant", 400, msg);

beforeEach(() => {
  __resetMock();
});

afterEach(() => {
  vi.restoreAllMocks();
});

/** Render a card, wait for its load to land, and open the editor. */
async function openEditor(ui: React.ReactElement) {
  const utils = render(<I18nProvider>{ui}</I18nProvider>);
  const editBtn = await utils.findByText(s.edit);
  await waitFor(() => expect(editBtn.closest("button")!.disabled).toBe(false));
  fireEvent.click(editBtn);
  fireEvent.change(utils.container.querySelector(".doc-editor")!, {
    target: { value: "一段新的內容" },
  });
  return utils;
}

async function failSaveAndRead(
  ui: React.ReactElement,
  spy: () => void
): Promise<HTMLElement> {
  spy();
  const utils = await openEditor(ui);
  fireEvent.click(utils.getByText(s.doneEdit));
  return await waitFor(() => {
    const line = utils.container.querySelector(".mp-lessons__error");
    expect(line).toBeTruthy();
    return line as HTMLElement;
  });
}

describe("journal cards · a failed save shows the server's REASON", () => {
  it("Insight: the server's message is on the screen", async () => {
    const line = await failSaveAndRead(<InsightCard roleKey="assistant" />, () =>
      vi.spyOn(mockApi, "saveInsight").mockRejectedValue(apiError(REASON))
    );
    // 🔴 THE assertion. The boolean version renders mp.insightSaveError here.
    expect(line.textContent).toBe(REASON);
    // ...and the generic copy is really gone, so the line above is measuring a
    // substitution rather than an addition.
    expect(line.textContent).not.toContain(mp.insightSaveError);
  });

  it("Insight: falls back to the i18n copy when there is no server message", async () => {
    const line = await failSaveAndRead(<InsightCard roleKey="assistant" />, () =>
      vi.spyOn(mockApi, "saveInsight").mockRejectedValue(new Error("network down"))
    );
    // Never blank: an empty red line is worse than a generic one.
    expect(line.textContent?.trim()).toBe(mp.insightSaveError);
  });

  it("Learning: the server's message is on the screen", async () => {
    const line = await failSaveAndRead(<LessonsCard roleKey="assistant" />, () =>
      vi.spyOn(mockApi, "saveLessons").mockRejectedValue(apiError(REASON))
    );
    expect(line.textContent).toBe(REASON);
    expect(line.textContent).not.toContain(mp.lessonsSaveError);
  });

  it("Learning: falls back to the i18n copy when there is no server message", async () => {
    const line = await failSaveAndRead(<LessonsCard roleKey="assistant" />, () =>
      vi.spyOn(mockApi, "saveLessons").mockRejectedValue(new Error("network down"))
    );
    expect(line.textContent?.trim()).toBe(mp.lessonsSaveError);
  });

  it("a SUCCESSFUL save shows no error line at all", async () => {
    // The anti-tautology for every assertion above: a card that rendered the
    // error line unconditionally would satisfy all four.
    const utils = await openEditor(<InsightCard roleKey="assistant" />);
    fireEvent.click(utils.getByText(s.doneEdit));
    await waitFor(() =>
      expect(utils.container.querySelector(".doc-editor")).toBeNull()
    );
    expect(utils.container.querySelector(".mp-lessons__error")).toBeNull();
    expect(
      within(utils.container).queryByText(mp.insightSaveError)
    ).toBeNull();
  });
});
