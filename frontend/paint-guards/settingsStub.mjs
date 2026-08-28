// settingsStub.mjs — T-1500 gate 4c. The server half of the zero-flash guard.
//
// WHY THIS EXISTS instead of `vite preview`:
//
// The flash this ticket fixes is the gap between first paint and the moment the
// login reconcile answers. Reproducing it needs a server that (a) actually knows
// the owner's custom theme so the reconcile CONFIRMS the cached picture instead
// of deleting it, and (b) answers over the network so CDP throttling delays it.
//
// [T-83ef] "the reconcile" is no longer ONE request. Themes left settings: the
// provider now does Promise.all([GET /api/settings, GET /api/themes]) and then
// fetches the ACTIVE theme's bundle from GET /api/themes/{id}. `custom_themes`
// does not exist on either face of /api/settings any more. So this stub serves
// the theme resource as well, and the two modes below are expressed in terms of
// it.
//
// `vite preview` + the default build gives neither: the shipped-by-default mock
// adapter answers the reconcile from memory in ~0 ms and its theme set does not
// contain the guard's theme, so reconcile finds the cached theme unknown and
// calls writePaint(null) — which REMOVES the record. Measured on the real build:
// with no auth token the frame probe reads BAD_FRAMES=0 (reconcile never runs,
// because it is gated on hasToken()); add a token and the SAME build reads
// BAD_FRAMES=231/233/249. A guard that green/reds on whether a token happens to
// be in localStorage is not measuring the product.
//
// So the guard builds with VITE_USE_MOCK=false — which is what bin/build ships —
// and points the app at this stub. Every frame number it produces is from the
// authenticated path, against a server that agrees the theme exists.
//
// Usage:
//   node settingsStub.mjs --dist dist --mode ok [--port N] [--delay 400]
//
// --port is OPTIONAL and normally absent: without it the OS picks a port nobody
// is using and this process prints the one it got. playwright-paint.config.ts
// does pass --port, with a port it allocated the same way — it has to, because
// Playwright needs the URL before the server exists. Pinning a literal port
// here is what used to make two working copies of this repo collide.
//
// Modes (the semantics are UNCHANGED; only the wire that carries them moved):
//   ok            — the server KNOWS the theme (the happy path this ticket is
//                   about): GET /api/themes LISTS it, GET /api/themes/{id}
//                   returns the full bundle, and settings' display_theme points
//                   at it.
//   unknown-theme — the server knows NO themes: GET /api/themes is `[]`,
//                   display_theme is "", and GET /api/themes/{id} is a 404. The
//                   cached picture is legitimately stale and MUST be dropped.
//                   Not a failure mode — documented behaviour, asserted separately.
//
// --delay applies to EVERY endpoint the reconcile touches (settings, the theme
// list AND the single-bundle read), not just settings. Delaying only settings
// would leave the other legs of the same reconcile answering instantly, and the
// guard would measure a flash window shorter than the real one.

