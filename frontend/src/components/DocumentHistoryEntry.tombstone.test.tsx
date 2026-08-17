// A TOMBSTONED revision reads, and above all DIFFS, as the shipped default
// (T-40f0 node 11 — owner caught this on screen, 2026-08-05).
//
// `tombstoned="true"` means "this document was following its shipped default";
// the row's text column is empty in the database because the text lives in the
// seed file. The cockpit read that empty column as literal content, so the diff
// pane against such a revision announced that EVERY line of the live document
// would be deleted — 「還原＝清空」 printed next to a destructive button, while
// `restoreDocumentHistory` actually writes the tombstone back and the fold puts
// the document ON the default.
//
// 🔴 WHY THESE FIXTURES AND NOT THE OBVIOUS ONES. Every pre-existing test of
// this behaviour used a `global_context` seed pseudo-version, and under THAT
// kind 「empty == the default」 is true by construction (`documentSeedContent`
// answers `{"text":"", "tombstoned":"true"}` for it). So the whole suite was
// pinning the bug's own premise as an invariant. The cases below therefore use
// a RETAINED revision (id 42, an author, a timestamp — not the seed row) of a
// FILE-SEEDED kind (`role_definition`, whose default is a real document), which
// is the only shape where the two can disagree. The global block keeps a case
// of its own further down, because its default really IS empty and that path
// must not regress.
//
// The criterion every assertion here serves: WHAT THE DIFF SAYS MUST EQUAL THE
// STATE A RESTORE LEAVES BEHIND. So the assertions are on the rows the diff
// actually draws — marker counts — not on whether some constant is referenced.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { DocumentHistoryEntry } from "./DocumentHistoryEntry";
import { __resetMock, mockApi } from "../api/mock";
import type { DocumentHistoryView, DocumentKind } from "../types";
import { stubDocumentHistory } from "../test/documentHistory";

const s = zh.settings;

const REVISION_TS = 1753776180;

/** One RETAINED revision — an id, an author and a timestamp. Deliberately not
 * the 初始版本 pseudo-row: that row already carried the seed, so it cannot tell
 * a fixed reader from a broken one. */
function retained(content: Record<string, string>): DocumentHistoryView {
  return { id: 42, content, createdTs: REVISION_TS, actorId: "owner-1" };
}

async function openReader(opts: {
  kind: DocumentKind;
  docKey: string;
  revision: DocumentHistoryView;
  currentContent: Record<string, string>;
  /** A document that ships a default — the reset's presence is what grows the
   * 初始版本 row AND what makes the host fetch the seed at all. */
  hasSeed?: boolean;
  /** Make the seed GET REJECT — the real degraded state (a flaky server, or a
   * kind whose /seed route 404s). */
  seedFails?: boolean;
}) {
  stubDocumentHistory(mockApi, [opts.revision]);
  if (opts.seedFails) {
    vi.spyOn(mockApi, "getDocumentSeed").mockRejectedValue(
      new Error("seed GET failed")
    );
  }
  const utils = render(
    <I18nProvider>
      <DocumentHistoryEntry
        kind={opts.kind}
        docKey={opts.docKey}
        title="版本紀錄"
        currentContent={opts.currentContent}
        onReset={opts.hasSeed === false ? undefined : async () => {}}
      />
    </I18nProvider>
  );
  fireEvent.click(utils.getByTestId(`doc-history-entry-${opts.kind}`));
  await utils.findByTestId("doc-history-list");
  await utils.findByTestId(`doc-history-item-${opts.revision.id}`);
  return utils;
}

/** The markers of the rows the diff actually drew: "-", "+" or a NBSP for an
 * unchanged line. Counting these is the only way to state "restoring this would
 * not wipe the document" in the terms the reader sees. */
function markers(container: HTMLElement): string[] {
  return Array.from(container.querySelectorAll(".diff-view__row")).map(
    (row) => (row.querySelectorAll("td")[2]?.textContent ?? "")
  );
}

beforeEach(() => {
  __resetMock();
  vi.restoreAllMocks();
});

