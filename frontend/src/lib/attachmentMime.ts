// The server's own extension table (go:embed'd there), so the mock and the
// composer type a filename exactly as the station does.
import mimeByExtension from "../../../server/ocserverd/attachment_mime_types.json";

const OCTET_STREAM = "application/octet-stream";
const TABLE: Record<string, string> = mimeByExtension;

export function attachmentMimeForName(filename: string): string {
  const name = filename.trim();
  const dot = name.lastIndexOf(".");
  if (dot < 0) return OCTET_STREAM;
  return TABLE[name.slice(dot + 1).toLowerCase()] ?? OCTET_STREAM;
}

/** A name for a clipboard image the browser handed over without one. Without
 * an extension the server would store it as a download, not an image. */
export function pastedImageName(type: string): string {
  const ext = Object.keys(TABLE).find((e) => TABLE[e] === type);
  return ext ? `pasted-image.${ext}` : "";
}
