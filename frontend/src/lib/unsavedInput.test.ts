import { afterEach, describe, expect, it } from "vitest";
import { trackUnsavedTextFields } from "./unsavedInput";

let stop: (() => void) | null = null;
afterEach(() => {
  stop?.();
  stop = null;
  document.body.innerHTML = "";
});

function track() {
  const t = trackUnsavedTextFields(document);
  stop = t.stop;
  return t;
}

describe("trackUnsavedTextFields", () => {
  it("reports a textarea typed into after it was focused, and clears once it holds its focus-time text again", () => {
    const t = track();
    const box = document.createElement("textarea");
    box.value = "saved";
    document.body.append(box);
    box.focus();
    expect(t.hasEditedField()).toBe(false);

    box.value = "saved and more";
    expect(t.hasEditedField()).toBe(true);

    box.value = "saved";
    expect(t.hasEditedField()).toBe(false);
  });

  it("reports an edited text input", () => {
    const t = track();
    const input = document.createElement("input");
    document.body.append(input);
    input.focus();
    input.value = "draft";
    expect(t.hasEditedField()).toBe(true);
  });

  it("ignores non-text inputs and fields that left the page", () => {
    const t = track();
    const slider = document.createElement("input");
    slider.type = "range";
    const area = document.createElement("textarea");
    document.body.append(slider, area);
    slider.focus();
    slider.value = "80";
    area.focus();
    area.value = "typed";
    area.remove();

    expect(t.hasEditedField()).toBe(false);
  });

  it("stops recording focus once stopped", () => {
    const t = track();
    t.stop();
    const area = document.createElement("textarea");
    document.body.append(area);
    area.focus();
    area.value = "typed";
    expect(t.hasEditedField()).toBe(false);
  });
});
