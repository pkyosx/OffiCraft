var __assign = (this && this.__assign) || function () {
    __assign = Object.assign || function(t) {
        for (var s, i = 1, n = arguments.length; i < n; i++) {
            s = arguments[i];
            for (var p in s) if (Object.prototype.hasOwnProperty.call(s, p))
                t[p] = s[p];
        }
        return t;
    };
    return __assign.apply(this, arguments);
};
var __awaiter = (this && this.__awaiter) || function (thisArg, _arguments, P, generator) {
    function adopt(value) { return value instanceof P ? value : new P(function (resolve) { resolve(value); }); }
    return new (P || (P = Promise))(function (resolve, reject) {
        function fulfilled(value) { try { step(generator.next(value)); } catch (e) { reject(e); } }
        function rejected(value) { try { step(generator["throw"](value)); } catch (e) { reject(e); } }
        function step(result) { result.done ? resolve(result.value) : adopt(result.value).then(fulfilled, rejected); }
        step((generator = generator.apply(thisArg, _arguments || [])).next());
    });
};
var __generator = (this && this.__generator) || function (thisArg, body) {
    var _ = { label: 0, sent: function() { if (t[0] & 1) throw t[1]; return t[1]; }, trys: [], ops: [] }, f, y, t, g = Object.create((typeof Iterator === "function" ? Iterator : Object).prototype);
    return g.next = verb(0), g["throw"] = verb(1), g["return"] = verb(2), typeof Symbol === "function" && (g[Symbol.iterator] = function() { return this; }), g;
    function verb(n) { return function (v) { return step([n, v]); }; }
    function step(op) {
        if (f) throw new TypeError("Generator is already executing.");
        while (g && (g = 0, op[0] && (_ = 0)), _) try {
            if (f = 1, y && (t = op[0] & 2 ? y["return"] : op[0] ? y["throw"] || ((t = y["return"]) && t.call(y), 0) : y.next) && !(t = t.call(y, op[1])).done) return t;
            if (y = 0, t) op = [op[0] & 2, t.value];
            switch (op[0]) {
                case 0: case 1: t = op; break;
                case 4: _.label++; return { value: op[1], done: false };
                case 5: _.label++; y = op[1]; op = [0]; continue;
                case 7: op = _.ops.pop(); _.trys.pop(); continue;
                default:
                    if (!(t = _.trys, t = t.length > 0 && t[t.length - 1]) && (op[0] === 6 || op[0] === 2)) { _ = 0; continue; }
                    if (op[0] === 3 && (!t || (op[1] > t[0] && op[1] < t[3]))) { _.label = op[1]; break; }
                    if (op[0] === 6 && _.label < t[1]) { _.label = t[1]; t = op; break; }
                    if (t && _.label < t[2]) { _.label = t[2]; _.ops.push(op); break; }
                    if (t[2]) _.ops.pop();
                    _.trys.pop(); continue;
            }
            op = body.call(thisArg, _);
        } catch (e) { op = [6, e]; y = 0; } finally { f = t = 0; }
        if (op[0] & 5) throw op[1]; return { value: op[0] ? op[1] : void 0, done: true };
    }
};
/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { build as esbuild } from "esbuild";
import { fileURLToPath } from "node:url";
import { localTestWorkers } from "./local-test-workers";
import { NO_SEASONAL_SCHEDULE_ALIAS } from "./seasonal-test-alias";
// [T-1500] Inline the pre-React theme applier into index.html.
// It is bundled FROM src/paint/prePaint.ts — i.e. it imports the real
// validateThemeBundle and the real generated whitelists, so there is exactly
// ONE grammar and ONE whitelist in the tree. A second <script type="module">
// tag does NOT work: Vite folds it into the 659 kB main chunk, so the paint
// waits for the whole app to download (measured: still 1-2 office frames).
var PLACEHOLDER = "<!--oc-prepaint-->";
function inlinePrePaint() {
    var entry = fileURLToPath(new URL("./src/paint/prePaint.ts", import.meta.url));
    return {
        name: "oc-inline-prepaint",
        transformIndexHtml: function (html) {
            return __awaiter(this, void 0, void 0, function () {
                var out, code;
                return __generator(this, function (_a) {
                    switch (_a.label) {
                        case 0: return [4 /*yield*/, esbuild({
                                entryPoints: [entry],
                                bundle: true,
                                format: "iife",
                                target: "es2018",
                                minify: true,
                                write: false,
                            })];
                        case 1:
                            out = _a.sent();
                            code = out.outputFiles[0].text;
                            if (code.includes("</script")) {
                                throw new Error("oc-inline-prepaint: bundled code carries </script — refusing to inline");
                            }
                            if (!html.includes(PLACEHOLDER)) {
                                throw new Error("oc-inline-prepaint: ".concat(PLACEHOLDER, " missing from index.html"));
                            }
                            // NOTE: the replacement MUST be a function — a string replacement makes
                            // String.replace interpret $-sequences in the minified code ($&, $', …) and
                            // silently corrupts the inlined script (measured: SyntaxError at runtime,
                            // frame probe went 2/1 red).
                            return [2 /*return*/, html.replace(PLACEHOLDER, function () { return "<script>".concat(code, "</script>"); })];
                    }
                });
            });
        },
    };
}
// Only vitest (mode "test") reads the worker options. Resolving the cap for
// `vite build` or the dev server as well would let a malformed
// OC_LOCAL_TEST_WORKERS break the product build.
function testWorkerOptions(mode) {
    if (mode !== "test")
        return {};
    var workers = localTestWorkers();
    return workers === undefined ? {} : { maxWorkers: workers };
}
export default defineConfig(function (_a) {
    var mode = _a.mode;
    return ({
        plugins: [inlinePrePaint(), react()],
        server: {
            // api/seeds.ts imports the repo-root seeds/*.md (the single source of truth)
            // via `?raw`. Those files live one level above the Vite root (frontend/), so
            // the dev server's file-serving allow-list must include the repo root or the
            // browser request for the raw module 403s.
            fs: { allow: [".."] },
        },
        test: __assign(__assign({ environment: "jsdom", globals: true, setupFiles: ["./src/test/setup.ts"], 
            // CI runners are on UTC, where formatting in UTC instead of local time
            // looks correct; a fixed non-UTC zone makes that mistake fail everywhere.
            env: { TZ: "Asia/Taipei" }, alias: [NO_SEASONAL_SCHEDULE_ALIAS] }, testWorkerOptions(mode)), { 
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
            ] }),
    });
});
