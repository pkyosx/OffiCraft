import { describe, it, expect } from "vitest";
import { chatBodyMatches, chatSearchSnippet } from "./chatSearch";

describe("chatBodyMatches", () => {
  it("matches ASCII letters in any case and ignores surrounding spaces in the query", () => {
    expect(chatBodyMatches("Deploy done", "  DEPLOY ")).toBe(true);
    expect(chatBodyMatches("Deploy done", "rollback")).toBe(false);
  });

  it("does not fold non-ASCII letters, like the server", () => {
    expect(chatBodyMatches("ÄBC", "äbc")).toBe(false);
    expect(chatBodyMatches("äBC", "äbc")).toBe(true);
  });
});

describe("chatSearchSnippet", () => {
  it("in a short body, marks every occurrence regardless of ASCII case and cuts nothing", () => {
    expect(chatSearchSnippet("Deploy 完成，再 deploy 一次", "DEPLOY")).toEqual({
      cutStart: false,
      cutEnd: false,
      parts: [
        { text: "Deploy", match: true },
        { text: " 完成，再 ", match: false },
        { text: "deploy", match: true },
        { text: " 一次", match: false },
      ],
    });
  });

  it("in a long body, keeps 40 characters each side of the first match and reports both cuts", () => {
    const body = "x".repeat(60) + "KEY" + "y".repeat(60);
    expect(chatSearchSnippet(body, "key")).toEqual({
      cutStart: true,
      cutEnd: true,
      parts: [
        { text: "x".repeat(40), match: false },
        { text: "KEY", match: true },
        { text: "y".repeat(40), match: false },
      ],
    });
  });

  it("with an occurrence straddling the cut, extends the cut to the end of that occurrence", () => {
    expect(chatSearchSnippet("aaKEYbbKEYcc", "key", 3)).toEqual({
      cutStart: false,
      cutEnd: true,
      parts: [
        { text: "aa", match: false },
        { text: "KEY", match: true },
        { text: "bb", match: false },
        { text: "KEY", match: true },
      ],
    });
  });

  it("with no match in the body, cuts from the start and marks nothing", () => {
    expect(chatSearchSnippet("abcdef", "zz", 2)).toEqual({
      cutStart: false,
      cutEnd: true,
      parts: [{ text: "abcd", match: false }],
    });
  });
});
