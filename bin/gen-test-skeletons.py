#!/usr/bin/env python3
"""Generate mirror-image Go test-file skeletons for a Go repo.

For every product file (a .go that is not _test.go and is not machine
generated) the generator emits <name>_test.go under the output directory,
mirroring the repo-relative path, with one top-level Test function per
function that is worth testing and a t.Skip("TODO: ...") body.

Route handlers listed in server/ocserverd/routes.go additionally get the
five endpoint-layer scenario frames as t.Run subtests.

The generator NEVER writes into the repo; it only reads it.
"""

from __future__ import annotations

import argparse
import os
import re
import sys
from dataclasses import dataclass, field

# ---------------------------------------------------------------- go scanning

GENERATED_MARKER = re.compile(r"^//\s*Code generated .* DO NOT EDIT\.$")
PACKAGE_RE = re.compile(r"^package\s+(\w+)")
# top-level func: column 0, optional receiver.
FUNC_RE = re.compile(r"^func\s+(?:\(\s*\w+\s+\*?([\w.\[\]]+)\s*\)\s*)?(\w+)\s*\(")


def strip_code(line: str) -> str:
    """Blank out string/rune literals and line comments so brace counting is safe."""
    out = []
    i = 0
    n = len(line)
    while i < n:
        c = line[i]
        if c == "/" and i + 1 < n and line[i + 1] == "/":
            break
        if c in ('"', "'", "`"):
            quote = c
            i += 1
            while i < n:
                if quote != "`" and line[i] == "\\":
                    i += 2
                    continue
                if line[i] == quote:
                    i += 1
                    break
                i += 1
            out.append('""')
            continue
        out.append(c)
        i += 1
    return "".join(out)


@dataclass
class GoFunc:
    name: str
    recv: str | None
    doc: list[str]
    body: list[str]
    line: int


@dataclass
class GoFile:
    path: str
    package: str
    generated_by: str | None
    funcs: list[GoFunc] = field(default_factory=list)


def parse_go_file(path: str) -> GoFile:
    with open(path, encoding="utf-8", errors="replace") as fh:
        lines = fh.read().splitlines()

    package = ""
    generated_by = None
    pkg_idx = None
    for idx, line in enumerate(lines):
        m = PACKAGE_RE.match(line)
        if m:
            package = m.group(1)
            pkg_idx = idx
            break
    # The Go convention puts the marker anywhere in the header, i.e. before the
    # package clause (gofmt/`go generate` reads it that way).
    header = lines[: pkg_idx if pkg_idx is not None else len(lines)]
    for line in header:
        if GENERATED_MARKER.match(line.strip()):
            generated_by = line.strip()
            break

    gf = GoFile(path=path, package=package, generated_by=generated_by)
    if generated_by:
        return gf

    in_block_comment = False
    doc: list[str] = []
    i = pkg_idx + 1 if pkg_idx is not None else 0
    while i < len(lines):
        raw = lines[i]
        stripped = raw.strip()
        if in_block_comment:
            if "*/" in stripped:
                in_block_comment = False
            i += 1
            continue
        if stripped.startswith("/*"):
            if "*/" not in stripped[2:]:
                in_block_comment = True
            i += 1
            continue
        if raw.startswith("//"):
            doc.append(stripped[2:].strip())
            i += 1
            continue
        m = FUNC_RE.match(raw)
        if not m:
            if stripped:
                doc = []
            i += 1
            continue

        recv, name = m.group(1), m.group(2)
        start = i
        depth = 0
        seen_brace = False
        body: list[str] = []
        while i < len(lines):
            code = strip_code(lines[i])
            if seen_brace:
                body.append(lines[i])
            for pos, ch in enumerate(code):
                if ch == "{":
                    depth += 1
                    if not seen_brace:
                        seen_brace = True
                        # a one-line func keeps its whole body on the signature line
                        body.append(lines[i][pos + 1 :])
                elif ch == "}":
                    depth -= 1
            i += 1
            if seen_brace and depth <= 0:
                break
        if body:
            # drop everything from the body's closing brace onward
            tail = body[-1]
            cut = strip_code(tail).rfind("}")
            body[-1] = tail[:cut] if cut >= 0 else tail
        gf.funcs.append(GoFunc(name=name, recv=recv, doc=doc, body=body, line=start + 1))
        doc = []
    return gf


# ------------------------------------------------------------ testability

SKIP_NAMES = {"init", "main"}


def body_statements(body: list[str]) -> list[str]:
    out = []
    for line in body:
        code = strip_code(line).strip()
        if not code:
            continue
        out.append(code)
    return out


