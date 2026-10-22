import { describe, it, expect } from "vitest";
import { buildActivityTree, type ActivityTreeNode } from "./activityTree";
import type { ActivitySubagent } from "../types";

const sub = (id: string, parentId: string | null, startedTs: number): ActivitySubagent => ({
  id,
  parentId,
  name: "Explore",
  description: id,
  startedTs,
});

type Shape = [string, Shape[]];
const shape = (nodes: ActivityTreeNode[]): Shape[] =>
  nodes.map((n) => [n.subagent.id, shape(n.children)]);

describe("buildActivityTree", () => {
  it("nests each sub-agent under its parent, earliest started first at every level", () => {
    const tree = buildActivityTree([
      sub("b", null, 20),
      sub("a2", "a", 15),
      sub("a", null, 10),
      sub("a1", "a", 12),
      sub("a1x", "a1", 13),
    ]);
    expect(shape(tree)).toEqual([
      ["a", [["a1", [["a1x", []]]], ["a2", []]]],
      ["b", []],
    ]);
  });

  it("hangs a sub-agent whose parent is not in the list directly under the main agent", () => {
    const tree = buildActivityTree([sub("a", null, 10), sub("orphan", "gone", 5)]);
    expect(shape(tree)).toEqual([
      ["orphan", []],
      ["a", []],
    ]);
  });

  it("hangs a self-parented or cyclic sub-agent under the main agent instead of dropping it", () => {
    const tree = buildActivityTree([sub("self", "self", 1), sub("x", "y", 2), sub("y", "x", 3)]);
    expect(shape(tree)).toEqual([
      ["self", []],
      ["x", []],
      ["y", []],
    ]);
  });

  it("returns no nodes when no sub-agent runs", () => {
    expect(buildActivityTree([])).toEqual([]);
  });
});
