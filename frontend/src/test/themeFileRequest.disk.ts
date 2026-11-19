import { readFileSync } from "node:fs";

const files = new Map<string, string>();

// Under vitest a theme file's `?url` is the dev server's `/@fs/<absolute path>`,
// and there is no dev server to answer it: the file is read from disk instead.
// ⚠️ Answered within microtasks, like the dynamic import these loaders used to
// be: a real Response body (or an async read) settles a macrotask later, which
// suites that flush with one setTimeout(0) do not wait for.
export async function themeFileRequest(url: string): Promise<Response> {
  const path = new URL(url, "http://localhost/").pathname;
  if (!path.startsWith("/@fs/")) throw new Error(`not a dev-server file URL: ${url}`);
  const file = decodeURIComponent(path.slice("/@fs".length));
  if (!files.has(file)) files.set(file, readFileSync(file, "utf8"));
  const text = files.get(file)!;
  return { ok: true, status: 200, json: async () => JSON.parse(text) } as Response;
}
