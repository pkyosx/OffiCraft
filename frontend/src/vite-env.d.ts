/// <reference types="vite/client" />

// Raw-text imports of the repo-root seed files — the single source of truth for
// the mock adapter (see api/seeds.ts). vite/client also declares `*?raw`; this
// narrower declaration makes the seed wiring self-documenting and independent
// of that.
declare module "*.md?raw" {
  const src: string;
  export default src;
}

interface ImportMetaEnv {
  /** "false" swaps the api client to the real backend (httpApi); anything else
   * (unset / "true") keeps the mock adapter. See api/index.ts. */
  readonly VITE_USE_MOCK?: string;
  /** Build-time owner-JWT injection fallback for gated routes (localStorage
   * `oc_token` takes precedence). See api/http.ts. */
  readonly VITE_OC_TOKEN?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
