// api/errorCodes.ts — the mock seam's ONLY source of an error-envelope `code`.
//
// 🔴 WHY THIS EXISTS (T-fd2c): the mock adapter used to hand-write the third
// `new ApiError(...)` argument at every refusal, and seven of those said
// `bad_request` — a code the real server CANNOT emit. `errorCodeForStatus`
// (server/ocserverd/server.go) answers `validation_error` for 400/422 and
// `client_error` for any unmapped 4xx; `bad_request` appears nowhere in
// server/, conformance/ or spec/openapi.json. Any component test that branched
// on the CODE rather than the status was therefore agreeing with a server that
// does not exist — a FALSE ORACLE, green forever, catching nothing.
//
// The structural fix is that the mock can no longer write a code at all: it
// imports `mockApiError` instead of `ApiError`, and the code is DERIVED from
// the status through the shared spec table.
//
// 🔴 The narrowing stops at the mock. `ApiError`'s own `code` stays a free
// string because http.ts fills it from a LIVE server response — a code the
// server adds tomorrow must not fail to compile in the browser today.

import { ApiError } from "./errors";
import errorCodeSpec from "../../../docs/design/api-error-envelope.codes.json";

const BY_STATUS: Record<string, string> = errorCodeSpec.by_status;

/** The closed code vocabulary, as the spec table declares it. */
export const ERROR_CODE_VOCABULARY: ReadonlySet<string> = new Set([
  ...Object.values(BY_STATUS),
  errorCodeSpec.fallback_5xx,
  errorCodeSpec.fallback_other,
]);

/** The status → envelope code map, read from `docs/design/api-error-envelope.codes.json` — the same
 * table `errorCodeForStatus` is pinned against on the Go side and
 * `CODE_BY_STATUS` is pinned against in conformance. Unmapped statuses fall
 * into the same two honest buckets the server uses. */
export function codeForStatus(status: number): string {
  const mapped = BY_STATUS[String(status)];
  if (mapped !== undefined) return mapped;
  return status >= 500 ? errorCodeSpec.fallback_5xx : errorCodeSpec.fallback_other;
}

/** The ApiError the MOCK adapter throws: same class the http client throws, but
 * with the code derived rather than typed. `message` keeps the historical
 * `http <status> for <call>` log format verbatim. */
export function mockApiError(
  message: string,
  status: number,
  serverMessage: string
): ApiError {
  return new ApiError(message, status, codeForStatus(status), serverMessage);
}
