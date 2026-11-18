import { describe, expect, it } from "vitest";
import { parseFrameMessage } from "./docCommentFrame";

const msg = (rest: Record<string, unknown>) => ({ oc: "doc-comments", v: 1, ...rest });

describe("parseFrameMessage", () => {
  it("accepts ready and a pick, showing the id where the label is missing or blank", () => {
    expect(
      [
        msg({ type: "ready" }),
        msg({ type: "pick", anchor: { id: "ac-AC-5", label: "AC-5：切到已結案 1 秒內可操作" } }),
        msg({ type: "pick", anchor: { id: "ac-AC-6" } }),
        msg({ type: "pick", anchor: { id: "ac-AC-7", label: "  " } }),
        msg({ type: "pick", anchor: { id: "x".repeat(128), label: "y".repeat(200) } }),
      ].map(parseFrameMessage),
    ).toEqual([
      { type: "ready" },
      { type: "pick", anchor: { id: "ac-AC-5", label: "AC-5：切到已結案 1 秒內可操作" } },
      { type: "pick", anchor: { id: "ac-AC-6", label: "ac-AC-6" } },
      { type: "pick", anchor: { id: "ac-AC-7", label: "ac-AC-7" } },
      { type: "pick", anchor: { id: "x".repeat(128), label: "y".repeat(200) } },
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
    ];
    expect(rejected.map(parseFrameMessage)).toEqual(rejected.map(() => null));
  });
});