def why_skip(fn: GoFunc) -> str | None:
    """Return a reason string when this func should get no Test, else None."""
    if fn.name in SKIP_NAMES and fn.recv is None:
        return "go entry point (init/main): no callable, observable result"
    stmts = body_statements(fn.body)
    if not stmts:
        return "empty body"
    if len(stmts) == 1 and stmts[0].startswith("return"):
        return "single-return helper: nothing to observe beyond the expression"
    return None


SENTENCE_END = re.compile(r"(?<=[.!?])\s")


def todo_sentence(fn: GoFunc) -> str:
    """First doc sentence, normalised; the honest fallback when there is none."""
    doc = [d for d in fn.doc if d]
    if doc:
        text = " ".join(doc)
        # Go doc comments open with the identifier; keep the sentence readable.
        parts = SENTENCE_END.split(text, 1)
        sentence = parts[0].strip()
        sentence = re.sub(r"\s+", " ", sentence).strip()
        # t.Skip is Print-like, so vet's printf analysis reads any %<letter> in
        # the message as a format directive and fails the build. Separating the
        # two characters keeps the doc text readable and the pattern inert.
        sentence = re.sub(r"%([A-Za-z])", r"% \1", sentence)
        if sentence and len(sentence) > len(fn.name) + 2:
            return "TODO: " + sentence
    return "TODO: 需要人工判斷這個函式的可觀察結果是什麼"


def go_string(s: str) -> str:
    return s.replace("\\", "\\\\").replace('"', '\\"')


# ------------------------------------------------------------- route table

PRINCIPAL_LADDER = ["machine", "agent", "admin_agent", "owner"]
CONST_TO_VALUE = {
    "authPublic": "public",
    "authGated": "gated",
    "principalOwner": "owner",
    "principalAdminAgent": "admin_agent",
    "principalAgent": "agent",
    "principalMachine": "machine",
    "requiresPublic": "public",
}


@dataclass
class Route:
    method: str
    path: str
    handler: str
    auth: str
    requires: str


def parse_routes(repo: str) -> dict[str, list[Route]]:
    path = os.path.join(repo, "server", "ocserverd", "routes.go")
    if not os.path.exists(path):
        return {}
    with open(path, encoding="utf-8", errors="replace") as fh:
        src = fh.read()
    routes: dict[str, list[Route]] = {}
    # Rows are brace-delimited RouteSpec literals; split on the handler field.
    for block in re.split(r"\n\t\t\{", src)[1:]:
        block = block.split("\n\t\t}")[0]
        h = re.search(r"Handler:\s*(?:\w+\.)?(\w+)", block)
        if not h:
            continue
        m = re.search(r'Method:\s*"([^"]+)"', block)
        p = re.search(r'Path:\s*"([^"]+)"', block)
        a = re.search(r"Auth:\s*(\w+)", block)
        rq = re.search(r"Requires:\s*(\w+)", block)
        routes.setdefault(h.group(1), []).append(
            Route(
                method=m.group(1) if m else "?",
                path=p.group(1) if p else "?",
                handler=h.group(1),
                auth=CONST_TO_VALUE.get(a.group(1), a.group(1)) if a else "?",
                requires=CONST_TO_VALUE.get(rq.group(1), rq.group(1)) if rq else "?",
            )
        )
    return routes


def endpoint_scenarios(routes: list[Route]) -> list[str]:
    """The five endpoint-layer scenario frames, as full-sentence t.Run names."""
    names: list[str] = []
    for r in routes:
        where = f"{r.method} {r.path}"
        params = re.findall(r"\{(\w+)\}", r.path)

        # (1) happy path
        names.append(f"a well-formed {where} answers 200")

        # (2) authentication - only where the row is gated
        if r.auth == "gated":
            names.append(f"a {where} request without a token answers 401")

        # (3) authorization - only where the row admits above the bottom rung
        if r.requires in PRINCIPAL_LADDER:
            idx = PRINCIPAL_LADDER.index(r.requires)
            if idx > 0:
                below = PRINCIPAL_LADDER[idx - 1]
                names.append(
                    f"an authenticated {below} identity answers 403 "
                    f"because this row requires {r.requires}"
                )

        # (4) routing and parameter binding
        if params:
            bound = ", ".join(params)
            names.append(
                f"a request to {where} reaches this handler with {bound} bound from the path"
            )
        else:
            names.append(f"a request to {where} reaches this handler and no other row")

        # (5) status-code mapping and request-level limits
        names.append(
            f"a {where} request the wire layer rejects (malformed body, wrong content type, "
            f"over the size cap) answers a 4xx without reaching the domain"
        )
    return names


# --------------------------------------------------------------- generation


