/** The request a theme file's loader makes. Its own module so the test runners
 * can alias it to a disk read (theme-file-request-alias.ts), leaving tests that
 * stub the global fetch to see only the requests they mean to. */
export function themeFileRequest(url: string): Promise<Response> {
  return fetch(url);
}
