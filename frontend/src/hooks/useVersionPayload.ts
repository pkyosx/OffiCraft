// Reading ONE version of a deliverable (a task artifact or a 檔案庫 document)
// for the version readers: what its bytes are, and their text when they are
// text. Both readers and both sides of a diff read through here, so neither
// side can be measured more generously than the other.
//
// WHETHER BYTES ARE TEXT IS ASKED OF THE RESPONSE, not of the row: the row's
// mime and the bytes' content type are two statements and only the second is
// the one being read, and a live row's mime is never borrowed for a version.
// A response that is not text is cancelled unread.
//
// 🔴 WITH ONE FALLBACK: a response whose mime is neither text nor image is read
// as text when a NAME ends in a known text extension (the closed list in
// lib/contentKind). `application/octet-stream` says the type is unknown, not
// that the bytes are binary — agent uploads of reports and logs arrive that
// way. Two names are consulted: the caller's (a version's own `filename`, or a
// live artifact's author-chosen title, which usually has no extension) and the
// one the response's own `Content-Disposition` carries, which is the only place
// the live side can still learn its blob's real name.

import { useEffect, useState } from "react";
import { authedAttachmentUrl } from "../api/http";
import { contentKindOf, isTextKind, textKindForName, type ContentKind } from "../lib/contentKind";

/** `opaque` is the honest answer for a file the reader cannot draw or compare —
 * never collapsed into "empty", which is a different and false statement. */
export type ArtifactPayload =
  | { state: "loading" }
  | { state: "text"; text: string; kind: ContentKind; url: string }
  | { state: "image"; src: string }
  | { state: "opaque"; mime: string; url: string }
  | { state: "link"; url: string }
  | { state: "gone" }
  | { state: "error" };

/** The blob's REAL filename, out of the download response's own
 * `Content-Disposition`. `filename*` wins when both are present — it is the one
 * that survives non-ASCII. The server percent-encodes it with Go's
 * `url.QueryEscape`, which writes a space as `+`; that is cosmetic for the one
 * question asked of this value (its extension), so it is left alone. Returns
 * "" when the header is absent or unparseable — never a made-up name. */
export function filenameFromContentDisposition(header: string | null): string {
  if (!header) return "";
  const ext = /;\s*filename\*\s*=\s*[^']*'[^']*'([^;]+)/i.exec(header);
  if (ext?.[1]) {
    const raw = ext[1].trim();
    try {
      return decodeURIComponent(raw);
    } catch {
      return raw;
    }
  }
  const quoted = /;\s*filename\s*=\s*"((?:[^"\\]|\\.)*)"/i.exec(header);
  if (quoted?.[1] !== undefined) return quoted[1].replace(/\\(.)/g, "$1");
  const bare = /;\s*filename\s*=\s*([^;]+)/i.exec(header);
  return bare?.[1] ? bare[1].trim() : "";
}

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
  const mime = (res.headers.get("content-type") ?? "").split(";")[0]!.trim().toLowerCase();
  if (mime.startsWith("image/")) {
    await res.body?.cancel().catch(() => {});
    return { state: "image", src: authedAttachmentUrl(url) };
  }
  const served = filenameFromContentDisposition(res.headers.get("content-disposition"));
  const typeName = textKindForName(name) !== null ? name : served;
  const named = textKindForName(name) ?? textKindForName(served);
  let contentKind = contentKindOf(mime, typeName);
  if (!isTextKind(contentKind) && contentKind !== "html" && named !== null) contentKind = named;
  if (isTextKind(contentKind) || contentKind === "html") {
    return { state: "text", text: await res.text(), kind: contentKind, url };
  }
  await res.body?.cancel().catch(() => {});
  return { state: "opaque", mime, url };
}

export function useVersionPayload(
  kind: "file" | "image" | "link" | undefined,
  url: string | undefined,
  name: string,
  warnAs: string,
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
        console.warn(`${warnAs}: content read failed`, e);
      });
    return () => {
      alive = false;
    };
  }, [kind, url, name, warnAs]);
  return payload;
}
