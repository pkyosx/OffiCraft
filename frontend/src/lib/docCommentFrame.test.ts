import { describe, expect, it } from "vitest";
import { parseFrameMessage } from "./docCommentFrame";

const msg = (rest: Record<string, unknown>) => ({ oc: "doc-comments", v: 1, ...rest });

describe("parseFrameMessage", () => {
  it("accepts ready, a pick, showing the id where the label is missing or blank, and a selection, trimmed, with an empty one meaning none", () => {
    expect(
      [
        msg({ type: "ready" }),
        msg({ type: "pick", anchor: { id: "ac-AC-5", label: "AC-5：切到已結案 1 秒內可操作" } }),
        msg({ type: "pick", anchor: { id: "ac-AC-6" } }),
        msg({ type: "pick", anchor: { id: "ac-AC-7", label: "  " } }),
        msg({ type: "pick", anchor: { id: "x".repeat(128), label: "y".repeat(200) } }),
        msg({ type: "select", text: "  在 GitHub 開啟 PR #702\n", rect: { left: 10, top: -4.5, width: 80, height: 16 } }),
        msg({ type: "select", text: "z".repeat(4000), rect: { left: 0, top: 0, width: 0, height: 0 } }),
        msg({ type: "select", text: "" }),
        msg({ type: "select", text: " \n ", rect: "ignored" }),
      ].map(parseFrameMessage),
    ).toEqual([
      { type: "ready" },
      { type: "pick", anchor: { id: "ac-AC-5", label: "AC-5：切到已結案 1 秒內可操作" } },
      { type: "pick", anchor: { id: "ac-AC-6", label: "ac-AC-6" } },
      { type: "pick", anchor: { id: "ac-AC-7", label: "ac-AC-7" } },
      { type: "pick", anchor: { id: "x".repeat(128), label: "y".repeat(200) } },
      { type: "select", selection: { text: "在 GitHub 開啟 PR #702", rect: { left: 10, top: -4.5, width: 80, height: 16 } } },
      { type: "select", selection: { text: "z".repeat(4000), rect: { left: 0, top: 0, width: 0, height: 0 } } },
      { type: "select", selection: null },
      { type: "select", selection: null },
    ]);
  });

  it("ignores anything off-protocol, malformed or oversize without throwing", () => {
    const rejected: unknown[] = [
      null,
      undefined,
      "ready",
      42,
      [],
      { type: "ready" },
      { oc: "doc-comments", type: "ready" },
      { oc: "other", v: 1, type: "ready" },
      msg({ v: 2, type: "ready" }),
      msg({ v: "1", type: "ready" }),
      msg({ type: "mode", on: true }),
      msg({ type: "pick" }),
      msg({ type: "pick", anchor: null }),
      msg({ type: "pick", anchor: "ac-1" }),
      msg({ type: "pick", anchor: {} }),
      msg({ type: "pick", anchor: { id: 5, label: "x" } }),
      msg({ type: "pick", anchor: { id: "", label: "x" } }),
      msg({ type: "pick", anchor: { id: "   ", label: "x" } }),
      msg({ type: "pick", anchor: { id: "x".repeat(129), label: "x" } }),
      msg({ type: "pick", anchor: { id: "ac-1", label: 7 } }),
      msg({ type: "pick", anchor: { id: "ac-1", label: "y".repeat(201) } }),
      msg({ type: "select" }),
      msg({ type: "select", text: 42, rect: { left: 0, top: 0, width: 1, height: 1 } }),
      msg({ type: "select", text: "z".repeat(4001), rect: { left: 0, top: 0, width: 1, height: 1 } }),
      msg({ type: "select", text: "PR" }),
      msg({ type: "select", text: "PR", rect: null }),
      msg({ type: "select", text: "PR", rect: { left: "0", top: 0, width: 1, height: 1 } }),
      msg({ type: "select", text: "PR", rect: { left: 0, top: 0, width: 1 } }),
      msg({ type: "select", text: "PR", rect: { left: 0, top: Infinity, width: 1, height: 1 } }),
      msg({ type: "select", text: "PR", rect: { left: 0, top: 0, width: -1, height: 1 } }),
      msg({ type: "select", text: "PR", rect: { left: 0, top: 0, width: 1, height: NaN } }),
    ];
    expect(rejected.map(parseFrameMessage)).toEqual(rejected.map(() => null));
  });
});
