import { describe, it, expect } from "vitest";
import {
  createGroup,
  deleteGroup,
  groupNameProblem,
  layoutTab,
  moveMember,
  renameGroup,
  reorderGroup,
  setGroupTaskTypes,
  appendMembers,
  ungroupedOfType,
} from "./chatGroups";
import type { ChatGroup } from "./chatGroups";

const g = (id: string, tab: ChatGroup["tab"], name: string, memberIds: string[], taskTypeKeys: string[] = []) => ({
  id,
  tab,
  name,
  memberIds,
  taskTypeKeys,
});

const STATE: ChatGroup[] = [
  g("s1", "staff", "前端", ["clara", "beto"]),
  g("s2", "staff", "後端", ["dora"]),
  g("o1", "outsource", "審查", ["ow-1"], ["review"]),
  g("o2", "outsource", "修 bug", [], ["bugfix"]),
];

describe("layoutTab", () => {
  it("lays a tab's rows out by its groups in order, skips ids not in the list, and leaves the rest ungrouped in the given order", () => {
    const rows = ["mira", "beto", "clara", "evan"].map((id) => ({ id }));
    const { sections, ungrouped } = layoutTab(STATE, "staff", rows, (r) => r.id);
    expect(sections.map((s) => [s.group.id, s.items.map((r) => r.id)])).toEqual([
      ["s1", ["clara", "beto"]],
      ["s2", []],
    ]);
    expect(ungrouped.map((r) => r.id)).toEqual(["mira", "evan"]);
  });
});

describe("groupNameProblem", () => {
  it("rejects a blank name, one longer than 40 characters, and one already used in the same tab; the other tab's names and the group's own name are fine", () => {
    expect(groupNameProblem(STATE, "staff", "   ")).toBe("blank");
    expect(groupNameProblem(STATE, "staff", "字".repeat(41))).toBe("long");
    expect(groupNameProblem(STATE, "staff", "字".repeat(40))).toBe(null);
    expect(groupNameProblem(STATE, "staff", " 前端 ")).toBe("taken");
    expect(groupNameProblem(STATE, "outsource", "前端")).toBe(null);
    expect(groupNameProblem(STATE, "staff", "前端", "s1")).toBe(null);
  });
});

describe("createGroup", () => {
  it("adds the group at the end of its tab, trimmed, taking its members out of their old group", () => {
    expect(createGroup(STATE, { id: "s3", tab: "staff", name: " 新 ", memberIds: ["beto"] })).toEqual([
      g("s1", "staff", "前端", ["clara"]),
      g("s2", "staff", "後端", ["dora"]),
      g("s3", "staff", "新", ["beto"]),
      g("o1", "outsource", "審查", ["ow-1"], ["review"]),
      g("o2", "outsource", "修 bug", [], ["bugfix"]),
    ]);
  });
});

describe("renameGroup / deleteGroup", () => {
  it("renames in place, and deleting a group drops it with its members and types", () => {
    expect(renameGroup(STATE, "s2", " API ")[1]).toEqual(g("s2", "staff", "API", ["dora"]));
    expect(deleteGroup(STATE, "o1")).toEqual([STATE[0], STATE[1], STATE[3]]);
  });
});

describe("reorderGroup", () => {
  it("moves a group within its own tab and leaves the other tab alone", () => {
    expect(reorderGroup(STATE, "s2", 0).map((x) => x.id)).toEqual(["s2", "s1", "o1", "o2"]);
    expect(reorderGroup(STATE, "o1", 9).map((x) => x.id)).toEqual(["s1", "s2", "o2", "o1"]);
  });
});

describe("moveMember", () => {
  it("moves a member within its group, into another group at a position or at the end, and out of every group", () => {
    expect(moveMember(STATE, "beto", "s1", 0)[0].memberIds).toEqual(["beto", "clara"]);
    const across = moveMember(STATE, "clara", "s2", 0);
    expect([across[0].memberIds, across[1].memberIds]).toEqual([["beto"], ["clara", "dora"]]);
    expect(moveMember(STATE, "clara", "s2")[1].memberIds).toEqual(["dora", "clara"]);
    const out = moveMember(STATE, "dora", null);
    expect(out[1].memberIds).toEqual([]);
    expect(out.flatMap((x) => x.memberIds)).not.toContain("dora");
  });
});

describe("setGroupTaskTypes", () => {
  it("sets the group's types, takes a ticked type away from the group that had it, and moves no member", () => {
    const next = setGroupTaskTypes(STATE, "o1", ["review", "bugfix"]);
    expect(next[2]).toEqual(g("o1", "outsource", "審查", ["ow-1"], ["review", "bugfix"]));
    expect(next[3]).toEqual(g("o2", "outsource", "修 bug", [], []));
    expect(next.slice(0, 2)).toEqual(STATE.slice(0, 2));
  });
});

describe("ungroupedOfType / appendMembers", () => {
  const workers = [
    { id: "ow-3", taskTypeKey: "bugfix" },
    { id: "ow-1", taskTypeKey: "bugfix" },
    { id: "ow-2", taskTypeKey: "review" },
    { id: "ow-4", taskTypeKey: "bugfix" },
  ];

  it("offers only the ungrouped workers of that type, in the given order", () => {
    expect(ungroupedOfType(STATE, workers, "bugfix")).toEqual(["ow-3", "ow-4"]);
    expect(ungroupedOfType(STATE, workers, "design")).toEqual([]);
  });

  it("appends the brought-in workers to the end of the group", () => {
    expect(appendMembers(STATE, "o1", ["ow-3", "ow-4"])[2].memberIds).toEqual(["ow-1", "ow-3", "ow-4"]);
  });
});
