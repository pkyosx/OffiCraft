# Contributing to OffiCraft

Thanks for taking an interest. This page tells you what to do, what happens
after you open a pull request, and when you can expect to hear from us.

The rest of this repository's documentation is a mix of English and Chinese.
This page is in English because it is the front door. You are welcome to write
issues and pull requests in either language.

---

## Before you write any code

**Tell us what you want to do first.** Open an issue that describes the problem
and the change you have in mind, and wait for a maintainer to say the direction
is one we want.

We would rather spend one exchange telling you "not this one" than have you
build something we are going to decline. Typo fixes, broken links and obviously
wrong documentation do not need this step — just send the pull request.

Also, before you start:

- Read the [README](README.md) and the developer notes under
  [`docs/dev/`](docs/dev/README.md).
- Keep one pull request to one topic. A change bundled with an unrelated
  refactor is much harder to accept, and usually gets sent back rather than
  reviewed.
- Do not commit build artifacts, vendored binaries, or anything that came out
  of a build directory.
- Do not commit credentials of any kind. The repository runs a content-level
  secret scan; a leaked key is a leaked key even after you force-push it away.
- Do not change anything under `.github/` unless the change *is* your topic.
  Workflow edits are reviewed as workflow edits, not as part of a feature.

---

## What happens after you open a pull request

There are two stages, and they happen **in this order**:

**Stage 1 — direction.** The project owner decides whether this is something the
project wants at all. Nobody reviews your code before that answer exists.

**Stage 2 — code review.** Only once the direction is agreed does anyone read
the diff and comment on it.

If the answer at stage 1 is no, we stop there and tell you. We will not send you
through a code review for a change we are not going to take — that would waste
your time as much as ours. A "no" at stage 1 is about the change, not about the
quality of the work.

### How quickly you will hear from us

**Within one business day you will get at least an acknowledgement** — a note
saying we have seen it and are looking. That is a first response, not a verdict:
the direction decision itself may take longer, and we will say so if it does.

If a business day passes with silence on your pull request, that is our mistake.
Say so on the thread.

---

## Automated checks

Opening a pull request from a fork schedules this repository's CI workflow
(`.github/workflows/ci.yml`) against your branch. It runs the same gates we run
on our own pull requests — no *gate* depends on a repository secret, so a fork
gets the complete check, not a reduced one. (This repository does use one
secret, but it drives a post-merge notification job that is marked
`oc-job-role: not-a-gate` and is gated on `github.ref == 'refs/heads/main'`, so
it never runs on a pull request at all. "No gate needs a secret" is the claim;
"this repository holds no secrets" is not.)

What the gates cover: unit tests, the regenerate-and-compare consistency gates,
the black-box conformance suite, the real-browser end-to-end suite, and the
host-shaped guard suites. The authoritative definition of every check is the repo-root `Makefile` — one
named target per check, each implemented exactly once — not the workflow file
and not here. Read the targets rather than trusting a list in prose:
`grep -nE '^[a-z][a-z0-9-]*:' Makefile`, and `grep -n 'run-checks' .github/workflows/ci.yml`
for which cell calls which. (That second query said `grep -n 'make '` until an
independent review ran it and got zero lines: the cells invoke the targets
through `bin/run-checks.sh`, never `make` directly — a query that answers nothing
is worse than none, because it reads as if it were checked.)

**The bar for merging:** every check on the pull request has reached a
conclusion, and every check is `success` — except for the jobs that only run
after a merge to `main`, which appear on a pull request with a conclusion of
`skipped`. A `skipped` conclusion on those is normal and expected. A check with
**no** conclusion at all is not a pass: it means that check never ran, which on
screen looks exactly like a check that did not go red.

### If your change adds a database migration

Adding a migration touches `server/ocserverd/migration.lock`, a generated file
whose whole job is to stop two such branches from landing on top of each other.
When your branch already carries the lock, that shows up as a **conflict** on
that file whenever another migration lands before yours — **including when the
two version numbers do not actually collide**. That is the designed cost, not a
bug.

