/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
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
    ],
  },
});
