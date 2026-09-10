// The frontend's closed SSE topic set, bound to the artifact that declares it.
//
// WHY this exists (T-05db node 4): `SSE_RESYNC_TOPICS` in api/http.ts is the
// list http.ts replays after a reconnect — one synthetic delta per topic, which
// is what makes every hook refetch the snapshot it may have missed while the
// stream was down (spec/sse.md §2.1: there is NO replay). A topic MISSING from
// that array fails in the worst possible shape: after a reconnect that topic
// silently never refetches, so the data is right, the screen is stale, and
// there is no error, no console warning and no red test. The user sees it; the
// suite does not. Until this file existed nothing in the frontend checked the
// array against anything — it was a hand transcription of a table in a
// markdown file, and there were in fact TWO such transcriptions (the second
// lived in hooks/sseFanout.test.tsx and, measured, had zero discriminating
// power: deleting a topic from it left the whole suite green).
//
// ⚠️ WHAT IT IS BOUND TO CHANGED. This guard used to PARSE spec/sse.md §3.1's
// markdown table, and so did a Go test and the Python conformance suite: three
// parsers over three hand-kept copies of one list. The list has ONE source now
// — `sseTopics` in server/ocserverd/hub.go, the map the publish seam consults
// before it fans anything — rendered by bin/gen-sse-topics into the committed
// spec/sse-topics.json and held to the code by the drift-sse-topics gate. That
// generated artifact is what this file reads. §3.1's table stays HAND-WRITTEN
// documentation and is deliberately NOT read here: a sentence in a spec cannot
// make the server fan a frame, and reading it back was how a stale table got to
// look like an authority.
//
// The artifact is read HERE AT RUN TIME, from the repo checkout. Reading a repo
// file from vitest is established practice in this tree (lib/themePaint,
// lib/paintArtifact, components/styleOwnership all readFileSync the sources
// they guard) — vitest runs in Node, `node:fs` is real.
//
// EQUALITY, not subset, and every offender is NAMED: a topic the server can
// send that the frontend never resyncs is the silent-staleness bug above; a
// topic in the frontend the server does not have is a phantom it can never send
// (hub.go drops it at the publish seam), i.e. a resync fan-out that costs every
// hook a refetch for nothing.
//
// 🔴 Do NOT "fix" a failure here by transcribing the topic list into this file.
// The whole point is that this file contains no topic names at all.

import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

// __dirname (= frontend/src/api) rather than import.meta.url: under the jsdom
// environment import.meta.url is not a file: URL, so fileURLToPath throws.
// Same resolution style as lib/themePaint.test.ts, which reads a server asset.
const TOPICS_PATH = resolve(__dirname, "../../..", "spec/sse-topics.json");

/** Extract the closed topic set from the generated artifact's text.
 *
 * FAIL-CLOSED by construction: every way this can stop finding topics THROWS
 * rather than returning a short or empty list. A parser that silently returned
 * [] would turn this guard green forever the day the artifact's shape changes —
 * the exact failure mode the guard is supposed to make impossible. Each throw
 * below is exercised by a test at the bottom of this file. */
export function parseTopicAsset(raw: string): string[] {
  let doc: unknown;
  try {
    doc = JSON.parse(raw);
  } catch (e) {
    throw new Error(
      `spec/sse-topics.json: not valid JSON (${String(e)}). It is a GENERATED ` +
        "artifact — regenerate it with bin/gen-sse-topics rather than repairing it by hand.",
    );
  }
  if (typeof doc !== "object" || doc === null || Array.isArray(doc)) {
    throw new Error(
      "spec/sse-topics.json: top level is not an object — the artifact's shape " +
        "changed and this reader must not guess at the new one.",
    );
  }
  const topics = (doc as { topics?: unknown }).topics;
  if (!Array.isArray(topics) || !topics.every((t) => typeof t === "string" && t.length > 0)) {
    throw new Error(
      "spec/sse-topics.json: `topics` must be a non-empty-string array. The " +
        "closed set is READ from this file at run time, so a shape change must " +
        "fail here instead of yielding an empty set that makes this guard vacuous.",
    );
  }
  if (topics.length === 0) {
    throw new Error(
      "spec/sse-topics.json: `topics` is EMPTY — an empty closed set would make " +
        "the comparison below agree with anything. Regenerate with bin/gen-sse-topics.",
    );
  }
  return topics as string[];
}

/** Read + parse the real artifact. Fails loudly (never empty) if the file is
 * gone or unreadable — a missing artifact must not read as "no topics, all
 * agreed". */
