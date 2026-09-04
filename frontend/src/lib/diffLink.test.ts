import { describe, it, expect } from "vitest";
import {
  diffParamsFromHref,
  diffRouteFromLocation,
  formatDiffSideAddress,
  formatDiffUrl,
  parseDiffParams,
  parseDiffSideAddress,
} from "./diffLink";

describe("parseDiffSideAddress", () => {
  it("reads a stored blob id", () => {
    expect(parseDiffSideAddress("att-0123456789ab")).toEqual({
      attachmentId: "att-0123456789ab",
    });
  });

  it("reads a document address at each of the three points in time", () => {
    expect(parseDiffSideAddress("doc:role_definition/pm/current/definition_md")).toEqual({
      doc: { kind: "role_definition", key: "pm", at: "current", field: "definition_md" },
    });
    expect(parseDiffSideAddress("doc:global_context/global/seed/text")?.doc?.at).toBe("seed");
    expect(parseDiffSideAddress("doc:global_context/global/1899/text")?.doc?.at).toBe("1899");
  });

  it("keeps a composite document key whole", () => {
    expect(parseDiffSideAddress("doc:lessons/pm::ops/current/text")?.doc?.key).toBe(
      "pm::ops",
    );
  });

  it("refuses anything that is not one of the two shapes", () => {
    // A blob id is 12 LOWERCASE hex — the shape the server mints.
    expect(parseDiffSideAddress("att-0123456789AB")).toBeNull();
    expect(parseDiffSideAddress("att-0123456789")).toBeNull();
    expect(parseDiffSideAddress("/api/chat/attachment/att-0123456789ab")).toBeNull();
    // A point in time that is neither reserved word nor a decimal id would
    // otherwise reach the server as a question about nothing.
    expect(parseDiffSideAddress("doc:global_context/global/latest/text")).toBeNull();
    // Every segment is required: a missing field is not "the whole document".
    expect(parseDiffSideAddress("doc:global_context/global/current")).toBeNull();
    expect(parseDiffSideAddress("doc:global_context//current/text")).toBeNull();
    expect(parseDiffSideAddress("")).toBeNull();
  });
});

describe("formatDiffSideAddress", () => {
  it("round-trips both shapes", () => {
    for (const raw of ["att-0123456789ab", "doc:insight/pm/seed/text"]) {
      expect(formatDiffSideAddress(parseDiffSideAddress(raw)!)).toBe(raw);
    }
  });
});

describe("parseDiffParams", () => {
  it("reads both sides, the optional labels and the signature", () => {
    expect(
      parseDiffParams(
        "?before=att-0123456789ab&after=doc:global_context/global/current/text" +
          "&label_before=%E6%94%B9%E5%8B%95%E5%89%8D&label_after=now&sig=abc123",
      ),
    ).toEqual({
      before: "att-0123456789ab",
      after: "doc:global_context/global/current/text",
      labelBefore: "改動前",
      labelAfter: "now",
      sig: "abc123",
    });
  });

  it("refuses a comparison that is missing a side or names a bad one", () => {
    expect(parseDiffParams("?before=att-0123456789ab")).toBeNull();
    expect(parseDiffParams("?before=att-0123456789ab&after=nonsense")).toBeNull();
    expect(parseDiffParams("")).toBeNull();
  });

  it("drops an empty label rather than heading a column with nothing", () => {
    const params = parseDiffParams(
      "?before=att-0123456789ab&after=att-fedcba987654&label_before=",
    );
    expect(params?.labelBefore).toBeUndefined();
  });
});

describe("formatDiffUrl", () => {
  it("round-trips through parseDiffParams with the values encoded", () => {
    const params = {
      before: "att-0123456789ab",
      after: "doc:lessons/pm::ops/current/text",
      labelBefore: "改動前 & 之後",
      sig: "s+g/1",
    };
    const url = formatDiffUrl(params);
    expect(url.startsWith("/diff?")).toBe(true);
    expect(parseDiffParams(url.slice(url.indexOf("?")))).toEqual(params);
  });
});

describe("diffParamsFromHref", () => {
  const base = "https://studio.example/#office";

  it("recognises our own compare url", () => {
    expect(
      diffParamsFromHref(
        "https://studio.example/diff?before=att-0123456789ab&after=att-fedcba987654",
        base,
      ),
    ).toEqual({ before: "att-0123456789ab", after: "att-fedcba987654" });
  });

  it("refuses another origin's /diff, another path, and a malformed comparison", () => {
    const query = "?before=att-0123456789ab&after=att-fedcba987654";
    expect(diffParamsFromHref(`https://evil.example/diff${query}`, base)).toBeNull();
    expect(diffParamsFromHref(`https://studio.example/diffs${query}`, base)).toBeNull();
    expect(diffParamsFromHref("https://studio.example/diff?before=att-0123456789ab", base))
      .toBeNull();
    expect(diffParamsFromHref("javascript:alert(1)", base)).toBeNull();
  });
});

describe("diffRouteFromLocation", () => {
  it("answers only on the compare path", () => {
    const search = "?before=att-0123456789ab&after=att-fedcba987654";
    expect(diffRouteFromLocation({ pathname: "/diff", search })?.before).toBe(
      "att-0123456789ab",
    );
    expect(diffRouteFromLocation({ pathname: "/", search })).toBeNull();
  });
});
