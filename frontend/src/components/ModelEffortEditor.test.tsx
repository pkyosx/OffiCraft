// ModelEffortEditor — the shared model/effort picker's suggestion vocabulary.
//
// CODEX_MODEL_OPTIONS is the ONE definition of the Codex quick-pick chips
// (TaskManualsPage and TaskReassignDialog import it, and CodexModelSelect
// renders it). The word a chip reports is what ships to the server as the
// launch model, and the warden resolves only these four family words, so they
// are pinned literally.

import { describe, it, expect, afterEach, vi } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { CODEX_MODEL_OPTIONS, CodexModelSelect, ModelEffortEditor } from "./ModelEffortEditor";
import {
  EFFORT_LABELS_EN_WITH_SLUG,
  EFFORT_SLUGS,
  clearLocale,
  optionTexts,
  optionValues,
  useEnglishLocale,
} from "../test/effortOptions";

describe("CodexModelSelect", () => {
  it("offers the four Codex model families as chips and reports the chosen family word", () => {
    const onModelChange = vi.fn();
    const utils = render(
      <I18nProvider>
        <CodexModelSelect model="" onModelChange={onModelChange} />
      </I18nProvider>
    );

    expect([...CODEX_MODEL_OPTIONS]).toEqual(["astra", "sol", "terra", "luna"]);
    for (const family of ["astra", "sol", "terra", "luna"]) {
      expect(utils.getByTestId(`me-codex-model-select-chip-${family}`).textContent).toBe(family);
    }

    fireEvent.click(utils.getByTestId("me-codex-model-select-chip-sol"));
    expect(onModelChange).toHaveBeenCalledWith("sol");
  });
});

describe("ModelEffortEditor", () => {
  afterEach(clearLocale);

  it("offers exactly the five English 投入程度 levels in the dropdown", () => {
    useEnglishLocale();
    const utils = render(
      <I18nProvider>
        <ModelEffortEditor
          model=""
          effort="medium"
          onModelChange={vi.fn()}
          onEffortChange={vi.fn()}
        />
      </I18nProvider>
    );

    const select = utils.getByTestId("me-effort-select");
    expect(select.tagName).toBe("SELECT");
    // EXACTLY, not "contains": a containment check cannot see a sixth option
    // appear or "X-High" quietly revert to "Extra High".
    expect(optionTexts(select)).toEqual([...EFFORT_LABELS_EN_WITH_SLUG]);
    expect(optionValues(select)).toEqual([...EFFORT_SLUGS]);
  });
});
