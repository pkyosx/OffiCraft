// components/TaskArtifactVersionsModal.tsx — reading what a pinned deliverable
// USED TO BE (T-60).
//
// A task artifact can now be REPLACED in place: the `ta-` id never moves, the
// content behind it does, and the few versions it replaced are retained. This
// is the cockpit's reader for that journal, opened from the 「N 版」 entry on the
// artifact row.
//
// ITS SHAPE IS DocumentHistoryModal's, and deliberately so — the cockpit already
// has one answer to "read one retained version, and show me what changed":
// a scrim + centred panel, the version's identity on the left, a 內容 / 差異
// toggle top-right. What is different here is that an artifact set has MANY
// artifacts and each has its OWN short journal, so the version LIST lives in
// this same panel (a left pane) instead of in a card behind it.
//
// 🔴 THE ONE CRITERION, inherited verbatim from DocumentHistoryModal: what the
// diff says must equal the actual state. So the 「目前」 side is read from the
// SERVER when this modal opens (`api.listTaskArtifacts` — `api.getTask`
// carries only id+label since T-66, and a version diff needs
// kind/url/filename/mime), NOT from the artifact rows the popover is
// holding. Those rows are an SSE-driven cache: they are refetched when a
// `task` event fans, which means they are right most of the time and
// silently stale exactly when they are not — a replace that landed while the
// popover sat open, or a fan-out that was dropped. A diff whose `+` side is
// a cache is a claim about the server that nothing verifies, and the
// document series records that this class of lie survives a fully green
// suite. One read at open costs one request and removes the class.
//
// If the artifact is GONE from the task the server just handed back (un-pinned
// from another surface while this was opening), that is said out loud — the
// versions are NOT diffed against the last thing this client happened to know.
//
// 🔴 THERE IS NO RESTORE. The server ships no restore verb by decision (an older
// version comes back by replacing FORWARD with it, which is the executing
// agent's write), and this reader does not grow a face for one.
//
// WHAT A "DIFF" IS DEPENDS ON THE ARTIFACT, and the three answers are different
// screens rather than one screen with holes in it:
//   * TEXT FILES — the two versions' bytes are fetched and handed to the shared
//     DiffView. No new comparison component, and DiffView/lineDiff are NOT
//     touched (T-59 owns them); the only knob this side may turn is the
//     `DiffViewOptions.maxLines` caller option, and it does not turn it — the
//     shared ceiling and its honest 「太大」 screen apply here too.
//   * IMAGES AND OTHER NON-TEXT FILES — DiffView cannot read them, so the panes
//     become a 替換前 / 替換後 toggle over the same viewing area. A toggle rather
//     than a side-by-side pair because this panel also has to work at 360px,
//     where two images side by side are two thumbnails.
//   * LINKS — the version IS a url, so the comparison is the old url and the new
//     one, printed.
//
// WHETHER BYTES ARE TEXT IS ASKED OF THE RESPONSE, not of the artifact row.
// A version does carry its own `mime` now, but the row's copy and the bytes'
// content type are two statements and only the second is the one being read —
// so each side is fetched and the SERVER's own `Content-Type` decides, which is
// the same authority `isInlineDisplayableMime` defers to. What must never
// happen is borrowing the LIVE artifact's mime for a version: kind is immutable
// across versions, but a file artifact may be a .txt today and have been a .pdf
// before. A response that is not text is cancelled unread rather than
// downloaded to be thrown away.
//
// The version's `filename` is the same kind of per-version fact: the wire
// resolves it from THAT version's retained blob, so the fallback below reads
// each side under its own name rather than under the live row's.
//
// 🔴 WITH ONE FALLBACK, because a mime-only rule loses the common case: an
// `application/octet-stream` file whose NAME ends in a text extension is read as
// text (TEXTUAL_EXTENSIONS below). That mime is an upload path saying it does
// not know, not a claim of binary — and the reports, logs and specs this cockpit
// mostly holds arrive under it. Without this, the deliverable class that made
// this reader exist would never reach the diff at all.

