# Release 0.10.1

Prepared 2026-10-24 under `docs/GOAL-SUMMIT.md` task 6. Not tagged, not published.

Follow `docs/release-0.10.0.md` step by step with `0.10.1` in place of `0.10.0`. The tree is already prepared as that document's step 1a describes: `CHANGELOG.md` carries `## [0.10.1] - 2026-10-24` under an empty `## [Unreleased]`, `npm/package.json` says `0.10.1`, and `go mod tidy` leaves no change. Change the changelog date to the day the tag is made.

What is new since 0.10.0 (see the changelog for each item): Google sign-in and the browser pages (redesigned 2026-10-24), brief upload, stop, run view, live updates and answering clarify questions in the browser, planner and executor hardening found by real csvstat runs, `brief diagnose` and planner refusal records, and server run logs.

Waiting on John before any of these: the tag and the public release (step 2 and 4), the npm one-time code (step 6), the desktop install (step 8) and the mini deploy (step 7). The gate (step 1b) passes on every push of main.
