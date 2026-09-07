// ModelEffortEditor — the shared model/effort picker's suggestion vocabulary.
//
// CODEX_MODEL_OPTIONS is the ONE definition of the Codex quick-pick chips
// (TaskManualsPage and TaskReassignDialog import it, and CodexModelSelect
// renders it). The identifier a chip reports is what ships to the server as the
// launch model, so it is pinned literally: `gpt-6-astra` is the slug the Codex
// app-server's `model/list` answers with, and a chip labelled right but
// reporting a stale slug would launch the wrong model silently.

import { describe, it, expect, vi } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { CODEX_MODEL_OPTIONS, CodexModelSelect } from "./ModelEffortEditor";

describe("CodexModelSelect", () => {
  it("offers gpt-6-astra among the Codex suggestions and reports it verbatim", () => {
    const onModelChange = vi.fn();
    const utils = render(
      <I18nProvider>
        <CodexModelSelect model="" onModelChange={onModelChange} />
      </I18nProvider>
    );

    expect(CODEX_MODEL_OPTIONS).toContain("gpt-6-astra");

    const chip = utils.getByTestId("me-codex-model-select-chip-gpt-6-astra");
    expect(chip.textContent).toBe("gpt-6-astra");
    fireEvent.click(chip);
    expect(onModelChange).toHaveBeenCalledWith("gpt-6-astra");
  });

  it("keeps the previously shipped identifiers alongside it", () => {
    expect([...CODEX_MODEL_OPTIONS]).toEqual([
      "gpt-6-astra",
      "gpt-5.6-terra",
      "gpt-5.6-sol",
      "gpt-5.6-luna",
    ]);
  });
});
