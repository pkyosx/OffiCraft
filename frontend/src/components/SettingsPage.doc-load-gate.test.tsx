// T-2d99 (F-4): the doc editor must not open over a doc that has not loaded.
//
// DocDetail derives `text` as `doc ? doc.text : ""`, so a NULL doc — the mount
// fetch still in flight, or failed — is indistinguishable from an empty one
// once startEdit() has seeded the draft. Clicking 編輯 during the load then
// opened a BLANK editor over content the owner had never seen, and committing
// it sent a whole-doc replace of "". That write cannot be caught server-side
// either: this call site passes allow_shrink: true (a human clearing a textarea
// is deliberate), which is exactly the flag that waives the T-2d99 wipe guard.
//
// So the guard has to sit where the intent is actually known: you cannot edit
// what has not arrived. LessonsCard already gates its pencil on
// `loading || error`; DocDetail was the one editable doc surface that did not.
//
// This test hangs getGlobalContext forever, so the 使用者自訂 block stays null
// for the whole test — the load-pending state, held still.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock } from "../api/mock";

const s = zh.settings;

// Everything rides the real mock adapter EXCEPT the global-context read, which
// never settles. Keeping the rest real means this test exercises the true
// SettingsPage wiring rather than a hand-built stand-in.
vi.mock("../api", async (importOriginal) => {
  const mod = await importOriginal<typeof import("../api")>();
  return {
    ...mod,
    api: {
      ...mod.api,
      getGlobalContext: () => new Promise(() => {}),
    },
  };
});

beforeEach(() => {
  __resetMock();
});

describe("SettingsPage · DocDetail load gate (T-2d99)", () => {
  it("編輯 is disabled while the doc is still loading, so no blank save can fire", async () => {
    const utils = render(
      <I18nProvider>
        <SettingsPage />
      </I18nProvider>
    );
    fireEvent.click(utils.getByText(s.roles));
    await utils.findByText(s.systemName);
    fireEvent.click(utils.getByText(s.customName));

    // The affordance still RENDERS (so the owner sees the page is editable),
    // but it must not be actionable until the doc lands.
    const edit = await utils.findByText(s.edit);
    const button = edit.closest("button");
    expect(button).toBeTruthy();
    expect(button!.disabled).toBe(true);

    // The load-bearing half: clicking it must not open the editor. Without the
    // gate this click opens a textarea seeded with "" — the blank-save path.
    fireEvent.click(button!);
    expect(utils.container.querySelector("textarea")).toBeNull();
    // And the commit affordance never appears, so there is nothing to submit.
    expect(utils.queryByText(s.doneEdit)).toBeNull();
  });
});
