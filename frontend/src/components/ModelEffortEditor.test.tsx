// ModelEffortEditor — the shared model/effort picker's suggestion vocabulary.
//
// CODEX_MODEL_OPTIONS is the ONE definition of the Codex quick-pick chips
// (TaskManualsPage and TaskReassignDialog import it, and CodexModelSelect
// renders it). The identifier a chip reports is what ships to the server as the
// launch model, so it is pinned literally — a chip labelled right but reporting
// a stale slug would launch the wrong model silently.
//
// `gpt-6-astra` is a MEASUREMENT, not a guarantee: on 2026-09-07, on eva-m5,
// codex-cli 0.153.4's `codex app-server` answered `model/list` with
// {"id":"gpt-6-astra","displayName":"GPT-6-Astra"}, and that release's binary
// contains `"slug": "gpt-6-astra"`. Nothing in this repo re-checks that against
// a live Codex, so the day the app-server renames the slug this literal goes
// quietly wrong and no test here turns red — re-measure before trusting it.

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
