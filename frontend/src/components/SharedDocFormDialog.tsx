// SharedDocFormDialog — 上傳新版本 on one document's page. A file goes up
// through the ordinary attachment upload first; the document then points at
// that blob.

import { useRef, useState, type FormEvent } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { attachmentMimeForName } from "../lib/attachmentMime";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import type { SharedDocKind } from "../types";
import "./confirm-modal.css";
import "./secrets.css";
import "./shared-docs.css";

export function sharedDocKindForFile(filename: string): SharedDocKind {
  return attachmentMimeForName(filename).startsWith("image/") ? "image" : "file";
}

/** The server never changes a document's kind, so a new version must keep it;
 * answers the message to show when `filename` would not. */
export function kindMismatchMessage(
  kind: SharedDocKind,
  filename: string,
  d: { kindMismatchImage: string; kindMismatchFile: string },
): string {
  const next = sharedDocKindForFile(filename);
  if (next === kind) return "";
  return kind === "image" ? d.kindMismatchImage : d.kindMismatchFile;
}

export function SharedDocFormDialog({
  docId,
  kind,
  run,
  onDone,
  onCancel,
}: {
  docId: string;
  kind: SharedDocKind;
  /** Runs one write; resolves "" on success or the message to show. */
  run: (action: () => Promise<unknown>) => Promise<string>;
  onDone: () => void;
  onCancel: () => void;
}) {
  const { t } = useI18n();
  const d = t.sharedDocs;
  const link = kind === "link";
  const [file, setFile] = useState<File | null>(null);
  const [url, setUrl] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const rootRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(() => {
    if (!busy) onCancel();
  }, rootRef);

  const canSubmit = !busy && (link ? url.trim() !== "" : file !== null);

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;
    if (file) {
      const mismatch = kindMismatchMessage(kind, file.name, d);
      if (mismatch) {
        setError(mismatch);
        return;
      }
    }
    setBusy(true);
    setError("");
    const err = await run(async () => {
      const content = file
        ? { attachmentId: await api.uploadAttachment(file, file.name) }
        : { url: url.trim() };
      await api.replaceSharedDoc(docId, content);
    });
    setBusy(false);
    if (err) {
      setError(err);
      return;
    }
    onDone();
  }

  return (
    <div ref={rootRef} className="confirm-modal" role="dialog" aria-modal="true">
      <form
        className="confirm-modal__box shared-docs__form"
        data-testid="shared-doc-replace-form"
        onSubmit={submit}
      >
        <div className="secrets__form-title">{d.replaceTitle}</div>
        {link ? (
          <label className="secrets__field">
            <span className="secrets__label">{d.urlLabel}</span>
            <input
              type="url"
              className="secrets__input"
              data-testid="shared-doc-url-input"
              value={url}
              maxLength={2048}
              placeholder="https://"
              onChange={(e) => setUrl(e.target.value)}
            />
          </label>
        ) : (
          <label className="secrets__field">
            <span className="secrets__label">{d.fileLabel}</span>
            <input
              type="file"
              className="secrets__input"
              data-testid="shared-doc-file-input"
              accept={kind === "image" ? "image/*" : undefined}
              onChange={(e) => {
                setError("");
                setFile(e.target.files?.[0] ?? null);
              }}
            />
          </label>
        )}
        {error && (
          <div className="confirm-modal__error" data-testid="shared-doc-form-error">
            {error}
          </div>
        )}
        <div className="secrets__form-actions">
          <button type="button" className="secrets__btn" disabled={busy} onClick={onCancel}>
            {d.cancel}
          </button>
          <button
            type="submit"
            className="secrets__btn secrets__submit"
            data-testid="shared-doc-form-submit"
            disabled={!canSubmit}
          >
            {d.save}
          </button>
        </div>
      </form>
    </div>
  );
}