**But a conflict is not the only way it stops you, and the quiet case is the one
to watch.** If your branch is older than the lock file, git takes the lock as a
plain *added* file when you merge — clean, no conflict, nothing asking you to
look. Your migration is then in the tree but not in the list, and CI fails with
`[lock:missing]` instead. So treat **"run `./bin/gen-migration-lock` after
merging main"** as unconditional; do not wait for a conflict to remind you.

Do not hand-merge that file. Renumber your own migration *file* if the numbers
really did collide, then run `./bin/gen-migration-lock`, which rewrites the
whole thing from the tree. The `roll sha256:` header line is computed; never
edit it by hand. Then read the diff: your new migration must appear as lines
**appended at the end**. A changed line in the middle means an already-released
migration was edited or removed — stop and look, do not commit past it.

Note also that a conflicted pull request gets **no checks at all**, and an
absent conclusion is not a pass — see the paragraph above.

### Green can go stale

A green tick on your pull request proves that *the workflow as it existed when
that run happened* passed. If the workflow gains a gate afterwards, the old
green does not cover it, and the pull request will still be showing ticks that
no longer mean what they look like.

So before we merge, we confirm that the head commit was checked **under the
current workflow**. If it was not, we will ask you to rebase onto the latest
`main` and push again so a fresh round runs. This is routine — it is not a
comment on your change.

### Running the checks yourself

`bash bin/ci.sh` runs the whole gate locally. It is NOT what decides whether your
change can be merged — the pull request's checks are (owner ruling, 2026-08-11);
see "The bar for merging" above. What it is for is checking your own work before
you open the PR. It expects a macOS Apple Silicon machine and a full developer
setup, so we do not assume every contributor can run it. If you cannot, say so in
the pull request description and let the cloud checks do the talking — but please
at least run the tests for whatever you touched.

To run only part of it, name the targets instead: `bash bin/run-checks.sh <target> …`
(target names: `grep -nE '^[a-z][a-z0-9-]*:' Makefile`). That wrapper's own
all-clear line is what tells you it passed; it deliberately cannot print the
whole-run marker `bin/ci.sh` ends with, and a guard enforces that no other script
is even capable of printing it.

Push a branch to your fork and open the pull request; the cloud round starts
from there on its own — this repository does not hold fork runs for maintainer
approval, so you should see checks moving within a minute or two of opening the
pull request. If nothing starts, that is a fault worth telling us about on the
thread, not something you are waiting on us to release.

---

## What gets declined without a code review

These are the shapes we turn away at stage 1, so you know before you invest the
effort:

- **Changes that move the product's direction** without having been agreed
  first — new user-facing concepts, new pages, renamed vocabulary.
- **Large unsolicited refactors**, reformatting sweeps, or dependency bumps
  bundled with unrelated work.
- **New runtime dependencies**, and anything that requires a paid service, an
  API key, or an account to run the tests.
- **Workflow changes that weaken a gate**: adding a secret to a gate job, adding
  a trigger, adding filters that make gates skip, making a job tolerate its own
  failure, or otherwise making a check able to report a pass it did not earn.
- **Anything that relaxes branch protection or bypasses the merge rules.**
- **Removing a test or a guard** to make a change pass. If a guard is wrong, say
  so and argue it — that is a legitimate change, but it is its own pull request
  with its own reasoning.
- **Generated output committed by hand.** Several artifacts in this repository
  are asserted to be byte-identical to what their generator produces; edit the
  generator.

None of these is a permanent ban on the idea. They are all "open an issue and
let's talk about it first".

---

## Style of the change itself

- Explain **why** in the pull request description. What was wrong before, and
  what is true now. The diff already says what changed.
- If you corrected something a comment or a document claimed, correct the
  comment or document in the same change. A stale explanation next to fixed code
  is the failure mode this repository cares most about.
- Do not assert numbers that will expire — counts of tests, counts of jobs,
  durations. If a number matters, point at the thing that produces it.
- Say plainly what you verified and what you only reasoned about. "I read the
  code and believe X" and "I ran it and observed X" are different claims, and
  both are welcome as long as they are labelled.

---

## Licence

This project is MIT licensed. By contributing, you agree that your contribution
is licensed under the same terms.
