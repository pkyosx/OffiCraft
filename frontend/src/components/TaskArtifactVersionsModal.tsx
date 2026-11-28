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
// carries only `artifact_count` since T-92, not one artifact row, and a
// version diff needs kind/url/mime), NOT from the artifact rows the popover is
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
// 🔴 WITH ONE FALLBACK: an `application/octet-stream` response whose NAME ends
// in a text extension is read as text (hooks/useVersionPayload.ts). That mime
// says the type is not known, not that the bytes are binary — the server
// answers it for text extensions outside its own type table (.ini, .env, .css).
//
// THE CONTENT PANE IS ContentView, the same body every other file surface
// draws: a markdown report is read rendered, JSON/XML indented, a CSV as a
// table, an HTML page in the sandboxed frame at its minted preview link. The
// 差異 pane stays the raw-text DiffView.

import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useI18n } from "../i18n";
import { api } from "../api";
import { useActorName } from "../hooks/useActorName";
import type {
  TaskArtifactView,
  TaskArtifactVersionView,
} from "../api/adapter";
import { formatAbsolute } from "../lib/dateFormat";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { DiffView } from "./DiffView";
import { VersionContent } from "./VersionContent";
import { useVersionPayload, type ArtifactPayload } from "../hooks/useVersionPayload";
import { CloseIcon } from "./icons";
import "./task-artifact-versions.css";

type Pane = "content" | "diff";

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
  const actorName = useActorName();
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
  // (T-66 cut `getTask` down to an id+label index and T-92 removed even that,
  // leaving a bare count a version reader cannot be drawn from); the popover's
  // rows are not consulted here at all.
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

  // The NAME each side is read under — a fallback for the mime, never a
  // substitute: see loadArtifactPayload.
  //
  // ⚠️ THE TWO SIDES READ DIFFERENT FIELDS SINCE T-92, and they are NOT the
  // same fact one step apart. A VERSION still carries its own `filename` on the
  // wire (this journal is a cockpit-only read of a bounded few rows and was not
  // narrowed), so `report.md` reaches this line intact. The LIVE artifact does
  // not: T-92 dropped `filename` and made `name` a REQUIRED author-chosen title
  // — `artifactDisplayName` returns the stored name FIRST and only falls back
  // to the blob's filename when there is none, so on any artifact written since
  // T-92 the live `name` is 「週報 v3」, extension and all evidence of the file
  // type gone.
  //
  // 🔴 That is why the live side cannot be judged from this string alone. The
  // filename is not lost, only moved: it rides the bytes response's
  // `Content-Disposition`, which loadArtifactPayload reads for exactly this
  // reason. Without that, a live .md pinned under a title read as `opaque`, and
  // the 差異 tab — which needs BOTH sides to be text — vanished for the most
  // ordinary deliverable this panel has.
  const versionName = (v: TaskArtifactVersionView) => v.filename || v.name;
  const liveName = live ? live.name : "";
  const selectedPayload = useVersionPayload(
    selected === "live" ? live?.kind : selectedVersion?.kind,
    selected === "live" ? live?.url : selectedVersion?.url,
    selected === "live"
      ? liveName
      : selectedVersion
        ? versionName(selectedVersion)
        : "",
    "TaskArtifactVersionsModal",
  );
  const livePayload = useVersionPayload(live?.kind, live?.url, liveName, "TaskArtifactVersionsModal");

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
  // a version's stored `name` is empty unless an agent chose to send one (T-92
  // split the old single `label` into name + description, and the history
  // table's `name` is NOT derived the way the live row's is), while its
  // `filename` is on the wire for every retained file. Reading only the name
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
    return (
      <VersionContent
        payload={p}
        notices={{
          loading: t.tasks.artifacts.versionsLoading,
          error: t.tasks.artifacts.versionsContentError,
          gone: t.tasks.artifacts.versionsContentGone,
          opaque: (mime) => msg.taskArtifactOpaque(mime),
        }}
        noticeClassName="ta-versions__notice"
        testId={testId}
      />
    );
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
                        ? ` · ${msg.taskArtifactVersionBy(actorName(row.v!.createdBy))}`
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
