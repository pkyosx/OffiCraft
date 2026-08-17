// DocumentHistoryModal — reading, comparing and restoring ONE retained
// revision (T-1f39).
//
// What is pinned here, in the order the owner meets it:
//   1. the DEFAULT pane is the version's content RENDERED as markdown — a real
//      <h2>/<li>, not the `##` characters that produced it;
//   2. the TOP-RIGHT toggle swaps to the line diff, which shows this version as
//      `-` and the CURRENT stored content as `+`, with each side's own line
//      numbers; and toggling back returns to the rendered document;
//   3. restore rides the caller's action, but only after the confirmation, and
//      closes the reader on success;
//   4. a FAILED restore keeps both dialogs open and shows the SERVER's reason;
//   5. an over-cap revision cannot be restored from in here either.

import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { DocumentHistoryModal } from "./DocumentHistoryModal";
import { ApiError } from "../api/errors";
import { DOC_CAP_CHARS_DEFAULT, DOC_CAP_CHARS_DEFAULTS } from "../api/docCap";
import type { DocumentKind } from "../types";
import { contentSizes } from "../api/docCap";

const s = zh.settings;

const VERSION_TS = 1753776180; // 2025-07-29 in the runner's local zone
const VERSION_MD = ["## 標題", "", "- 第一項", "- 第二項"].join("\n");


function open(opts: {
  kind?: DocumentKind;
  content?: Record<string, string>;
  /** Explicit `undefined` means "the host document has not loaded" — a default
   * parameter cannot express that (JS would substitute the default), so the
   * key's PRESENCE is what decides. */
  currentContent?: Record<string, string>;
  onRestore?: () => Promise<void>;
  onBack?: () => void;
  onClose?: () => void;
}) {
  const {
    kind = "lessons" as DocumentKind,
    content = { text: VERSION_MD },
    onBack,
    onRestore = async () => {},
    onClose = () => {},
  } = opts;
  const currentContent =
    "currentContent" in opts ? opts.currentContent : { text: VERSION_MD };
  return render(
    <I18nProvider>
      <DocumentHistoryModal
        kind={kind}
        // The host passes the row's facts and the revision's OWN text — two
        // reads since T-1170, so the fixture spells both rather than one
        // object that carries everything.
        createdTs={VERSION_TS}
        tombstoned={content.tombstoned === "true"}
        sizes={contentSizes(content)}
        content={content}
        actorLine="Mira（owner-1）"
        currentContent={currentContent}
        docCaps={DOC_CAP_CHARS_DEFAULTS}
        onBack={onBack}
        onRestore={onRestore}
        onClose={onClose}
      />
    </I18nProvider>
  );
}

/** [beforeLine, afterLine, marker, text] per diff row, in render order. */
function diffRows(container: HTMLElement): string[][] {
  return Array.from(container.querySelectorAll(".diff-view__row")).map((row) =>
    Array.from(row.querySelectorAll("td")).map((td) => td.textContent ?? "")
  );
}

