import { describe, it, expect } from "vitest";
import type { Member } from "../types";
import type { OutsourceWorkerView } from "../api/adapter";
import {
  activeMentionQuery,
  filterMentionCandidates,
  insertMention,
  mentionCandidates,
  type MentionCandidate,
} from "./chatMention";

const label = (codename: string) => `外包 · ${codename}`;

function staff(id: string, name: string, roleName: string, kind = "staff"): Member {
  return { id, name, roleName, kind } as Member;
}
function worker(over: Partial<OutsourceWorkerView>): OutsourceWorkerView {
  return { id: "ow-1", codename: "O-7", model: "", effort: "", taskId: "", ...over };
}

describe("mentionCandidates", () => {
  it("staff (not wardens) then outsource; task shown only for a live worker with a task number", () => {
    const got = mentionCandidates(
      [staff("m-1", "Mira", "助理"), staff("w-1", "Warden", "", "warden")],
      [
        worker({ id: "ow-a", codename: "O-7", status: "active", taskNo: "T-492", taskTitle: "修聊天" }),
        worker({ id: "ow-b", codename: "S-3", status: "released", taskNo: "T-100", taskTitle: "舊票" }),
        worker({ id: "ow-c", codename: "H-1", status: "assigned", taskNo: "", taskTitle: "" }),
      ],
      label,
    );
    expect(got).toEqual([
      { kind: "member", id: "m-1", name: "Mira", roleName: "助理" },
      { kind: "worker", id: "ow-a", name: "外包 · O-7", taskNo: "T-492", taskTitle: "修聊天" },
      { kind: "worker", id: "ow-b", name: "外包 · S-3" },
      { kind: "worker", id: "ow-c", name: "外包 · H-1" },
    ]);
  });
});

describe("activeMentionQuery", () => {
  it("@ at start or after whitespace, caret after it → the typed query", () => {
    expect(activeMentionQuery("@mi", 3)).toEqual({ start: 0, query: "mi" });
    expect(activeMentionQuery("hi @Family Mo", 13)).toEqual({ start: 3, query: "Family Mo" });
  });

  it("@ glued to a word, a newline after @, or no @ → no query", () => {
    expect(activeMentionQuery("a@b", 3)).toBeNull();
    expect(activeMentionQuery("@a\nb", 4)).toBeNull();
    expect(activeMentionQuery("@ x", 3)).toBeNull();
    expect(activeMentionQuery("hello", 5)).toBeNull();
  });
});

describe("filterMentionCandidates", () => {
  const pool: MentionCandidate[] = [
    { kind: "member", id: "m-1", name: "Kye", roleName: "Developer" },
    { kind: "worker", id: "ow-1", name: "外包 · O-7", taskNo: "T-492", taskTitle: "Fix chat mention list" },
    { kind: "member", id: "m-2", name: "Mira", roleName: "Assistant" },
    { kind: "worker", id: "ow-2", name: "外包 · S-3", taskNo: "T-1049", taskTitle: "Ship release" },
    { kind: "member", id: "m-3", name: "Amira", roleName: "Reviewer" },
  ];
  const ids = (q: string) => filterMentionCandidates(pool, q).map((c) => c.id);

  it("empty query → the whole pool in order", () => {
    expect(ids("")).toEqual(["m-1", "ow-1", "m-2", "ow-2", "m-3"]);
  });

  it("name substring, any case → matching rows", () => {
    expect(ids("KY")).toEqual(["m-1"]);
  });

  it("task number with prefix (T-492) → that worker", () => {
    expect(ids("t-492")).toEqual(["ow-1"]);
  });

  it("bare digits (492) → the worker whose task number part starts with them", () => {
    expect(ids("492")).toEqual(["ow-1"]);
    expect(ids("104")).toEqual(["ow-2"]);
  });

  it("title substring → that worker", () => {
    expect(ids("mention")).toEqual(["ow-1"]);
  });

  it("role name substring → that staff member", () => {
    expect(ids("review")).toEqual(["m-3"]);
  });

  it("names starting with the query rank before names merely containing it", () => {
    expect(ids("mira")).toEqual(["m-2", "m-3"]);
    expect(ids("a")).toEqual(["m-3", "ow-1", "m-2", "ow-2"]);
  });

  it("nothing matches → empty, while a matching query on the same pool is not", () => {
    expect(ids("zzz")).toEqual([]);
    expect(ids("ship")).toEqual(["ow-2"]);
  });
});

describe("insertMention", () => {
  it("replaces @query with @name and a space, caret after the space", () => {
    expect(insertMention("hi @mi there", { start: 3, query: "mi" }, "Mira")).toEqual({
      text: "hi @Mira  there",
      caret: 9,
    });
  });
});
