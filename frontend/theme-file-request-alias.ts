import { fileURLToPath } from "node:url";

// vitest only: the theme loaders read their files from disk instead of through
// the global fetch, which many suites stub or forbid for their own requests.
export const THEME_FILE_REQUEST_ALIAS = {
  find: /^\.\/themeFileRequest$/,
  replacement: fileURLToPath(new URL("./src/test/themeFileRequest.disk.ts", import.meta.url)),
};