import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useI18n } from "../i18n";
import { api } from "../api";
import { authedAttachmentUrl } from "../api/http";
import type {
  TaskArtifactView,
  TaskArtifactVersionView,
} from "../api/adapter";
import { formatAbsolute } from "../lib/dateFormat";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { DiffView } from "./DiffView";
import { CloseIcon } from "./icons";
import "./task-artifact-versions.css";

type Pane = "content" | "diff";
/** Which side of a non-text comparison the single viewing area is showing. */

/** What one version's bytes turned out to BE. `opaque` is the honest answer for
 * a file this surface cannot render or compare — it is never collapsed into
 * "empty", which is a different and false statement. */
export type ArtifactPayload =
  | { state: "loading" }
  | { state: "text"; text: string }
  | { state: "image"; src: string }
  | { state: "opaque"; mime: string }
  | { state: "link"; url: string }
  | { state: "gone" }
  | { state: "error" };

/** Extensions this panel reads as text when the RESPONSE will not say so.
 *
 * 🔴 A mime test alone is not enough, and the ticket's own motivating artifact
 * is the proof: a `.md` report uploaded through the agent tooling comes back
 * `application/octet-stream`, which is what an upload path says when it does not
 * know — not a claim that the bytes are binary. Reports, logs and specs are the
 * deliverables this cockpit sees most, so a mime-only rule sends exactly the
 * common case to the 前/後 toggle, where it can never be diffed. Wrong in the
 * OTHER direction is cheap: this list is closed and holds only extensions whose
 * bytes are text by definition, so the worst case is text rendered as text. */
const TEXTUAL_EXTENSIONS = new Set([
  "md", "txt", "log", "json", "csv", "yaml", "yml", "diff", "patch", "sql",
  "go", "ts", "tsx", "js", "py", "sh", "toml", "ini", "env", "html", "css",
  "xml",
]);

/** Whether a deliverable's NAME says its bytes are text. The name is the blob's
 * filename, falling back to the label — on a retained version exactly as on the
 * live artifact; a deliverable with neither simply falls through to the mime
 * answer. */
export function looksTextualByName(name: string): boolean {
  const dot = name.lastIndexOf(".");
  if (dot < 0) return false;
  return TEXTUAL_EXTENSIONS.has(name.slice(dot + 1).toLowerCase());
}

/** Read ONE version's content. The kind decides how far this has to go: a link
 * needs no fetch at all, an image is displayed by the browser rather than read
 * here, and only a file is actually fetched — headers first, body only if the
 * bytes are text.
 *
 * `name` is the second opinion on that last question. The server's own
 * `Content-Type` still has the FIRST word (both directions: `text/*` is text,
 * `image/*` is an image and the name never overrules it), and the name is only
 * consulted when the mime is neither — which is where `application/octet-stream`
 * lands. */
export async function loadArtifactPayload(
  kind: "file" | "image" | "link",
  url: string,
  name = "",
): Promise<ArtifactPayload> {
  if (!url) return { state: "gone" };
  if (kind === "link") return { state: "link", url };
  if (kind === "image") return { state: "image", src: authedAttachmentUrl(url) };
  const res = await fetch(authedAttachmentUrl(url));
  if (!res.ok) throw new Error(`http ${res.status}`);
  const mime = (res.headers.get("content-type") ?? "")
    .split(";")[0]!
    .trim()
    .toLowerCase();
  const isImageMime = mime.startsWith("image/");
  if (mime.startsWith("text/") || (!isImageMime && looksTextualByName(name))) {
    return { state: "text", text: await res.text() };
  }
  // Not text: drop the body instead of downloading a binary this panel cannot
  // show anyway. An image mime still displays — `kind` only tells us the server
  // did not flag it as one.
  await res.body?.cancel().catch(() => {});
  if (isImageMime) {
    return { state: "image", src: authedAttachmentUrl(url) };
  }
  return { state: "opaque", mime };
}

/** The one loader both sides share. Nothing about it is version-specific, which
 * is the point: the live artifact's bytes are read exactly the way a retained
 * version's are, so neither side can be measured more generously than the other. */
