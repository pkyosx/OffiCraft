// api/errors.ts — the typed API error both adapters throw (mock ↔ http parity).
//
// The FE face of the server's unified error envelope
// `{"error":{"code","message"}}` (docs/design/api-error-envelope.md). Lives in
// its own seam-neutral module so the mock adapter can throw the SAME class the
// real http client throws without importing the http layer.

/** The error every non-2xx API call rejects with. `status` is the HTTP
 * status; `code` (machine-readable snake_case) and `serverMessage` (human
 * text) come from the envelope body — HONEST-empty `""` when the body did not
 * carry the envelope (a proxy error page, a dropped body). `message` keeps
 * the historical `http <status> for <METHOD> <path>` format verbatim (pinned
 * by client.test.ts) so logs and legacy string-matching stay stable. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly serverMessage: string;

  constructor(message: string, status: number, code: string, serverMessage: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.serverMessage = serverMessage;
  }
}

/** True when `e` is an API rejection with HTTP status `status` — the one way
 * callers branch on an error's status (deleteRole's 409 有成員在線上 case).
 * Falls back to the historical `http <status>` message regex for a plain
 * Error thrown outside the adapters (defense in depth, not a contract). */
export function isHttpStatus(e: unknown, status: number): boolean {
  if (e instanceof ApiError) return e.status === status;
  return e instanceof Error && new RegExp(`\\bhttp ${status}\\b`).test(e.message);
}