describe("DocumentHistoryModal", () => {
  it("opens on the version's content, RENDERED as markdown", () => {
    const utils = open({});

    // The rendered document, not its source: a heading element and list items.
    // Asserting on the text alone would pass on a <pre> of the raw markdown,
    // which is the exact thing the owner asked not to be shown by default.
    const body = utils.getByTestId("doc-history-modal").querySelector(
      ".doc-hist-modal__body"
    ) as HTMLElement;
    expect(body.querySelector("h2")?.textContent).toBe("標題");
    expect(
      Array.from(body.querySelectorAll("li")).map((li) => li.textContent)
    ).toEqual(["第一項", "第二項"]);
    // …and the syntax itself is gone.
    expect(body.textContent).not.toContain("##");
    expect(body.textContent).not.toContain("- 第一項");

    // The diff is the OTHER pane — it is not on screen until asked for.
    expect(utils.container.querySelector(".diff-view")).toBeNull();
  });

  it("names the version by its time and actor in the header", () => {
    const utils = open({});
    const header = utils.container.querySelector(
      ".doc-hist-modal__header"
    ) as HTMLElement;
    expect(header.textContent).toContain(s.historyByLabel);
    expect(header.textContent).toContain("owner-1");
    expect(header.querySelector(".doc-hist-modal__when")?.textContent).toMatch(
      /\d+\/\d+ \d\d:\d\d/
    );
  });

  it("toggles to the diff, showing this version as - and the current as +", () => {
    const utils = open({
      content: { text: ["共同的第一行", "舊的第二行", "共同的第三行"].join("\n") },
      currentContent: {
        text: ["共同的第一行", "新的第二行", "共同的第三行"].join("\n"),
      },
    });

    fireEvent.click(utils.getByTestId("doc-history-pane-diff"));

    // The exact edit script, line numbers included: line 2 was replaced, and
    // the two unchanged lines keep the same number on both sides.
    expect(diffRows(utils.container)).toEqual([
      ["1", "1", "\u00a0", "共同的第一行"],
      ["2", "", "-", "舊的第二行"],
      ["", "2", "+", "新的第二行"],
      ["3", "3", "\u00a0", "共同的第三行"],
    ]);
    // Which side is which, said in words as well as in colour.
    const diff = utils.getByTestId("doc-history-diff-text");
    expect(within(diff).getByText(/此版本/).textContent).toContain("此版本");
    expect(within(diff).getByText(s.historyCurrentLabel)).toBeTruthy();
    // The comparison is against what the SERVER stores, and says so — the
    // editor above may hold unsaved edits this diff deliberately ignores.
    expect(utils.getByText(s.historyDiffNote)).toBeTruthy();

    // …and back: the rendered document returns, the diff goes away.
    fireEvent.click(utils.getByTestId("doc-history-pane-content"));
    expect(utils.container.querySelector(".diff-view")).toBeNull();
    // The version's OWN text is what came back — this version's second line,
    // never the current document's.
    const body = utils.container.querySelector(
      ".doc-hist-modal__body"
    ) as HTMLElement;
    expect(body.textContent).toContain("舊的第二行");
    expect(body.textContent).not.toContain("新的第二行");
  });

  it("says the two are identical rather than drawing an empty diff", () => {
    const utils = open({});
    fireEvent.click(utils.getByTestId("doc-history-pane-diff"));
    expect(utils.getByTestId("diff-view-empty").textContent).toBe(
      zh.diff.noChanges
    );
  });

  it("declines to compare while the current document has not loaded", () => {
    // The honest degraded state: with no current content, a diff would report
    // the whole document as deleted.
    const utils = open({ currentContent: undefined });
    fireEvent.click(utils.getByTestId("doc-history-pane-diff"));
    expect(utils.getByTestId("doc-history-diff-pending").textContent).toBe(
      s.historyDiffPending
    );
    expect(utils.container.querySelector(".diff-view")).toBeNull();
  });

  it("diffs each field a multi-field revision carries", () => {
    // The retired whole-manual kind still carries four fields in one snapshot;
    // diffing only the first would hide every change below it.
    const utils = open({
      kind: "task_manual",
      content: { sop_md: "第一版 SOP", learnings: "舊的經驗" },
      currentContent: { sop_md: "第一版 SOP", learnings: "新的經驗" },
    });
    fireEvent.click(utils.getByTestId("doc-history-pane-diff"));

    expect(diffRows(utils.getByTestId("doc-history-diff-learnings"))).toEqual([
      ["1", "", "-", "舊的經驗"],
      ["", "1", "+", "新的經驗"],
    ]);
    expect(
      utils.getByTestId("doc-history-diff-sop_md").textContent
    ).toContain(zh.diff.noChanges);
  });

  it("compares a field the CURRENT document has and the revision does not", () => {
    // The union, not the revision's own key set: a field that appeared since
    // the revision was retained is a difference, and walking only one side's
    // names would hide exactly the change the diff was opened for.
    const utils = open({
      kind: "task_manual",
      content: { sop_md: "第一版 SOP" },
      currentContent: { sop_md: "第一版 SOP", learnings: "後來才有的經驗" },
    });
    fireEvent.click(utils.getByTestId("doc-history-pane-diff"));

    expect(diffRows(utils.getByTestId("doc-history-diff-learnings"))).toEqual([
      ["", "1", "+", "後來才有的經驗"],
    ]);
  });

  it("judges the cap on the ONE field the restored series writes back", () => {
    // The split's own behaviour change (T-1f39): restoring a SOP writes the
    // SOP alone, so an over-cap LEARNINGS doc is none of its business. The
    // legacy four-field bundle restores both and is still refused — the
    // contrast is what makes this a statement about scope rather than about
    // this one fixture.
    const overCap = "字".repeat(DOC_CAP_CHARS_DEFAULT + 1);
    const content = { sop_md: "短 SOP", learnings: overCap };
    const current = { sop_md: "短 SOP", learnings: "短" };

    const split = open({ kind: "task_manual_sop", content, currentContent: current });
    expect(
      (split.getByTestId("doc-history-modal-restore") as HTMLButtonElement)
        .disabled
    ).toBe(false);
    expect(split.queryByTestId("doc-history-modal-blocked")).toBeNull();
    split.unmount();

    const bundle = open({ kind: "task_manual", content, currentContent: current });
    expect(
      (bundle.getByTestId("doc-history-modal-restore") as HTMLButtonElement)
        .disabled
    ).toBe(true);
    expect(
      bundle.getByTestId("doc-history-modal-blocked").textContent
    ).toContain(s.historyField.learnings);
  });

  it("restore asks first, then calls through and closes the reader", async () => {
    const onRestore = vi.fn().mockResolvedValue(undefined);
    const onClose = vi.fn();
    const utils = open({ onRestore, onClose });

    fireEvent.click(utils.getByTestId("doc-history-modal-restore"));
    // The confirmation is the gate: nothing has been overwritten yet.
    expect(onRestore).not.toHaveBeenCalled();
    expect(
      within(utils.getByTestId("doc-history-restore-confirm")).getByText(
        s.historyRestoreConfirmAction
      )
    ).toBeTruthy();

    fireEvent.click(utils.getByTestId("doc-history-restore-confirm-btn"));

    await waitFor(() => expect(onRestore).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
  });

  it("keeps the reader open on a failed restore, showing the server's reason", async () => {
    const onRestore = vi
      .fn()
      .mockRejectedValue(
        new ApiError("http 400", 400, "bad_request", "learnings doc is over the limit")
      );
    const onClose = vi.fn();
    const utils = open({ onRestore, onClose });

    fireEvent.click(utils.getByTestId("doc-history-modal-restore"));
    fireEvent.click(utils.getByTestId("doc-history-restore-confirm-btn"));

    // The SERVER's own sentence, not the generic fallback — it is the only
    // thing that says WHY.
    await utils.findByText("learnings doc is over the limit");
    expect(utils.getByTestId("doc-history-restore-confirm")).toBeTruthy();
    expect(utils.getByTestId("doc-history-modal")).toBeTruthy();
    expect(onClose).not.toHaveBeenCalled();
  });

  it("falls back to the dictionary's line when the failure carries no reason", async () => {
    const onRestore = vi.fn().mockRejectedValue(new Error("boom"));
    const utils = open({ onRestore });

    fireEvent.click(utils.getByTestId("doc-history-modal-restore"));
    fireEvent.click(utils.getByTestId("doc-history-restore-confirm-btn"));

    await utils.findByText(s.historyRestoreError);
    expect(utils.getByTestId("doc-history-restore-confirm")).toBeTruthy();
  });

  it("cannot restore an over-cap revision, and says why", () => {
    // Over the cap AND no shorter than what is stored now — the shape the
    // server refuses with a 400. The current content must be SHORTER than the
    // revision for the rule to bite at all.
    const overCap = "字".repeat(DOC_CAP_CHARS_DEFAULT + 1);
    const utils = open({
      content: { text: overCap },
      currentContent: { text: "短" },
    });

    expect(
      within(utils.getByTestId("doc-history-modal")).getByText(
        s.historyBlockedBadge
      )
    ).toBeTruthy();
    const reason = utils.getByTestId("doc-history-modal-blocked");
    expect(reason.textContent).toContain(s.historyField.text);
    expect(reason.textContent).toContain(String(DOC_CAP_CHARS_DEFAULT));

    const restore = utils.getByTestId(
      "doc-history-modal-restore"
    ) as HTMLButtonElement;
    expect(restore.disabled).toBe(true);
    // Dead, not merely dimmed: clicking it opens no confirmation.
    fireEvent.click(restore);
    expect(utils.queryByTestId("doc-history-restore-confirm")).toBeNull();
  });

  it("closes on Esc, on the close button and on the backdrop", () => {
    for (const dismiss of [
      (_u: ReturnType<typeof open>) =>
        fireEvent.keyDown(window, { key: "Escape" }),
      (u: ReturnType<typeof open>) =>
        fireEvent.click(u.getByTestId("doc-history-modal-close")),
      (u: ReturnType<typeof open>) =>
        fireEvent.click(u.getByTestId("doc-history-modal")),
    ]) {
      const onClose = vi.fn();
      const utils = open({ onClose });
      dismiss(utils);
      expect(onClose).toHaveBeenCalledTimes(1);
      utils.unmount();
    }
  });

  it("keeps the reader up while the confirmation is open", () => {
    // A backdrop click with the confirm dialog on top must not dismiss the
    // surface underneath it — the dialog owns the interaction at that point.
    const onClose = vi.fn();
    const utils = open({ onClose });
    fireEvent.click(utils.getByTestId("doc-history-modal-restore"));
    fireEvent.click(utils.getByTestId("doc-history-modal"));
    expect(onClose).not.toHaveBeenCalled();
    expect(utils.getByTestId("doc-history-restore-confirm")).toBeTruthy();
  });

  // T-1f39 (owner 2026-07-31): the reader is now opened FROM a list, so it owes
  // a way back into it. Closing is a different exit — it leaves the history
  // altogether — and a reader that only closes turns "compare the next version
  // too" into a round trip out through the editor and back in.
  it("steps back to the list it was opened from, without closing the history", () => {
    const onBack = vi.fn();
    const onClose = vi.fn();
    const utils = open({ onBack, onClose });

    fireEvent.click(utils.getByTestId("doc-history-modal-back"));
    expect(onBack).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();
  });

  it("offers no way back when it was not opened from a list", () => {
    const utils = open({});
    expect(utils.queryByTestId("doc-history-modal-back")).toBeNull();
  });

  it("says a blank revision is blank instead of rendering nothing", () => {
    const utils = open({ content: { text: "" }, currentContent: { text: "" } });
    expect(utils.getByText(s.historyModalEmpty)).toBeTruthy();
  });

  // ── 初始版本 as a pseudo-version (T-40f0) ──────────────────────────────────
  // The list's bottom row used to jump straight to a reset confirmation, because
  // the seed's content was never sent to the cockpit. It now arrives here and
  // reads/diffs/restores through this one code path, with two differences that
  // both come from the same fact: nobody wrote it and it has no timestamp.
  describe("the shipped default (初始版本)", () => {
    function openSeed(opts: {
      content?: Record<string, string>;
      currentContent?: Record<string, string>;
      seedUnavailable?: boolean;
      onRestore?: () => Promise<void>;
    }) {
      return render(
        <I18nProvider>
          <DocumentHistoryModal
            kind="global_context"
            createdTs={0}
            tombstoned={
              (opts.content ?? { text: "", tombstoned: "true" }).tombstoned ===
              "true"
            }
            sizes={contentSizes(opts.content ?? { text: "", tombstoned: "true" })}
            content={opts.content ?? { text: "", tombstoned: "true" }}
            seed
            seedUnavailable={opts.seedUnavailable}
            actorLine=""
            currentContent={opts.currentContent ?? { text: "owner's block" }}
            docCaps={DOC_CAP_CHARS_DEFAULTS}
            onRestore={opts.onRestore ?? (async () => {})}
            onClose={() => {}}
          />
        </I18nProvider>
      );
    }

    it("names itself instead of inventing a timestamp and an author", () => {
      const utils = openSeed({});
      const header = utils.container.querySelector(
        ".doc-hist-modal__header"
      ) as HTMLElement;
      expect(header.textContent).toContain(s.historySeedTitle);
      // No 修改者 line: there is nobody to name, and naming nobody as somebody
      // is the failure mode a bare `actorLine` would have produced.
      expect(header.textContent).not.toContain(s.historyByLabel);
      expect(
        utils.container.querySelector(".doc-hist-modal__when")?.textContent
      ).toBe(s.historySeedTitle);
    });

    it("diffs against the live document with the default on the - side", () => {
      const utils = openSeed({
        content: { text: "shipped default", tombstoned: "true" },
        currentContent: { text: "owner's rewrite" },
      });
      fireEvent.click(utils.getByTestId("doc-history-pane-diff"));
      expect(diffRows(utils.container)).toEqual([
        ["1", "", "-", "shipped default"],
        ["", "1", "+", "owner's rewrite"],
      ]);
      // The `-` side is labelled 初始版本 — the same slot a retained revision
      // fills with its timestamp.
      expect(
        utils.container.querySelector(".diff-view__label--before")?.textContent
      ).toBe(`-${s.historySeedTitle}`);
    });

    it("restores through the SAME confirmation, with the reset's own wording", async () => {
      const onRestore = vi.fn().mockResolvedValue(undefined);
      const utils = openSeed({ onRestore });

      const restore = utils.getByTestId("doc-history-modal-restore");
      expect(restore.textContent).toBe(s.historySeedRestore);
      fireEvent.click(restore);
      // Looking was free; going back is not, and the gate is the same one.
      expect(onRestore).not.toHaveBeenCalled();
      expect(
        utils.getByTestId("doc-history-restore-confirm").textContent
      ).toContain(s.historySeedConfirm);

      fireEvent.click(utils.getByTestId("doc-history-restore-confirm-btn"));
      await waitFor(() => expect(onRestore).toHaveBeenCalledTimes(1));
    });

    it("says the default could not be READ, and keeps restoring it possible", () => {
      // The two must not be conflated: 「這個版本沒有內容」 is a claim about the
      // document, and it is false here. Restoring needs nothing from this
      // client, so the control stays live.
      const utils = openSeed({ content: {}, seedUnavailable: true });
      expect(
        utils.getByTestId("doc-history-seed-unavailable").textContent
      ).toBe(s.historySeedUnavailable);
      expect(utils.queryByText(s.historyModalEmpty)).toBeNull();
      expect(
        (utils.getByTestId("doc-history-modal-restore") as HTMLButtonElement)
          .disabled
      ).toBe(false);

      // …in BOTH panes — switching does not find a different story.
      fireEvent.click(utils.getByTestId("doc-history-pane-diff"));
      expect(utils.getByTestId("doc-history-seed-unavailable")).toBeTruthy();
      expect(utils.container.querySelector(".diff-view")).toBeNull();
    });
  });
});