function usePayload(
  kind: "file" | "image" | "link" | undefined,
  url: string | undefined,
  name = "",
): ArtifactPayload {
  const [payload, setPayload] = useState<ArtifactPayload>({ state: "loading" });
  useEffect(() => {
    if (kind === undefined || url === undefined) {
      setPayload({ state: "loading" });
      return;
    }
    let alive = true;
    setPayload({ state: "loading" });
    loadArtifactPayload(kind, url, name)
      .then((p) => {
        if (alive) setPayload(p);
      })
      .catch((e) => {
        if (alive) setPayload({ state: "error" });
        console.warn("TaskArtifactVersionsModal: content read failed", e);
      });
    return () => {
      alive = false;
    };
  }, [kind, url, name]);
  return payload;
}

export function TaskArtifactVersionsModal({
  taskId,
  artifactId,
  onClose,
}: {
  taskId: string;
  artifactId: string;
  onClose: () => void;
}) {
  const { t, msg } = useI18n();
  const [versions, setVersions] = useState<TaskArtifactVersionView[] | null>(
    null,
  );
  /** `undefined` while the task read is in flight, `null` once the server has
   * answered and the artifact is NOT on it any more. */
  const [live, setLive] = useState<TaskArtifactView | null | undefined>(
    undefined,
  );
  const [failed, setFailed] = useState(false);
  const [selected, setSelected] = useState<number | "live" | null>(null);
  const [pane, setPane] = useState<Pane>("content");
  const nowTs = Date.now() / 1000;

  // 🔴 The `+` side comes from the SERVER, in the same breath as the journal —
  // see the header. `listTaskArtifacts` is the cockpit's own FULL artifact read
  // (T-66 left `getTask` carrying an id+label index a version reader cannot be
  // drawn from); the popover's rows are not consulted here at all.
  useEffect(() => {
    let alive = true;
    Promise.all([
      api.listTaskArtifactVersions(taskId, artifactId),
      api.listTaskArtifacts(taskId),
    ])
      .then(([list, artifacts]) => {
        if (!alive) return;
        setVersions(list);
        setLive(artifacts.find((a) => a.id === artifactId) ?? null);
        setSelected(list.length > 0 ? list[0]!.id : "live");
      })
      .catch((e) => {
        if (alive) setFailed(true);
        console.warn("TaskArtifactVersionsModal: load failed", e);
      });
    return () => {
      alive = false;
    };
  }, [taskId, artifactId]);

  // Esc closes — as a LAYER. This panel portals to document.body, so it is not
  // inside the artifacts popover's subtree: escapeLayers breaks the tie between
  // two non-nesting surfaces in favour of the one registered LAST, which is this
  // one. The popover stays open behind it and takes the next Esc.
  const rootRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(onClose, rootRef);

  const selectedVersion =
    typeof selected === "number"
      ? (versions?.find((v) => v.id === selected) ?? null)
      : null;

  // The NAME each side is read under — the blob's own filename first, the label
  // second, on BOTH sides. A retained version's filename is that version's own
  // fact (the wire resolves it from its `attachment_id`), so neither side is
  // named more generously than the other: a label-less .md report is still a
  // .md on the older side. It is a fallback for the mime, never a substitute:
  // see loadArtifactPayload.
  //
  // ⚠️ THE TWO SIDES READ DIFFERENT FIELDS SINCE T-92, and that asymmetry is
  // real rather than an oversight: a VERSION still carries its own `filename`
  // (this journal is a cockpit-only read of a bounded few rows and was not
  // narrowed), while the LIVE artifact no longer does — its `name` IS the
  // server's derivation from that filename, so reading it reads the same fact
  // one step later.
  const versionName = (v: TaskArtifactVersionView) => v.filename || v.name;
  const liveName = live ? live.name : "";
  const selectedPayload = usePayload(
    selected === "live" ? live?.kind : selectedVersion?.kind,
    selected === "live" ? live?.url : selectedVersion?.url,
    selected === "live"
      ? liveName
      : selectedVersion
        ? versionName(selectedVersion)
        : "",
  );
  const livePayload = usePayload(live?.kind, live?.url, liveName);

  /**
   * Whether the 差異 tab exists AT ALL for what is on screen.
   *
   * 🔴 owner 2026-09-03 (c-5d9766b7f0a0): 「好像只有文字檔需要有差異那個選項」.
   * The test is the PAYLOAD, not the kind: a file whose bytes do not read as
   * text (a pdf, a zip) used to get a tab whose only content was "nothing to
   * compare" — a dead end the reader had to click to discover. An image and a
   * link never get one either.
   */
  const diffable =
    selected !== "live" &&
    live !== null &&
    selectedPayload.state === "text" &&
    livePayload.state === "text";

  // The tab can disappear underneath the pane — pick another version, or let a
  // still-loading side resolve to something unreadable — so the pane follows it
  // back rather than leaving the body blank.
  useEffect(() => {
    if (!diffable) setPane("content");
  }, [diffable]);

  // Word-for-word the live side's fallback chain below, and that is the point:
  // a version's `label` is empty unless an agent chose to send one, while its
  // `filename` is on the wire for every retained file. Reading only the label
  // rendered the whole older column as "unnamed" underneath a named live row,
  // and hid the one difference between two versions a reader most needs to see —
  // that the deliverable was re-filed under a new name.
  //
  // The live side needs no such chain since T-92 — the server guarantees a
  // non-empty `name` — but the `||` tail stays for an older server or a fixture
  // that sends none, so the column never reads "unnamed" for a reason that is
  // about the payload rather than about the deliverable.
  const versionTitle = (v: TaskArtifactVersionView) =>
    v.name ||
    v.filename ||
    (v.kind === "link" ? v.url : t.tasks.artifacts.versionsUnnamed);
  const liveTitle =
    live &&
    (live.name ||
      (live.kind === "link" ? live.url : t.tasks.artifacts.versionsUnnamed));

  const rows = useMemo(
    () => [{ key: "live" as const }, ...(versions ?? []).map((v) => ({ key: v.id, v }))],
    [versions],
  );

  function payloadBlock(p: ArtifactPayload, testId: string) {
    switch (p.state) {
      case "loading":
        return (
          <p className="ta-versions__notice" data-testid={`${testId}-loading`}>
            {t.tasks.artifacts.versionsLoading}
          </p>
        );
      case "error":
        return (
          <p className="ta-versions__notice" data-testid={`${testId}-error`}>
            {t.tasks.artifacts.versionsContentError}
          </p>
        );
      case "gone":
        return (
          <p className="ta-versions__notice" data-testid={`${testId}-gone`}>
            {t.tasks.artifacts.versionsContentGone}
          </p>
        );
      case "opaque":
        return (
          <p className="ta-versions__notice" data-testid={`${testId}-opaque`}>
            {msg.taskArtifactOpaque(p.mime)}
          </p>
        );
      case "link":
        return (
          <a
            className="ta-versions__url"
            data-testid={`${testId}-link`}
            href={p.url}
            target="_blank"
            rel="noopener noreferrer"
          >
            {p.url}
          </a>
        );
      case "image":
        return (
          <img
            className="ta-versions__image"
            data-testid={`${testId}-image`}
            src={p.src}
            alt=""
          />
        );
      case "text":
        return (
          <pre className="ta-versions__text" data-testid={`${testId}-text`}>
            {p.text}
          </pre>
        );
    }
  }

  /**
   * The 差異 pane. ONE screen: a line diff of two texts.
   *
   * 🔴 owner 2026-09-03 (c-5d9766b7f0a0, verbatim): 「好像只有文字檔需要有差異
   * 那個選項」. So the pane is not reached at all unless BOTH sides read as text
   * — see `diffable` — and the older shapes this function used to carry (a
   * link's two urls, an image's before/after toggle, and the "nothing to
   * compare" notices) are gone rather than unreachable. A dead branch that
   * still renders is the one a reader trusts.
   */
  function diffBody() {
    if (selectedPayload.state !== "text" || livePayload.state !== "text") {
      // Unreachable while `diffable` gates the tab; kept as a total function
      // rather than a cast, and deliberately silent.
      return null;
    }
    return (
      <DiffView
        before={selectedPayload.text}
        after={livePayload.text}
        beforeLabel={
          selectedVersion
            ? msg.taskArtifactVersionLabel(
                formatAbsolute(selectedVersion.createdTs, nowTs),
              )
            : t.tasks.artifacts.versionsUnnamed
        }
        afterLabel={t.tasks.artifacts.versionsCurrent}
        testId="ta-versions-diff"
      />
    );
  }

  function body() {
    if (failed) {
      return (
        <p className="ta-versions__notice" data-testid="ta-versions-load-error">
          {t.tasks.artifacts.versionsLoadError}
        </p>
      );
    }
    if (versions === null || selected === null) {
      return (
        <p className="ta-versions__notice" data-testid="ta-versions-loading">
          {t.tasks.artifacts.versionsLoading}
        </p>
      );
    }
    if (pane === "content") {
      if (selected === "live" && live === null) {
        return (
          <p className="ta-versions__notice" data-testid="ta-versions-unpinned">
            {t.tasks.artifacts.versionsUnpinned}
          </p>
        );
      }
      return payloadBlock(selectedPayload, "ta-versions-content");
    }
    return diffBody();
  }

  return createPortal(
    <div
      ref={rootRef}
      className="ta-versions"
      data-testid="ta-versions-modal"
      role="dialog"
      aria-modal="true"
      aria-label={t.tasks.artifacts.versionsTitle}
      onClick={onClose}
    >
      <div className="ta-versions__panel" onClick={(e) => e.stopPropagation()}>
        <div className="ta-versions__header">
          <span className="ta-versions__title">
            {t.tasks.artifacts.versionsTitle}
          </span>
          <div className="ta-versions__actions">
            {diffable && (
            <div
              className="ta-versions__tabs"
              role="group"
              aria-label={t.tasks.artifacts.versionsPaneLabel}
            >
              {(["content", "diff"] as Pane[]).map((which) => (
                <button
                  key={which}
                  type="button"
                  className={`ta-versions__tab${pane === which ? " ta-versions__tab--on" : ""}`}
                  data-testid={`ta-versions-pane-${which}`}
                  aria-pressed={pane === which}
                  onClick={() => setPane(which)}
                >
                  {which === "content"
                    ? t.tasks.artifacts.versionsPaneContent
                    : t.tasks.artifacts.versionsPaneDiff}
                </button>
              ))}
            </div>
            )}
            <button
              type="button"
              className="ta-versions__close"
              data-testid="ta-versions-close"
              aria-label={t.tasks.artifacts.versionsClose}
              onClick={onClose}
            >
              <CloseIcon size={16} />
            </button>
          </div>
        </div>

        <div className="ta-versions__split">
          <ul className="ta-versions__list" data-testid="ta-versions-list">
            {rows.map((row) =>
              row.key === "live" ? (
                <li key="live">
                  <button
                    type="button"
                    className={`ta-versions__row${selected === "live" ? " ta-versions__row--on" : ""}`}
                    data-testid="ta-versions-row-live"
                    aria-pressed={selected === "live"}
                    onClick={() => setSelected("live")}
                  >
                    <span className="ta-versions__row-name">
                      {t.tasks.artifacts.versionsCurrent}
                    </span>
                    <span className="ta-versions__row-meta">
                      {live === null
                        ? t.tasks.artifacts.versionsUnpinned
                        : (liveTitle ?? "")}
                    </span>
                  </button>
                </li>
              ) : (
                <li key={row.key}>
                  <button
                    type="button"
                    className={`ta-versions__row${selected === row.key ? " ta-versions__row--on" : ""}`}
                    data-testid={`ta-versions-row-${row.key}`}
                    aria-pressed={selected === row.key}
                    onClick={() => setSelected(row.key)}
                  >
                    <span className="ta-versions__row-name">
                      {versionTitle(row.v!)}
                    </span>
                    <span className="ta-versions__row-meta">
                      {formatAbsolute(row.v!.createdTs, nowTs)}
                      {row.v!.createdBy
                        ? ` · ${msg.taskArtifactVersionBy(row.v!.createdBy)}`
                        : ""}
                    </span>
                  </button>
                </li>
              ),
            )}
            {versions !== null && versions.length === 0 && (
              <li
                className="ta-versions__notice"
                data-testid="ta-versions-empty"
              >
                {t.tasks.artifacts.versionsEmpty}
              </li>
            )}
          </ul>
          <div className="ta-versions__body" data-pane={pane}>
            {body()}
          </div>
        </div>
      </div>
    </div>,
    document.body,
  );
}