describe("DocumentHistoryEntry · a tombstoned revision is the shipped default", () => {
  it("diffs a RETAINED tombstone against the live doc as the SEED, not as a wipe", async () => {
    // The document currently differs from its default by exactly one line. The
    // truthful diff therefore has one `-` and one `+`; the defect drew one `+`
    // per line of the whole document (and a lone `-` for the empty string),
    // i.e. "restoring this deletes everything you have".
    const seed = (await mockApi.getDocumentSeed("role_definition", "assistant"))
      .content.definition_md;
    const seedLines = seed.split("\n");
    expect(seedLines.length).toBeGreaterThan(2); // the fixture must be able to lie

    const utils = await openReader({
      kind: "role_definition",
      docKey: "assistant",
      // What the server stores for a reset: the field is EMPTY and the tombstone
      // is the whole message (api_roles.go → `RoleDef{RoleKey: role,
      // Tombstoned: true}`).
      revision: retained({ definition_md: "", tombstoned: "true" }),
      currentContent: {
        definition_md: ["owner 改寫的第一行", ...seedLines.slice(1)].join("\n"),
      },
    });

    fireEvent.click(utils.getByTestId("doc-history-open-42"));
    fireEvent.click(await utils.findByTestId("doc-history-pane-diff"));
    const drawn = await waitFor(() => {
      const found = markers(utils.container);
      expect(found.length).toBeGreaterThan(0);
      return found;
    });

    expect(drawn.filter((m) => m === "+")).toHaveLength(1);
    expect(drawn.filter((m) => m === "-")).toHaveLength(1);
    // …and the rest of the document is shown as UNTOUCHED, which is the half
    // that says "restoring this is safe".
    expect(drawn.filter((m) => m !== "+" && m !== "-").length).toBeGreaterThan(0);
  });

  it("says there is nothing to change when the doc already sits on its default", async () => {
    // The state right after a reset: restoring the tombstone is a no-op, and
    // the reader must say so rather than paint the document red.
    const seed = (await mockApi.getDocumentSeed("role_definition", "assistant"))
      .content.definition_md;
    const utils = await openReader({
      kind: "role_definition",
      docKey: "assistant",
      revision: retained({ definition_md: "", tombstoned: "true" }),
      currentContent: { definition_md: seed },
    });

    fireEvent.click(utils.getByTestId("doc-history-open-42"));
    fireEvent.click(await utils.findByTestId("doc-history-pane-diff"));

    expect((await utils.findByTestId("diff-view-empty")).textContent).toBe(
      zh.diff.noChanges
    );
    expect(markers(utils.container)).toHaveLength(0);
  });

  it("renders the default's own text in the content pane", async () => {
    const seed = (await mockApi.getDocumentSeed("role_definition", "assistant"))
      .content.definition_md;
    const utils = await openReader({
      kind: "role_definition",
      docKey: "assistant",
      revision: retained({ definition_md: "", tombstoned: "true" }),
      currentContent: { definition_md: "owner 的版本" },
    });

    fireEvent.click(utils.getByTestId("doc-history-open-42"));
    const body = (await utils.findByTestId("doc-history-modal")).querySelector(
      ".doc-hist-modal__body"
    ) as HTMLElement;
    // A distinctive phrase from the shipped default, not 「這個版本沒有內容」.
    await waitFor(() =>
      expect(body.textContent).toContain(seedPhrase(seed))
    );
    expect(body.textContent).not.toContain(s.historyModalEmpty);
  });

  it("says the row was on the DEFAULT, never that it was blank", async () => {
    // The list row's own line. 「（當時是空白內容）」 is a claim about content and
    // it is false here — the revision has content, just not its own.
    const utils = await openReader({
      kind: "role_definition",
      docKey: "assistant",
      revision: retained({ definition_md: "", tombstoned: "true" }),
      currentContent: { definition_md: "owner 的版本" },
    });
    const row = utils.getByTestId("doc-history-item-42");
    expect(row.textContent).toContain(s.historyDefaultContent);
    expect(row.textContent).not.toContain(s.historyNoContent);
  });

  // ── the reverse: a version that REALLY stored an empty string ──────────────
  // Without this, the fix above could quietly collapse two different states into
  // one and nothing would notice.
  it("still calls a genuinely blank revision blank", async () => {
    const utils = await openReader({
      kind: "global_context",
      docKey: "global",
      revision: retained({ text: "", tombstoned: "false" }),
      currentContent: { text: "owner 寫的區塊" },
    });

    const row = utils.getByTestId("doc-history-item-42");
    expect(row.textContent).toContain(s.historyNoContent);
    expect(row.textContent).not.toContain(s.historyDefaultContent);

    fireEvent.click(utils.getByTestId("doc-history-open-42"));
    const modal = await utils.findByTestId("doc-history-modal");
    await waitFor(() =>
      expect(modal.textContent).toContain(s.historyModalEmpty)
    );
    expect(modal.textContent).not.toContain(s.historyModalDefaultContent);
  });

  // ── global_context must not regress ────────────────────────────────────────
  // Its default IS the empty document (`documentSeedContent` says so), so the
  // substitution above changes nothing here — and that is worth pinning,
  // because this is the one kind where the old, wrong reading looked right.
  it("keeps telling the truth for the document whose default is empty", async () => {
    const utils = await openReader({
      kind: "global_context",
      docKey: "global",
      revision: retained({ text: "", tombstoned: "true" }),
      currentContent: { text: "owner 寫的區塊" },
    });

    // The row says 「當時採用出廠預設內容」 — which for this document happens to
    // also be empty, but the reason is the tombstone, not emptiness.
    expect(utils.getByTestId("doc-history-item-42").textContent).toContain(
      s.historyDefaultContent
    );

    fireEvent.click(utils.getByTestId("doc-history-open-42"));
    fireEvent.click(await utils.findByTestId("doc-history-pane-diff"));
    const drawn = await waitFor(() => {
      const found = markers(utils.container);
      expect(found.length).toBeGreaterThan(0);
      return found;
    });
    // Restoring it really WOULD take the owner's block away, and here saying so
    // is CORRECT — the whole document is one line, and the default it goes back
    // to is genuinely the empty one. Nothing above changed that.
    expect(drawn).toEqual(["+"]);
    const rows = utils.container.querySelectorAll(".diff-view__row");
    expect(rows[0]?.querySelectorAll("td")[3]?.textContent).toBe(
      "owner 寫的區塊"
    );

    // …and the CONTENT pane names the reason that pane is empty. For this one
    // kind the shipped default really is the empty document, so the pane has
    // nothing to render — but 「這個版本沒有任何內容」 would still be the wrong
    // sentence, because the reason is the tombstone, not emptiness.
    fireEvent.click(utils.getByTestId("doc-history-pane-content"));
    const modal = utils.getByTestId("doc-history-modal");
    expect(modal.textContent).toContain(s.historyModalDefaultContent);
    expect(modal.textContent).not.toContain(s.historyModalEmpty);
  });

  // ── the seed could not be read ────────────────────────────────────────────
  // The substitution has an input, and that input can be missing: the seed GET
  // fails, or the kind ships no /seed route at all. Falling back to the empty
  // text column there resurrects the original lie under a green suite — this is
  // the branch that stops it.
  it("refuses to paint a wipe when the default cannot be READ", async () => {
    const utils = await openReader({
      kind: "role_definition",
      docKey: "assistant",
      revision: retained({ definition_md: "", tombstoned: "true" }),
      currentContent: { definition_md: ["第一行", "第二行", "第三行"].join("\n") },
      seedFails: true,
    });

    fireEvent.click(utils.getByTestId("doc-history-open-42"));
    fireEvent.click(await utils.findByTestId("doc-history-pane-diff"));

    // 🔴 THE WIPE ASSERTION COMES FIRST, on purpose. If the notice were checked
    // first, a regression here would report "the notice is missing" — true, but
    // it buries the thing that actually matters, which is that the pane went
    // back to drawing the whole live document as an addition on top of nothing.
    // Assertion order is what decides which sentence the next person reads.
    await waitFor(() => expect(utils.getByTestId("doc-history-modal")).toBeTruthy());
    expect(markers(utils.container)).toHaveLength(0);
    expect(utils.container.querySelector(".diff-view")).toBeNull();
    // …and instead of a diff, the pane says why it has none.
    expect(utils.getByTestId("doc-history-default-unreadable")).toBeTruthy();

    // …the same story in the other pane, and restore stays live: putting the
    // document back on its default needs nothing from this client.
    fireEvent.click(utils.getByTestId("doc-history-pane-content"));
    expect(utils.getByTestId("doc-history-default-unreadable")).toBeTruthy();
    expect(
      (utils.getByTestId("doc-history-modal-restore") as HTMLButtonElement)
        .disabled
    ).toBe(false);
  });

  it("names the RIGHT version when it says the default cannot be read", async () => {
    // 「初始版本的內容目前讀不到…還原成初始版本仍然可以執行」 was written for the
    // 初始版本 ROW. Printed on a revision that has an id, a timestamp and an
    // author, it misidentifies the version standing next to a destructive
    // button — the same family of defect as the diff this file exists to fix.
    const utils = await openReader({
      kind: "role_definition",
      docKey: "assistant",
      revision: retained({ definition_md: "", tombstoned: "true" }),
      currentContent: { definition_md: "owner 的版本" },
      seedFails: true,
    });

    fireEvent.click(utils.getByTestId("doc-history-open-42"));
    const notice = await utils.findByTestId("doc-history-default-unreadable");
    expect(notice.textContent).toBe(s.historyDefaultUnreadable);
    // The 初始版本 row's own sentence must NOT be what a retained revision says.
    expect(notice.textContent).not.toBe(s.historySeedUnavailable);
    expect(
      utils.queryByTestId("doc-history-seed-unavailable")
    ).toBeNull();
  });

  // ── insight: the third kind, now with a witness ───────────────────────────
  // `api/mock.ts` used to 404 this document's /seed while the server has served
  // it since T-6501, so until now this kind's only observable change was one
  // wrong screen swapped for another. Same shape as the role_definition case
  // above, on the kind the owner's screenshot actually came from.
  it("diffs a tombstoned INSIGHT revision as its per-role seed", async () => {
    const seed = (await mockApi.getDocumentSeed("insight", "assistant")).content
      .text;
    const seedLines = seed.split("\n");
    expect(seedLines.length).toBeGreaterThan(2);

    const utils = await openReader({
      kind: "insight",
      docKey: "assistant",
      // api_insight.go:195 — `Insight{RoleKey: roleKey, Tombstoned: true}`, so
      // the text column is the zero value here too.
      revision: retained({ text: "", tombstoned: "true" }),
      currentContent: {
        text: ["owner 改寫的第一行", ...seedLines.slice(1)].join("\n"),
      },
    });

    fireEvent.click(utils.getByTestId("doc-history-open-42"));
    fireEvent.click(await utils.findByTestId("doc-history-pane-diff"));
    const drawn = await waitFor(() => {
      const found = markers(utils.container);
      expect(found.length).toBeGreaterThan(0);
      return found;
    });

    expect(drawn.filter((m) => m === "+")).toHaveLength(1);
    expect(drawn.filter((m) => m === "-")).toHaveLength(1);
    expect(drawn.filter((m) => m !== "+" && m !== "-").length).toBeGreaterThan(0);
  });
});

/** A phrase from the shipped default that can only be on screen if the seed
 * itself is what got rendered. The markdown SYNTAX is stripped off, because the
 * content pane renders markdown — asserting on the raw `# ` would fail on a
 * correctly rendered heading. */
function seedPhrase(seed: string): string {
  const line = seed.split("\n").find((l) => l.trim() !== "") ?? "";
  return line.replace(/^[#\-*>\s]+/, "").trim();
}
