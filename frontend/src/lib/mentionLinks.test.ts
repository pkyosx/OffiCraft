import { describe, it, expect } from "vitest";
import {
  linkMentions,
  mentionHref,
  mentionTargetFromHref,
  type NamedMentionTarget,
} from "./mentionLinks";

const EVA: NamedMentionTarget = { kind: "member", id: "m-eva", name: "Family Money Eva" };
const FAMILY: NamedMentionTarget = { kind: "member", id: "m-fam", name: "Family" };
const O7: NamedMentionTarget = { kind: "worker", id: "ow-o7", name: "外包 · O-7" };
const ALL = [FAMILY, EVA, O7];

describe("linkMentions", () => {
  it("staff and outsource mentions → markdown links to their detail routes", () => {
    expect(linkMentions("@Family Money Eva 請和 @外包 · O-7 對一下", ALL)).toBe(
      "[@Family Money Eva](/#office/member/m-eva) 請和 [@外包 · O-7](/#office/worker/ow-o7) 對一下",
    );
  });

  it("a name that is a prefix of a longer name → the longest name wins", () => {
    expect(linkMentions("@Family Money Eva hi, @Family too", ALL)).toBe(
      "[@Family Money Eva](/#office/member/m-eva) hi, [@Family](/#office/member/m-fam) too",
    );
  });

  it("an @ glued to a preceding word (email) → left alone", () => {
    expect(linkMentions("mail me at x@Family.com", ALL)).toBe("mail me at x@Family.com");
  });

  it("a name followed by more latin letters → not a mention of the shorter name", () => {
    expect(linkMentions("@Familyish", ALL)).toBe("@Familyish");
  });

  it("CJK text running straight after a name → still a mention", () => {
    const xm: NamedMentionTarget = { kind: "member", id: "m-xm", name: "小明" };
    expect(linkMentions("@小明你好", [xm])).toBe("[@小明](/#office/member/m-xm)你好");
  });

  it("mentions inside inline code, a fenced block or an existing link → untouched", () => {
    const text = [
      "`ping @Family` and @Family",
      "```",
      "@Family",
      "```",
      "[see @Family](https://example.com) @Family",
    ].join("\n");
    expect(linkMentions(text, ALL)).toBe(
      [
        "`ping @Family` and [@Family](/#office/member/m-fam)",
        "```",
        "@Family",
        "```",
        "[see @Family](https://example.com) [@Family](/#office/member/m-fam)",
      ].join("\n"),
    );
  });

  it("a name containing brackets → brackets escaped in the link label", () => {
    const odd: NamedMentionTarget = { kind: "member", id: "m-odd", name: "A[1]" };
    expect(linkMentions("@A[1] ok", [odd])).toBe("[@A\\[1\\]](/#office/member/m-odd) ok");
  });

  it("an unknown @name → text unchanged, while a known one on the same line is linked", () => {
    expect(linkMentions("@Nobody and @Family", ALL)).toBe(
      "@Nobody and [@Family](/#office/member/m-fam)",
    );
  });
});

describe("mentionTargetFromHref", () => {
  it("member and worker routes → their target; any other path → null", () => {
    expect(mentionTargetFromHref("/#office/member/m-eva")).toEqual({ kind: "member", id: "m-eva" });
    expect(mentionTargetFromHref("/#office/worker/ow-o7")).toEqual({ kind: "worker", id: "ow-o7" });
    expect(mentionTargetFromHref("/#office/member/m-eva/from/replies")).toBeNull();
    expect(mentionTargetFromHref("/#tasks/T-1")).toBeNull();
  });

  it("round-trips what mentionHref writes", () => {
    expect(mentionHref({ kind: "worker", id: "ow-o7" })).toBe("/#office/worker/ow-o7");
  });
});