import { createServer } from "node:http";
import { readFile, stat } from "node:fs/promises";
import { readFileSync } from "node:fs";
import { extname, join, normalize, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = fileURLToPath(new URL(".", import.meta.url));

function arg(name, fallback) {
  const i = process.argv.indexOf(`--${name}`);
  return i === -1 ? fallback : process.argv[i + 1];
}

// 0 = let the OS choose. See the --port note above; never default to a literal.
const PORT = Number(arg("port", "0"));
const DIST = resolve(HERE, "..", arg("dist", "dist"));
const MODE = arg("mode", "ok");
const DELAY = Number(arg("delay", "0"));

// The ONE source of truth for the server's theme — the same JSON the jsdom suite
// and the Playwright specs read, so the three layers cannot disagree about what
// the server said. (Plain Node here: it cannot import the TypeScript module.)
const SERVER_THEME = JSON.parse(
  readFileSync(resolve(HERE, "..", "src/lib/paintFixtures.theme.json"), "utf8")
);

/** Whether this server KNOWS the owner's theme. The ONE switch both faces read,
 * so `/api/themes` and `display_theme` can never disagree about which mode this
 * process is in. */
const KNOWS_THEME = MODE !== "unknown-theme";

/** GET /api/settings → SettingsDTO. Only the fields the cockpit reads are set to
 * anything interesting; the rest are the shipped defaults so no other panel
 * renders an error state that could add a page error the guard would blame on
 * the paint path.
 *
 * [T-83ef] `custom_themes` is NOT here, because it is not on the real SettingsDTO
 * any more — themes are their own resource. `display_theme` stays: settings still
 * owns WHICH theme is active. */
function settingsDTO() {
  const known = KNOWS_THEME;
  return {
    owner_token_ttl: 86400,
    agent_token_ttl: 604800,
    handover_pct: 50,
    codex_compaction_threshold: 3,
    monitoring_refresh_seconds: 5,
    outsource_max_parallel: 3,
    doc_cap_chars_duty: 1000,
    doc_cap_chars_insight: 15000,
    doc_cap_chars_learning: 15000,
    doc_cap_chars_manual_sop: 15000,
    doc_cap_chars_manual_learnings: 15000,
    updater_receive_beta: false,
    updater_auto_update: false,
    org_name: "",
    owner_name: "",
    push_contact_email: "",
    display_theme: known ? SERVER_THEME.id : "",
    display_language: "zh",
    display_wide: false,
    // The real settingsDTO carries no `omitempty`, so this key is ALWAYS on the
    // wire — null once onboarding has finished, which is every installation the
    // owner reloads. Absent and null map to the same `null` in the FE mapper, so
    // this changes no behaviour; it makes the stub's key set byte-equal to the
    // server's, which is what stops "the stub drifted" from being a live theory
    // every time this guard goes red.
    onboarding: null,
  };
}

/** GET /api/themes → ThemeListItemDTO[] — id + name ONLY, never the bundle.
 * A stub that returned whole bundles here would let a regression that reads
 * `colors` off a list row keep the guard green while production paints nothing.
 *
 * This is where the two modes now live: `ok` lists the theme, `unknown-theme`
 * lists nothing (which is what makes the cached picture legitimately stale). */
function themeListDTO() {
  return KNOWS_THEME ? [{ id: SERVER_THEME.id, name: SERVER_THEME.name }] : [];
}

/** The unified 404 envelope. NEVER 401 — a 401 clears the token and bounces the
 * app to the login wall, which would unmount the very page the guard samples. */
function notFound(res, message) {
  sendJSON(res, 404, { error: { code: "not_found", message } });
}

const MIME = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".webmanifest": "application/manifest+json",
  ".woff2": "font/woff2",
  ".ico": "image/x-icon",
};

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function sendJSON(res, status, body) {
  const buf = Buffer.from(JSON.stringify(body));
  res.writeHead(status, {
    "content-type": "application/json; charset=utf-8",
    "content-length": buf.length,
    // No caching anywhere: a cached settings response would silently remove the
    // very round trip this guard is built to measure.
    "cache-control": "no-store",
  });
  res.end(buf);
}

async function serveFile(res, absPath) {
  const body = await readFile(absPath);
  res.writeHead(200, {
    "content-type": MIME[extname(absPath)] ?? "application/octet-stream",
    "content-length": body.length,
    "cache-control": "no-store",
  });
  res.end(body);
}