export function readTopicAsset(path: string = TOPICS_PATH): string[] {
  let raw: string;
  try {
    raw = readFileSync(path, "utf8");
  } catch (e) {
    throw new Error(
      `spec/sse-topics.json unreadable at ${path}: ${String(e)} — this guard ` +
        "reads the generated topic asset at run time and cannot pass without it. " +
        "Create it with bin/gen-sse-topics.",
    );
  }
  return parseTopicAsset(raw);
}

describe("SSE_RESYNC_TOPICS vs spec/sse-topics.json", () => {
  it("equals the closed topic set the server declares", async () => {
    const declared = new Set(readTopicAsset());
    // Imported lazily so a parse/read failure above reports as ITSELF rather
    // than as a confusing module-load error from the adapter.
    const { SSE_RESYNC_TOPICS } = await import("./http");
    const code = new Set<string>(SSE_RESYNC_TOPICS);

    const missing = [...declared].filter((t) => !code.has(t)).sort();
    const extra = [...code].filter((t) => !declared.has(t)).sort();

    expect(
      { missing, extra },
      "api/http.ts SSE_RESYNC_TOPICS MUST equal the closed topic set in " +
        "spec/sse-topics.json (generated from hub.go's sseTopics).\n" +
        "  `missing` = the server can fan it but the client NEVER resyncs it: " +
        "after a reconnect that topic silently stops refetching — right data, " +
        "stale screen, zero errors. Add it to SSE_RESYNC_TOPICS.\n" +
        "  `extra` = resynced by the client but NOT in the server's set: a " +
        "phantom topic the server can never emit (hub.go drops it at the " +
        "publish seam). Add it to hub.go's sseTopics first and re-run " +
        "bin/gen-sse-topics, or drop it here.\n" +
        "  🔴 Do NOT silence this by copying the topic list into the test.",
    ).toEqual({ missing: [], extra: [] });
  });

  // The array's own SHAPE — the one thing no comparison against the artifact
  // can check. The confrontation above compares SETS, and a Set de-duplicates
  // by definition; the two deep-equality guards (api/http.sse-pool.test.ts,
  // hooks/sseFanout.test.tsx) now build their expectation FROM this array, so a
  // duplicate lands on both sides of the equality and cancels. Measured by
  // review round 2: duplicating "chat" in SSE_RESYNC_TOPICS left all 218 files
  // / 1820 tests green, while the hand-copy those guards replaced had caught it
  // (5 failed). That is the one axis the fold lost, and this is where it comes
  // back — a duplicate costs every subscriber of that topic an extra refetch on
  // every reconnect, silently and with no red anywhere.
  it("declares each topic exactly once (a duplicate is invisible to set/self comparisons)", async () => {
    const { SSE_RESYNC_TOPICS } = await import("./http");
    const seen = new Map<string, number>();
    for (const t of SSE_RESYNC_TOPICS) seen.set(t, (seen.get(t) ?? 0) + 1);
    const duplicated = [...seen.entries()].filter(([, n]) => n > 1).map(([t, n]) => `${t}×${n}`);
    expect(
      duplicated,
      "SSE_RESYNC_TOPICS must list each topic exactly once: resyncAll fans one " +
        "synthetic delta per ELEMENT, so a repeated element makes every hook " +
        "subscribed to it refetch again on every reconnect. Nothing else in the " +
        "suite can see this — the comparison above is set-based, and the " +
        "fan-out guards compare the fan against this same array.",
    ).toEqual([]);
    expect(new Set(SSE_RESYNC_TOPICS).size).toBe(SSE_RESYNC_TOPICS.length);
  });

  // ——— fail-closed: every way the reader can stop seeing the topic list must
  // be LOUD. Constructed inputs, so these hold regardless of what the real
  // artifact currently contains.
  it("throws when the artifact is not JSON (not: passes with zero topics)", () => {
    expect(() => parseTopicAsset("{not json")).toThrow(/not valid JSON/);
  });

  it("throws when the top level is not an object", () => {
    expect(() => parseTopicAsset("[]")).toThrow(/top level is not an object/);
  });

  it("throws when `topics` is missing, mistyped, or empty", () => {
    for (const broken of [
      '{"generated_by":"bin/gen-sse-topics"}',
      '{"topics":{}}',
      '{"topics":["member",7]}',
      '{"topics":["member",""]}',
      '{"topics":[]}',
    ]) {
      expect(() => parseTopicAsset(broken), `must refuse ${broken}`).toThrow(/`topics`/);
    }
  });

  it("throws when the artifact cannot be read", () => {
    expect(() => readTopicAsset(`${TOPICS_PATH}.does-not-exist`)).toThrow(/unreadable/);
  });
});
