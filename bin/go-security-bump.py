#!/usr/bin/env python3
"""Scan the Go modules for Go standard-library vulnerabilities and, when a patch
release of the same Go minor fixes them, apply the version bump that PR #650 did
by hand. Driven by .github/workflows/go-security-bump.yml; run from the repo root.

  scan              govulncheck every module; prints the bump target (or nothing)
  apply OLD NEW     rewrite the Go version in go.mod, ci.yml and the install guide
  anchor SRC_SHA    rebuild dist/officraft per dist/officraft/BUILD.md

GOVULNCHECK and GO_RELEASES_URL override the scanner binary and the release list.
"""

import json
import os
import re
import subprocess
import sys
import urllib.request
from pathlib import Path

ROOT = Path.cwd()
MODULE_DIRS = ["server/ocserverd", "cli/ocagent", "cli/ocwarden", "cli/officraft"]
CI_WORKFLOW = ".github/workflows/ci.yml"
INSTALL_GUIDE = "docs/guide/install.md"
GO_RELEASES_URL = os.environ.get("GO_RELEASES_URL", "https://go.dev/dl/?mode=json")
# "toolchain" findings are vulnerabilities in the go command itself; they are fixed
# by the same release bump as "stdlib" ones.
GO_ITSELF = {"stdlib", "toolchain"}


class BumpError(Exception):
    pass


def parse_version(v):
    m = re.fullmatch(r"v?(?:go)?(\d+)\.(\d+)(?:\.(\d+))?", v)
    if not m:
        raise BumpError(f"unrecognised Go version {v!r}")
    return tuple(int(x or 0) for x in m.groups())


def fmt(v):
    return ".".join(str(x) for x in v)


def repo_go_version():
    text = (ROOT / MODULE_DIRS[0] / "go.mod").read_text()
    m = re.search(r"^go (\S+)$", text, re.M)
    if not m:
        raise BumpError(f"{MODULE_DIRS[0]}/go.mod has no go directive")
    return parse_version(m.group(1))


def json_stream(text):
    dec = json.JSONDecoder()
    i = 0
    while True:
        while i < len(text) and text[i].isspace():
            i += 1
        if i >= len(text):
            return
        obj, i = dec.raw_decode(text, i)
        yield obj


def scan_module(module_dir, current):
    env = dict(os.environ, GOOS="darwin", GOARCH="arm64", GOTOOLCHAIN="local")
    proc = subprocess.run(
        [os.environ.get("GOVULNCHECK", "govulncheck"), "-format", "json", "./..."],
        cwd=ROOT / module_dir, env=env, capture_output=True, text=True,
    )
    if proc.returncode != 0:
        raise BumpError(f"govulncheck failed in {module_dir}:\n{proc.stderr}")
    findings = {}
    for obj in json_stream(proc.stdout):
        # Stdlib findings are judged against the toolchain that ran the scan, not
        # against go.mod; a scan on any other Go version answers a different question.
        if "config" in obj and obj["config"].get("go_version") != "go" + fmt(current):
            raise BumpError(
                f"{module_dir} was scanned with {obj['config'].get('go_version')}, "
                f"but the repo pins go {fmt(current)}"
            )
        f = obj.get("finding")
        if not f or f["trace"][0].get("module") not in GO_ITSELF:
            continue
        # Only symbol-level findings (a vulnerable function is actually reached),
        # matching govulncheck's own "Your code is affected" verdict.
        if not f["trace"][0].get("function"):
            continue
        findings[f["osv"]] = f.get("fixed_version", "")
    return findings


def latest_patch(current):
    with urllib.request.urlopen(GO_RELEASES_URL, timeout=60) as resp:
        releases = json.load(resp)
    same_minor = [
        parse_version(r["version"]) for r in releases
        if r.get("stable") and parse_version(r["version"])[:2] == current[:2]
    ]
    if not same_minor:
        raise BumpError(
            f"go {current[0]}.{current[1]} is no longer in the supported release list; "
            "a minor-version upgrade is needed and is not automated"
        )
    return max(same_minor)