const server = createServer(async (req, res) => {
  const url = new URL(req.url, `http://localhost:${boundPort}`);
  const path = url.pathname;

  // ── the reconcile's three legs. EVERY one of them sleeps for --delay ────────
  // The provider fires GET /api/settings and GET /api/themes together and then
  // GET /api/themes/{active}. The flash under measurement is the whole of that
  // wait, so a leg that answers instantly shortens the window the guard can see.

  if (path === "/api/settings" && req.method === "GET") {
    if (DELAY > 0) await sleep(DELAY);
    sendJSON(res, 200, settingsDTO());
    return;
  }

  if (path === "/api/themes" && req.method === "GET") {
    if (DELAY > 0) await sleep(DELAY);
    sendJSON(res, 200, themeListDTO());
    return;
  }

  // /api/themes/{theme_id} — the single-bundle read, plus the write face so the
  // cockpit's own editor calls do not land on the catch-all 404 and surface as a
  // page error the guard would blame on the paint path.
  const themeId = path.startsWith("/api/themes/")
    ? decodeURIComponent(path.slice("/api/themes/".length))
    : null;
  if (themeId !== null && !themeId.includes("/")) {
    const isKnown = KNOWS_THEME && themeId === SERVER_THEME.id;
    if (DELAY > 0) await sleep(DELAY);
    if (req.method === "GET") {
      if (!isKnown) {
        notFound(res, `paint-guard stub: theme ${themeId} not found`);
        return;
      }
      // The FULL bundle — colours, fonts, canvas image and canvas mode. This is
      // the ONLY place the guard can get the server's copy of the picture now,
      // and it is the same JSON the jsdom suite deep-compares against
      // paintFixtures.VALID_RICH_BUNDLE, so the layers cannot drift.
      sendJSON(res, 200, SERVER_THEME);
      return;
    }
    if (req.method === "PUT") {
      // A RECEIPT, never the bundle echoed back (server parity: echoing would
      // resend the embedded images, which is what the theme resource exists to
      // stop doing).
      sendJSON(res, 200, {
        id: themeId,
        created: !isKnown,
        order_idx: 0,
        updated_at: Math.floor(Date.now() / 1000),
      });
      return;
    }
    if (req.method === "DELETE") {
      if (!isKnown) {
        notFound(res, `paint-guard stub: theme ${themeId} not found`);
        return;
      }
      // Deleting the ACTIVE theme resets display_theme in the same request; this
      // stub's active id IS the server theme, so the reset flag is that identity.
      sendJSON(res, 200, {
        id: themeId,
        deleted: true,
        display_theme_reset: themeId === SERVER_THEME.id,
      });
      return;
    }
  }

  if (path.startsWith("/api/")) {
    // Everything else the cockpit pokes at is out of scope. 404 with the unified
    // error envelope so the client's own error mapping runs (an ApiError the
    // caller catches) rather than an unhandled shape. NEVER 401: a 401 clears the
    // token and bounces the app to the login wall, which would unmount the very
    // page the guard is sampling.
    sendJSON(res, 404, {
      error: { code: "not_found", message: `paint-guard stub: ${path} not stubbed` },
    });
    return;
  }

  // Static, with an SPA fallback to index.html.
  const rel = normalize(path).replace(/^(\.\.[/\\])+/, "").replace(/^\/+/, "");
  const candidate = join(DIST, rel);
  try {
    if (rel && (await stat(candidate)).isFile()) {
      await serveFile(res, candidate);
      return;
    }
  } catch {
    /* fall through to index.html */
  }
  try {
    await serveFile(res, join(DIST, "index.html"));
  } catch {
    res.writeHead(500, { "content-type": "text/plain" });
    res.end(`paint-guard stub: no index.html under ${DIST} — run the build first`);
  }
});

/** The port actually bound, which is only the same as PORT when one was pinned;
 * with --port absent the OS chooses and this is where the answer lands. */
let boundPort = PORT;

// A failed listen must SAY WHAT FAILED. Without this handler node re-throws the
// 'error' event as an unhandled exception whose stack points into this file, and
// Playwright reports it as "Process from config.webServer was not able to start"
// — which reads exactly like the stub itself being broken, and sends whoever hit
// it off to debug code that is fine. The message below names the actual fault
// and what to do about it.
server.on("error", (err) => {
  const detail =
    err.code === "EADDRINUSE"
      ? `port ${PORT} is ALREADY IN USE by another process — most often a second ` +
        `copy of this stub, i.e. another working copy of this repo running the ` +
        `paint guards at the same time. This stub is NOT broken. Drop --port to ` +
        `let the OS pick a free one, which is what playwright-paint.config.ts does.`
      : err.code === "EACCES"
        ? `this user may not bind port ${PORT} (ports below 1024 need root). Drop ` +
          `--port to let the OS pick a high one.`
        : `${err.code ?? "unknown error"} — ${err.message}`;
  console.error(`[paint-guard stub] FAILED TO LISTEN: ${detail}`);
  process.exit(1);
});

server.listen(PORT, () => {
  boundPort = server.address().port;
  console.log(
    `[paint-guard stub] :${boundPort} dist=${DIST} mode=${MODE} reconcileDelay=${DELAY}ms ` +
      `(settings + themes + themes/{id}) knowsTheme=${KNOWS_THEME} theme=${SERVER_THEME.id}`
  );
});
