// Unified build-version label (T-e9d1 round 3, owner final):
// `v<yymmdd>-<hhmm>-<shortsha>` (e.g. v260716-0930-14417c9), composed purely
// on the frontend from /api/version's `git_sha` + `git_time` — the date/time
// are the running binary's git COMMIT time, the sha its short git sha. No
// build/server involvement: display-only string assembly.
//
// The date/time digits are read literally from the ISO-8601 string (the
// commit's own recorded wall clock + offset), NOT converted to the viewer's
// local timezone — the same build must label identically on every machine.

/** First 7 chars of a git sha (already-short shas pass through unchanged). */
function shortSha(gitSha: string): string {
  return gitSha.slice(0, 7);
}

/**
 * Compose the unified version label from the build identity. When the commit
 * time is unavailable (null / unparsable — release tarball without git), the
 * label honestly degrades to the short sha alone; a date is never fabricated.
 */
export function formatBuildVersion(
  gitSha: string,
  gitTime: string | null
): string {
  const sha = shortSha(gitSha);
  if (!sha) return "";
  if (!gitTime) return sha;
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(gitTime);
  if (!m) return sha;
  const [, yyyy, mm, dd, hh, min] = m;
  return `v${yyyy.slice(2)}${mm}${dd}-${hh}${min}-${sha}`;
}