def existing_test_names(repo: str) -> set[str]:
    names: set[str] = set()
    for root, dirs, files in os.walk(repo):
        dirs[:] = [d for d in dirs if d != ".git"]
        for f in files:
            if f.endswith("_test.go"):
                with open(os.path.join(root, f), encoding="utf-8", errors="replace") as fh:
                    names |= set(re.findall(r"^func\s+(Test\w*)\s*\(", fh.read(), re.M))
    return names


def product_files(repo: str, scope: str) -> list[str]:
    out = []
    for root, dirs, files in os.walk(repo):
        dirs[:] = [d for d in dirs if d not in (".git", "node_modules", "dist")]
        for f in sorted(files):
            if not f.endswith(".go") or f.endswith("_test.go"):
                continue
            p = os.path.join(root, f)
            rel = os.path.relpath(p, repo)
            if scope == "server-only" and not rel.startswith("server" + os.sep):
                continue
            out.append(p)
    return sorted(out)


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--repo", required=True, help="path to the Go repo (read-only)")
    ap.add_argument("--out", required=True, help="output directory for the skeletons")
    ap.add_argument("--scope", choices=["all", "server-only"], default="all")
    ap.add_argument(
        "--allow-existing-name-collisions",
        action="store_true",
        help="emit Test funcs whose name a repo test already uses (default: skip them)",
    )
    args = ap.parse_args(argv)

    repo = os.path.abspath(args.repo)
    out = os.path.abspath(args.out)
    if out.startswith(repo + os.sep) or out == repo:
        print("refusing to write inside the repo", file=sys.stderr)
        return 2
    os.makedirs(out, exist_ok=True)

    routes = parse_routes(repo)
    taken = set() if args.allow_existing_name_collisions else existing_test_names(repo)
    emitted: set[str] = set()

    stats = {
        "product_files": 0,
        "generated_files_skipped": 0,
        "test_files": 0,
        "tests": 0,
        "subtests": 0,
        "endpoint_tests": 0,
        "funcs_seen": 0,
        "funcs_skipped": 0,
        "name_collisions": 0,
        "no_doc": 0,
    }
    skip_reasons: dict[str, int] = {}
    generated_files: list[str] = []

    for path in product_files(repo, args.scope):
        stats["product_files"] += 1
        gf = parse_go_file(path)
        if gf.generated_by:
            stats["generated_files_skipped"] += 1
            generated_files.append(os.path.relpath(path, repo))
            continue
        if not gf.package:
            continue

        blocks: list[str] = []
        for fn in gf.funcs:
            stats["funcs_seen"] += 1
            reason = why_skip(fn)
            if reason:
                stats["funcs_skipped"] += 1
                skip_reasons[reason] = skip_reasons.get(reason, 0) + 1
                continue
            # Repo convention: Test<Method>, receiver type NOT in the name.
            test_name = "Test" + fn.name[0].upper() + fn.name[1:]
            if test_name in taken or test_name in emitted:
                stats["name_collisions"] += 1
                continue
            emitted.add(test_name)

            fn_routes = routes.get(fn.name, []) if fn.recv else []
            body_lines: list[str] = []
            if fn_routes:
                stats["endpoint_tests"] += 1
                for scenario in endpoint_scenarios(fn_routes):
                    stats["subtests"] += 1
                    body_lines.append(
                        f'\tt.Run("{go_string(scenario)}", '
                        f'func(t *testing.T) {{ t.Skip("TODO") }})'
                    )
            else:
                todo = todo_sentence(fn)
                if todo.endswith("需要人工判斷這個函式的可觀察結果是什麼"):
                    stats["no_doc"] += 1
                body_lines.append(f'\tt.Skip("{go_string(todo)}")')

            stats["tests"] += 1
            blocks.append(
                f"func {test_name}(t *testing.T) {{\n" + "\n".join(body_lines) + "\n}\n"
            )

        if not blocks:
            continue
        rel = os.path.relpath(path, repo)
        dest = os.path.join(out, rel[: -len(".go")] + "_test.go")
        os.makedirs(os.path.dirname(dest), exist_ok=True)
        header = (
            f"// Skeleton generated from {rel} by gen_test_skeletons.py.\n"
            f"// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.\n\n"
            f"package {gf.package}\n\n"
            f'import "testing"\n\n'
        )
        with open(dest, "w", encoding="utf-8") as fh:
            fh.write(header + "\n".join(blocks))
        stats["test_files"] += 1

    print(f"repo:  {repo}")
    print(f"out:   {out}")
    print(f"scope: {args.scope}")
    for k, v in stats.items():
        print(f"  {k}: {v}")
    print("  skipped-func reasons:")
    for r, c in sorted(skip_reasons.items(), key=lambda kv: -kv[1]):
        print(f"    {c:5d}  {r}")
    print("  generated files excluded:")
    for g in generated_files:
        print(f"    {g}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
