/// <reference types="vitest/config" />
import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import { build as esbuild } from "esbuild";
import { fileURLToPath } from "node:url";

// [T-1500] Inline the pre-React theme applier into index.html.
// It is bundled FROM src/paint/prePaint.ts — i.e. it imports the real
// validateThemeBundle and the real generated whitelists, so there is exactly
// ONE grammar and ONE whitelist in the tree. A second <script type="module">
// tag does NOT work: Vite folds it into the 659 kB main chunk, so the paint
// waits for the whole app to download (measured: still 1-2 office frames).
const PLACEHOLDER = "<!--oc-prepaint-->";

function inlinePrePaint(): Plugin {
  const entry = fileURLToPath(new URL("./src/paint/prePaint.ts", import.meta.url));
  return {
    name: "oc-inline-prepaint",
    async transformIndexHtml(html: string) {
      const out = await esbuild({
        entryPoints: [entry],
        bundle: true,
        format: "iife",
        target: "es2018",
        minify: true,
        write: false,
      });
      const code = out.outputFiles[0].text;
      if (code.includes("</script")) {
        throw new Error("oc-inline-prepaint: bundled code carries </script — refusing to inline");
      }
      if (!html.includes(PLACEHOLDER)) {
        throw new Error(`oc-inline-prepaint: ${PLACEHOLDER} missing from index.html`);
      }
      // NOTE: the replacement MUST be a function — a string replacement makes
      // String.replace interpret $-sequences in the minified code ($&, $', …) and
      // silently corrupts the inlined script (measured: SyntaxError at runtime,
      // frame probe went 2/1 red).
      return html.replace(PLACEHOLDER, () => `<script>${code}</script>`);
    },
  };
}

export default defineConfig({
  plugins: [inlinePrePaint(), react()],
  server: {
    // api/seeds.ts imports the repo-root seeds/*.md (the single source of truth)
    // via `?raw`. Those files live one level above the Vite root (frontend/), so
    // the dev server's file-serving allow-list must include the repo root or the
    // browser request for the raw module 403s.
    fs: { allow: [".."] },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    // T-187c: the Playwright Component-Testing visual guards live in
    // visual-guards/*.ct.spec.tsx and run in a REAL browser (see
    // playwright-ct.config.ts). Vitest's default include glob
    // (**/*.{test,spec}.tsx) would otherwise sweep them into the jsdom suite,
    // where `import "@playwright/experimental-ct-react"` throws at collect
    // time and reddens `vitest run`. The two runners must own disjoint globs:
    // vitest owns *.test.tsx, Playwright owns *.ct.spec.tsx. Excluding the
    // whole visual-guards/ dir keeps the story fixtures out too.
    exclude: [
      "**/node_modules/**",
      "**/dist/**",
      "**/.cache/**",
      "**/{karma,rollup,webpack,vite,vitest,jest,ava,babel,nyc,cypress,tsup,build,eslint,prettier}.config.*",
      "visual-guards/**",
      "**/*.ct.spec.tsx",
      // T-1500: same disjoint-globs discipline for the paint guards, which load
      // the real dist/ in a real Chromium (see playwright-paint.config.ts).
      // Vitest's default glob would otherwise sweep *.paint.spec.ts in and throw
      // at collect time on `import "@playwright/test"`.
      // Narrower than `paint-guards/**`: the browser specs in there must stay
      // out, but the plain-node helpers beside them (freePort.ts) have jsdom
      // tests that belong in this suite.
      "paint-guards/**/*.spec.ts",
      "**/*.paint.spec.ts",
    ],
  },
});
