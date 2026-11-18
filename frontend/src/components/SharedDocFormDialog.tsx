// SharedDocFormDialog — 新增檔案 and 上傳新版本. A file goes up through the
// ordinary attachment upload first; the document then points at that blob.

import { useRef, useState, type FormEvent } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { attachmentMimeForName } from "../lib/attachmentMime";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import type { SharedDocKind } from "../types";
import "./confirm-modal.css";
import "./secrets.css";
import "./shared-docs.css";

type Source = "file" | "link";

export function SharedDocFormDialog({
  mode,
  docId,
  kind,
  run,
  onDone,
  onCancel,
}: {
  mode: "create" | "replace";
  /** replace only. */
  docId?: string;
  /** replace only — the document's fixed kind. */
  kind?: SharedDocKind;
  /** Runs one write; resolves "" on success or the message to show. */
  run: (action: () => Promise<unknown>) => Promise<string>;
  /** create: the new id; replace: "". */
  onDone: (id: string) => void;
  onCancel: () => void;
}) {
  const { t } = useI18n();
  const d = t.sharedDocs;
  const [source, setSource] = useState<Source>(kind === "link" ? "link" : "file");
  const [file, setFile] = useState<File | null>(null);
  const [url, setUrl] = useState("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const rootRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(() => {
    if (!busy) onCancel();
  }, rootRef);

  const hasContent = source === "file" ? file !== null : url.trim() !== "";
  const canSubmit =
    !busy && hasContent && (mode === "replace" || name.trim() !== "");

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;
    setBusy(true);
    setError("");
    let createdId = "";
    const err = await run(async () => {
      const content =
        source === "file" && file
          ? { attachmentId: await api.uploadAttachment(file, file.name) }
          : { url: url.trim() };
      if (mode === "replace") {
        await api.replaceSharedDoc(docId ?? "", content);
        return;
      }
      const fileKind: SharedDocKind = attachmentMimeForName(
        file?.name ?? "",
      ).startsWith("image/")
        ? "image"
        : "file";
      createdId = await api.createSharedDoc({
        kind: source === "link" ? "link" : fileKind,
        name: name.trim(),
        description: description.trim(),
        ...content,
      });
    });
    setBusy(false);
    if (err) {
      setError(err);
      return;
    }
    onDone(createdId);
  }

  return (
    <div ref={rootRef} className="confirm-modal" role="dialog" aria-modal="true">
      <form
        className="confirm-modal__box shared-docs__form"
        data-testid={mode === "create" ? "shared-doc-create-form" : "shared-doc-replace-form"}
        onSubmit={submit}
      >
        <div className="secrets__form-title">
          {mode === "create" ? d.createTitle : d.replaceTitle}
        </div>
        {mode === "create" && (
          <div className="shared-docs__source" role="radiogroup">
            {(["file", "link"] as Source[]).map((s) => (
              <label key={s} className="shared-docs__source-option">
                <input
                  type="radio"
                  name="shared-doc-source"
                  data-testid={`shared-doc-source-${s}`}
                  checked={source === s}
                  onChange={() => setSource(s)}
                />
                {s === "file" ? d.sourceFile : d.sourceLink}
              </label>
            ))}
          </div>
        )}
        {source === "file" ? (
          <label className="secrets__field">
            <span className="secrets__label">{d.fileLabel}</span>
            <input
              type="file"
              className="secrets__input"
              data-testid="shared-doc-file-input"
              accept={kind === "image" ? "image/*" : undefined}
              onChange={(e) => setFile(e.target.files?.[0] ?? null)}
            />
          </label>
        ) : (
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
        )}
        {mode === "create" && (
          <>
            <label className="secrets__field">
              <span className="secrets__label">{d.nameLabel}</span>
              <input
                className="secrets__input"
                data-testid="shared-doc-name-input"
                value={name}
                maxLength={128}
                onChange={(e) => setName(e.target.value)}
              />
            </label>
            <label className="secrets__field">
              <span className="secrets__label">{d.descriptionLabel}</span>
              <textarea
                className="secrets__input secrets__textarea"
                data-testid="shared-doc-description-input"
                value={description}
                maxLength={1024}
                onChange={(e) => setDescription(e.target.value)}
              />
            </label>
          </>
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
            {mode === "create" ? d.create : d.save}
          </button>
        </div>
      </form>
    </div>
  );
}
