// SharedDocVersionsModal — 版本紀錄 of one 檔案庫 document, in the same window
// the documents' own history uses (HistoryWindow): a list of the retained
// versions, then one version read on its own — its content by default, the
// line diff against the current file one toggle away, and 還原 behind a
// destructive confirmation.
//
// What "content" and "diff" mean follows the document's kind: a text file is
// drawn through ContentView and diffed as text; an image has no diff, only the
// picture; a link shows the version's URL beside the current one.
//
// The current side is read from the server when the window opens, together
// with the list, so a diff never compares against a page that went stale.

import { useEffect, useState } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import type { SharedDocDetailView, SharedDocVersionView } from "../types";
import { formatAbsolute } from "../lib/dateFormat";
import { useVersionPayload } from "../hooks/useVersionPayload";
import { DiffView } from "./DiffView";
import { HistoryListWindow, HistoryReaderWindow, type HistoryPane } from "./HistoryWindow";
import { VersionContent } from "./VersionContent";
import "./doc-hist-modal.css";
import "./settings.css";
import "./task-artifact-versions.css";

export function SharedDocVersionsModal({
  docId,
  actorName,
  onRestored,
  onClose,
}: {
  docId: string;
  actorName: (id: string) => string;
  onRestored: () => void;
  onClose: () => void;
}) {
  const { t, msg } = useI18n();
  const d = t.sharedDocs;
  const s = t.settings;
  const [versions, setVersions] = useState<SharedDocVersionView[] | null>(null);
  const [current, setCurrent] = useState<SharedDocDetailView | null>(null);
  const [failed, setFailed] = useState(false);
  const [reading, setReading] = useState<SharedDocVersionView | null>(null);
  const [pane, setPane] = useState<HistoryPane>("content");
  const nowTs = Date.now() / 1000;

  useEffect(() => {
    let alive = true;
    Promise.all([api.listSharedDocVersions(docId), api.getSharedDoc(docId)])
      .then(([list, doc]) => {
        if (!alive) return;
        setVersions(list);
        setCurrent(doc);
      })
      .catch((e) => {
        if (alive) setFailed(true);
        console.warn("SharedDocVersionsModal: load failed", e);
      });
    return () => {
      alive = false;
    };
  }, [docId]);

  const readingPayload = useVersionPayload(
    reading?.kind,
    reading?.url,
    reading ? reading.filename || reading.name : "",
    "SharedDocVersionsModal",
  );
  const currentPayload = useVersionPayload(
    reading && current ? current.kind : undefined,
    current?.url,
    current ? current.filename || current.name : "",
    "SharedDocVersionsModal",
  );
  const diffable = readingPayload.state === "text" && currentPayload.state === "text";
  useEffect(() => {
    if (!diffable) setPane("content");
  }, [diffable]);

  const byLine = (id: string) => `${s.historyByLabel} ${actorName(id)}`;
  const title = current ? `${current.name} · ${d.versions}` : d.versions;

  if (reading === null) {
    return (
      <HistoryListWindow title={title} note={d.versionsNote} onClose={onClose}>
        {failed ? (
          <div className="set-error">{d.versionsLoadError}</div>
        ) : versions === null ? (
          <div className="doc-hist__empty">{d.versionsLoading}</div>
        ) : versions.length === 0 ? (
          <div className="doc-hist__empty" data-testid="sd-versions-empty">
            {d.versionsEmpty}
          </div>
        ) : (
          <ul className="doc-hist__list" data-testid="sd-versions-list">
            {versions.map((v) => (
              <li className="doc-hist__item" key={v.id}>
                <div
                  className="doc-hist__row"
                  data-testid={`sd-versions-open-${v.id}`}
                  role="button"
                  tabIndex={0}
                  title={s.historyOpen}
                  onClick={() => setReading(v)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      setReading(v);
                    }
                  }}
                >
                  <div className="doc-hist__meta">
                    <span className="doc-hist__when">{formatAbsolute(v.createdTs, nowTs)}</span>
                    <span className="doc-hist__actor">{byLine(v.createdBy)}</span>
                    {v.kind !== "link" && current && v.filename && v.filename !== current.filename && (
                      <span className="doc-hist__actor">{v.filename}</span>
                    )}
                    <span className="doc-hist__open">{s.historyOpen}</span>
                  </div>
                  {v.kind === "link" && <div className="doc-hist__empty">{v.url}</div>}
                </div>
              </li>
            ))}
          </ul>
        )}
      </HistoryListWindow>
    );
  }

  const when = formatAbsolute(reading.createdTs, nowTs);
  const versionLabel = msg.docHistoryVersionLabel(when);

  let body;
  if (reading.kind === "link") {
    body = (
      <div className="ta-versions__urls" data-testid="sd-versions-urls">
        <div className="ta-versions__url-row">
          <span className="ta-versions__url-side">{versionLabel}</span>
          <a className="ta-versions__url" href={reading.url} target="_blank" rel="noopener noreferrer">
            {reading.url}
          </a>
        </div>
        <div className="ta-versions__url-row">
          <span className="ta-versions__url-side">{d.versionsCurrentUrl}</span>
          {current && (
            <a className="ta-versions__url" href={current.url} target="_blank" rel="noopener noreferrer">
              {current.url}
            </a>
          )}
        </div>
      </div>
    );
  } else if (pane === "diff" && readingPayload.state === "text" && currentPayload.state === "text") {
    body = (
      <>
        <p className="doc-hist-modal__diff-note">{s.historyDiffNote}</p>
        <DiffView
          before={readingPayload.text}
          after={currentPayload.text}
          beforeLabel={versionLabel}
          afterLabel={s.historyCurrentLabel}
          testId="sd-versions-diff"
        />
      </>
    );
  } else {
    body = (
      <>
        <VersionContent
          payload={readingPayload}
          notices={{
            loading: s.historyLoading,
            error: s.historyVersionUnreadable,
            gone: s.historyVersionUnreadable,
            opaque: () => d.binary,
          }}
          noticeClassName="doc-hist-modal__notice"
          label={reading.filename || reading.name}
          testId="sd-versions-content"
        />
        {reading.kind === "image" && reading.filename && (
          <p className="doc-hist-modal__notice">{reading.filename}</p>
        )}
      </>
    );
  }

  return (
    <HistoryReaderWindow
      ident={
        <>
          <span className="doc-hist-modal__when">{when}</span>
          <span className="doc-hist-modal__actor">{byLine(reading.createdBy)}</span>
        </>
      }
      pane={diffable ? pane : undefined}
      onPane={setPane}
      onBack={() => setReading(null)}
      onClose={onClose}
      restore={{
        label: s.historyRestore,
        confirmBody: msg.docHistoryRestoreConfirm(when),
        confirmLabel: s.historyRestoreConfirmAction,
        run: async () => {
          await api.restoreSharedDocVersion(docId, reading.id);
          onRestored();
        },
      }}
    >
      {body}
    </HistoryReaderWindow>
  );
}
