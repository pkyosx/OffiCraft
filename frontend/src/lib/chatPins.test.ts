import { describe, it, expect } from "vitest";
import { orderByPins, pinAt, togglePin } from "./chatPins";

const ids = (xs: { id: string }[]) => xs.map((x) => x.id);
const items = ["a", "b", "c", "d"].map((id) => ({ id }));

describe("orderByPins", () => {
  it("puts this list's pinned items first in pin order and keeps the rest in their given order", () => {
    const { ordered, pinnedIds } = orderByPins(items, ["c", "x-other-tab", "a"], (i) => i.id);
    expect(ids(ordered)).toEqual(["c", "a", "b", "d"]);
    expect(pinnedIds).toEqual(["c", "a"]);
  });
});

describe("togglePin", () => {
  it("pins at the end of the tab's pinned run, ahead of later other-tab ids", () => {
    expect(togglePin(["a", "ow-1", "c", "ow-2"], "b", ["a", "c"])).toEqual([
      "a",
      "ow-1",
      "c",
      "b",
      "ow-2",
    ]);
  });

  it("pins at the end when the tab has no pins yet", () => {
    expect(togglePin(["mira"], "ow-1", [])).toEqual(["mira", "ow-1"]);
  });

  it("unpins an already pinned id and keeps the others' order", () => {
    expect(togglePin(["a", "b", "c"], "b", ["a", "b", "c"])).toEqual(["a", "c"]);
  });
});

describe("pinAt", () => {
  it("reorders a pinned id within its tab's pins, leaving other-tab ids in place", () => {
    const pins = ["a", "ow-1", "c", "ow-2"];
    expect(pinAt(pins, "c", 0, ["a", "c"])).toEqual(["c", "a", "ow-1", "ow-2"]);
    expect(pinAt(pins, "ow-1", 1, ["ow-1", "ow-2"])).toEqual(["a", "c", "ow-2", "ow-1"]);
  });

  it("clamps an index past the end to the last pinned slot", () => {
    expect(pinAt(["a", "b", "c"], "a", 9, ["a", "b", "c"])).toEqual(["b", "c", "a"]);
  });
});