def cmd_scan():
    current = repo_go_version()
    findings = {}
    for d in MODULE_DIRS:
        findings.update(scan_module(d, current))
    if not findings:
        print(f"clean: no Go standard-library vulnerability reached at go {fmt(current)}",
              file=sys.stderr)
        return ""
    fixable = {osv: fv for osv, fv in findings.items() if fv}
    unfixed = sorted(set(findings) - set(fixable))
    if unfixed:
        print("no fixed release yet: " + ", ".join(unfixed), file=sys.stderr)
    if not fixable:
        return ""
    target = latest_patch(current)
    beyond = sorted(osv for osv, fv in fixable.items() if parse_version(fv) > target)
    if beyond:
        raise BumpError(
            f"go {fmt(target)} is the newest {current[0]}.{current[1]} release but does not fix "
            + ", ".join(beyond) + "; a minor-version upgrade is needed and is not automated"
        )
    if target <= current:
        return ""
    print(f"bump go {fmt(current)} -> {fmt(target)} for " + ", ".join(sorted(fixable)),
          file=sys.stderr)
    return json.dumps({"from": fmt(current), "to": fmt(target), "vulns": sorted(fixable)})


def rewrite(path, pairs):
    p = ROOT / path
    text = p.read_text()
    new = text
    for pattern, repl in pairs:
        new = re.sub(pattern, repl, new, flags=re.M)
    if new == text:
        raise BumpError(f"{path}: no Go version found to rewrite; the layout drifted from what this script knows")
    p.write_text(new)
    return new


def cmd_apply(old, new):
    o = re.escape(old)
    for d in MODULE_DIRS:
        rewrite(f"{d}/go.mod", [(rf"^go {o}$", f"go {new}"), (rf"^toolchain go{o}$", f"toolchain go{new}")])
    rewrite(CI_WORKFLOW, [(rf"go-version: '{o}'", f"go-version: '{new}'"),
                          (rf"go\.mod says {o}\b", f"go.mod says {new}")])
    rewrite(INSTALL_GUIDE, [(rf"`go {o}`", f"`go {new}`")])
    # The rewrite above is pattern-by-pattern; a spelling it does not know would
    # survive and leave CI and go.mod disagreeing, so any leftover fails the run.
    # Bounded so that 1.27.1 does not match inside its own replacement 1.27.12.
    old_version = re.compile(rf"(?<![\d.]){o}(?![\d])")
    leftovers = [
        f"{path}:{n}:{line}"
        for path in [*[f"{d}/go.mod" for d in MODULE_DIRS], CI_WORKFLOW, INSTALL_GUIDE]
        for n, line in enumerate((ROOT / path).read_text().splitlines(), 1)
        if old_version.search(line)
    ]
    if leftovers:
        raise BumpError(f"go {old} is still mentioned after the rewrite:\n" + "\n".join(leftovers))


def cmd_anchor(source_sha):
    module = ROOT / "cli/officraft"
    subprocess.run(
        ["go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w",
         "-o", "../../dist/officraft/officraft", "./..."],
        cwd=module, check=True,
    )
    files = sorted(
        str(p.relative_to(ROOT)) for p in module.rglob("*")
        if (p.is_file() or p.is_symlink())
        and (p.suffix in (".go", ".s") or p.name in ("go.mod", "go.sum"))
        and not p.name.endswith("_test.go")
    )
    listing = subprocess.run(["shasum", "-a", "256", *files], cwd=ROOT, check=True,
                             capture_output=True, text=True).stdout
    digest = subprocess.run(["shasum", "-a", "256"], input=listing, check=True,
                            capture_output=True, text=True).stdout.split()[0]
    dist = ROOT / "dist/officraft"
    (dist / "source.sha256").write_text(digest + "\n")
    binary = subprocess.run(["shasum", "-a", "256", "officraft"], cwd=dist, check=True,
                            capture_output=True, text=True).stdout
    (dist / "binary.sha256").write_text(binary)
    build_md = dist / "BUILD.md"
    build_md.write_text(re.sub(r"^Source commit: `[0-9a-f]+`", f"Source commit: `{source_sha[:7]}`",
                               build_md.read_text(), count=1, flags=re.M))
    # The digest above is a second spelling of the pipeline in BUILD.md; the
    # checker is the one CI runs, so it decides whether the two agree.
    subprocess.run(["bash", "bin/check-officraft-dist"], cwd=ROOT, check=True)


def main(argv):
    try:
        if argv[:1] == ["scan"] and len(argv) == 1:
            print(cmd_scan())
        elif argv[:1] == ["apply"] and len(argv) == 3:
            cmd_apply(argv[1], argv[2])
        elif argv[:1] == ["anchor"] and len(argv) == 2:
            cmd_anchor(argv[1])
        else:
            print(__doc__, file=sys.stderr)
            return 2
    except BumpError as e:
        print(f"go-security-bump: {e}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
