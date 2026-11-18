// SharedDocVersionsModal — 版本紀錄 of one shared document. Same shell as
// TaskArtifactVersionsModal (version list left, 內容 / 差異 right, 差異 only when
// both sides read as text), plus 「還原這一版」 acting on the selected version.
//
// The current side is read from the server when the modal opens, together with
// the list, so a diff never compares against a page that went stale.

import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useI18n } from "../i18n";
import { api } from "../api";
import type { SharedDocDetailView, SharedDocVersionView } from "../types";
import { formatAbsolute } from "../lib/dateFormat";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { DiffView } from "./DiffView";
import { CloseIcon } from "./icons";
import {
  loadArtifactPayload,
  type ArtifactPayload,
} from "./TaskArtifactVersionsModal";
import "./task-artifact-versions.css";

type Pane = "content" | "diff";

function usePayload(
  kind: SharedDocVersionView["kind"] | undefined,
  url: string | undefined,
  name: string,
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
        console.warn("SharedDocVersionsModal: content read failed", e);
      });
    return () => {
      alive = false;
    };
  }, [kind, url, name]);
  return payload;
}

export function SharedDocVersionsModal({
  docId,
  actorName,
  run,
  onClose,
}: {
  docId: string;
  actorName: (id: string) => string;
  run: (action: () => Promise<unknown>) => Promise<string>;
  onClose: () => void;
}) {
  const { t, msg } = useI18n();
  const d = t.sharedDocs;
  const [versions, setVersions] = useState<SharedDocVersionView[] | null>(null);
  const [current, setCurrent] = useState<SharedDocDetailView | null>(null);
  const [failed, setFailed] = useState(false);
  const [selected, setSelected] = useState<number | "current" | null>(null);
  const [pane, setPane] = useState<Pane>("content");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [nonce, setNonce] = useState(0);
  const nowTs = Date.now() / 1000;

  useEffect(() => {
    let alive = true;
    Promise.all([api.listSharedDocVersions(docId), api.getSharedDoc(docId)])
      .then(([list, doc]) => {
        if (!alive) return;
        setVersions(list);
        setCurrent(doc);
        setSelected(list.length > 0 ? list[0]!.id : "current");
      })
      .catch((e) => {
        if (alive) setFailed(true);
        console.warn("SharedDocVersionsModal: load failed", e);
      });
    return () => {
      alive = false;
    };
  }, [docId, nonce]);

  const rootRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(onClose, rootRef);

  const selectedVersion =
    typeof selected === "number"
      ? (versions?.find((v) => v.id === selected) ?? null)
      : null;
  const shown = selected === "current" ? current : selectedVersion;
  const selectedPayload = usePayload(
    shown?.kind,
    shown?.url,
    shown ? shown.filename || shown.name : "",
  );
  const currentPayload = usePayload(
    current?.kind,
    current?.url,
    current ? current.filename || current.name : "",
  );
  const diffable =
    selected !== "current" &&
    selectedPayload.state === "text" &&
    currentPayload.state === "text";
  useEffect(() => {
    if (!diffable) setPane("content");
  }, [diffable]);

  const canRestore =
    selectedVersion !== null && current !== null && current.archivedTs === null;

  async function restore() {
    if (!selectedVersion || busy) return;
    setBusy(true);
    setError("");
    const err = await run(() =>
      api.restoreSharedDocVersion(docId, selectedVersion.id),
    );
    setBusy(false);
    if (err) {
      setError(err);
      return;
    }
    setVersions(null);
    setSelected(null);
    setNonce((n) => n + 1);
  }

  function payloadBlock(p: ArtifactPayload) {
    switch (p.state) {
      case "loading":
        return <p className="ta-versions__notice">{d.versionsLoading}</p>;
      case "error":
      case "gone":
        return <p className="ta-versions__notice">{d.versionsLoadError}</p>;
      case "opaque":
        return <p className="ta-versions__notice">{d.binary}</p>;
      case "link":
        return (
          <a
            className="ta-versions__url"
            href={p.url}
            target="_blank"
            rel="noopener noreferrer"
          >
            {p.url}
          </a>
        );
      case "image":
        return <img className="ta-versions__image" src={p.src} alt="" />;
      case "text":
        return (
          <pre className="ta-versions__text" data-testid="sd-versions-text">
            {p.text}
          </pre>
        );
    }
  }

  function body() {
    if (failed) {
      return <p className="ta-versions__notice">{d.versionsLoadError}</p>;
    }
    if (versions === null || selected === null) {
      return <p className="ta-versions__notice">{d.versionsLoading}</p>;
    }
    if (pane === "diff" && selectedVersion && diffable) {
      if (selectedPayload.state !== "text" || currentPayload.state !== "text") {
        return null;
      }
      return (
        <DiffView
          before={selectedPayload.text}
          after={currentPayload.text}
          beforeLabel={msg.sharedDocVersionLabel(
            formatAbsolute(selectedVersion.createdTs, nowTs),
          )}
          afterLabel={d.versionsCurrent}
          testId="sd-versions-diff"
        />
      );
    }
    return payloadBlock(selectedPayload);
  }

  return createPortal(
    <div
      ref={rootRef}
      className="ta-versions"
      data-testid="sd-versions-modal"
      role="dialog"
      aria-modal="true"
      aria-label={d.versions}
      onClick={onClose}
    >
      <div className="ta-versions__panel" onClick={(e) => e.stopPropagation()}>
        <div className="ta-versions__header">
          <span className="ta-versions__title">
            {current ? `${d.versions} · ${current.name}` : d.versions}
          </span>
          <div className="ta-versions__actions">
            {diffable && (
              <div
                className="ta-versions__tabs"
                role="group"
                aria-label={d.versionsPaneLabel}
              >
                {(["content", "diff"] as Pane[]).map((which) => (
                  <button
                    key={which}
                    type="button"
                    className={`ta-versions__tab${pane === which ? " ta-versions__tab--on" : ""}`}
                    data-testid={`sd-versions-pane-${which}`}
                    aria-pressed={pane === which}
                    onClick={() => setPane(which)}
                  >
                    {which === "content" ? d.versionsPaneContent : d.versionsPaneDiff}
                  </button>
                ))}
              </div>
            )}
            {canRestore && (
              <button
                type="button"
                className="ta-versions__tab"
                data-testid="sd-versions-restore"
                disabled={busy}
                onClick={() => void restore()}
              >
                {d.restore}
              </button>
            )}
            <button
              type="button"
              className="ta-versions__close"
              aria-label={d.versionsClose}
              onClick={onClose}
            >
              <CloseIcon size={16} />
            </button>
          </div>
        </div>
        {error && (
          <p className="ta-versions__notice" role="alert" data-testid="sd-versions-error">
            {error}
          </p>
        )}
        <div className="ta-versions__split">
          <ul className="ta-versions__list" data-testid="sd-versions-list">
            {current && (
              <li>
                <button
                  type="button"
                  className={`ta-versions__row${selected === "current" ? " ta-versions__row--on" : ""}`}
                  data-testid="sd-versions-row-current"
                  aria-pressed={selected === "current"}
                  onClick={() => setSelected("current")}
                >
                  <span className="ta-versions__row-name">{d.versionsCurrent}</span>
                  <span className="ta-versions__row-meta">
                    {formatAbsolute(current.updatedTs, nowTs)} ·{" "}
                    {msg.sharedDocVersionBy(actorName(current.updatedBy))}
                  </span>
                </button>
              </li>
            )}
            {(versions ?? []).map((v) => (
              <li key={v.id}>
                <button
                  type="button"
                  className={`ta-versions__row${selected === v.id ? " ta-versions__row--on" : ""}`}
                  data-testid={`sd-versions-row-${v.id}`}
                  aria-pressed={selected === v.id}
                  onClick={() => setSelected(v.id)}
                >
                  <span className="ta-versions__row-name">
                    {formatAbsolute(v.createdTs, nowTs)}
                  </span>
                  <span className="ta-versions__row-meta">
                    {msg.sharedDocVersionBy(actorName(v.createdBy))}
                  </span>
                </button>
              </li>
            ))}
            {versions !== null && versions.length === 0 && (
              <li className="ta-versions__notice" data-testid="sd-versions-empty">
                {d.versionsEmpty}
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
