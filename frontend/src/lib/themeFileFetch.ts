import type { ThemeBundle } from "./themeBundleCore";
import { themeFileRequest } from "./themeFileRequest";
import { reportStaleAsset } from "./staleBuild";

// Fetched rather than imported: a browser caches a failed dynamic import per
// URL and rejects every later import of it without a request, so a theme file
// loaded as a JS chunk could never be retried after one network failure.
export function themeFileLoaders(
  urls: Record<string, string>,
  request: (url: string) => Promise<Response> = themeFileRequest
): Record<string, () => Promise<ThemeBundle>> {
  return Object.fromEntries(
    Object.entries(urls).map(([path, url]) => [path, () => fetchThemeFile(request, url)])
  );
}

async function fetchThemeFile(
  request: (url: string) => Promise<Response>,
  url: string
): Promise<ThemeBundle> {
  const res = await request(url);
  if (res.status === 404) reportStaleAsset();
  if (!res.ok) throw new Error(`theme file ${url}: HTTP ${res.status}`);
  return (await res.json()) as ThemeBundle;
}
