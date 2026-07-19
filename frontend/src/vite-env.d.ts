/// <reference types="vite/client" />

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
