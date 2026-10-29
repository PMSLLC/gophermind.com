---
spec_version: "2.0"
id: gm-2026-10-28-001
title: linkcheck
language: go
base_branch: main
landing: commit
on_ambiguity: assume_and_document
---

## Overview

`linkcheck` is a small command-line tool that reads one Markdown or HTML file, or standard input when the name is `-`, extracts every web link in it, and checks whether each link still resolves. It prints one line per link and a summary, and exits non-zero when a link is broken, so it can run in CI on a docs repository. Links are checked concurrently. An optional `--fix` mode rewrites redirected links to their final address. It is one Go binary with no configuration file.

## Features

### Extract links

`links.Extract` finds link targets in text, in order of appearance, whatever the file extension.

- A Markdown link `[text](url)` yields `url`, ended by the first `)` or white space; an image `![alt](url)` yields `url` once.
- A Markdown autolink `<url>` yields `url` when it starts with `http://` or `https://`, compared ignoring case.
- An HTML attribute `href="url"` or `src="url"`, with double or single quotes and any letter case in the attribute name, yields `url`.
- `Start` is the offset of the first byte of the URL and `End` the offset after its last byte, so `text[Start:End]` equals the URL; quotes, angle brackets and parentheses are outside the range.
- Bare URLs in plain text and unquoted attribute values such as `href=x` are not links.
- Links inside code fences or inline code are extracted like any other.
- Empty input and input with no links yield an empty list.
- `links.Rewrite` replaces every extracted target whose text is a key of `repl`, using the offsets from `Extract`, and leaves every other byte unchanged.

### Classify targets

`links.Split` turns the extracted links into the URLs to check and a skipped count.

- A target is checked only when its scheme is `http` or `https`, compared ignoring case, so `HTTP://x/` is checked.
- Every other target (relative paths, `#anchors`, `mailto:`, empty) is skipped, and skipped targets are counted once per occurrence: `[a](#top) [b](#top)` is 2 skipped.
- Two targets are the same URL only when their text is identical, fragment included; a repeated URL is kept once, in order of first appearance.

### Resolve one URL

`check.One` sends a GET request for one URL and returns a `Result`.

- Redirect statuses 301, 302, 303, 307 and 308 are followed, resolving a relative `Location` against the current URL.
- A final status from 200 to 299 is `ok`, or `redirected` when at least one redirect was followed; `Final` is the last URL reached.
- Following 10 redirects and then reaching 2xx is `redirected`; an 11th redirect response is `broken` with reason `too many redirects`.
- Any other final status is `broken` with reason `HTTP <code>`, for example `HTTP 404`.
- A redirect status with no `Location` header, or a `Location` that does not resolve to an `http` or `https` URL, is `broken` with reason `request failed`.
- A request that runs past the context deadline is `broken` with reason `timeout`.
- Any other failure, such as connection refused, is `broken` with reason `request failed`.

### Print the report

`report.Write` prints one line per result in input order, then the summary.

- An `ok` result prints `ok <url>`; a `redirected` result prints `redirected <url> -> <final>`; a broken result prints `broken <url> (<reason>)`.
- With fixing on, the line `fixed: <n>` is printed after the per-URL lines and before the summary, even when `n` is 0.
- The last line is `checked <n> links: <a> ok, <b> redirected, <c> broken, <d> skipped`, where `n` is `a+b+c`.
- An empty file, or one with no links, prints `checked 0 links: 0 ok, 0 redirected, 0 broken, 0 skipped`.
- The exit code is 0 when no link is broken and 1 when at least one is.

### Concurrency and timeout

- `--workers N` sets how many requests run at once; the default is 8 and N must be 1 or more.
- `--timeout D` sets the limit for one URL, in `time.ParseDuration` syntax; the default is `10s` and D must be positive.
- The timeout covers the request and all of its redirects together, not each hop.
- Output order never depends on which request finishes first.

### Fix redirects

`linkcheck --fix docs.md` does a normal check, then rewrites the file.

- Every target whose result is `redirected` is replaced by its final URL; ok and broken links are left as they are.
- `n` in `fixed: <n>` is the number of distinct URLs replaced.
- The file is written only when `n` is at least 1. A fixed file that cannot be written prints `linkcheck: <error>` on standard error and exits with code 2.
- The exit code is still decided by broken links, not by fixes.

### Report errors

- A file that cannot be opened prints `linkcheck: <error>` on standard error and exits with code 2.
- No file argument, or more than one, prints `usage: linkcheck [--fix] [--workers N] [--timeout D] FILE` on standard error and exits with code 2.
- Flags must come before `FILE`; `linkcheck docs.md --fix` is two arguments and gets the usage line and exit code 2.
- An invalid `--workers` or `--timeout` value prints the same usage line and exits with code 2.
- With `--fix`, the file name `-` prints `linkcheck: --fix needs a file, not standard input` on standard error and exits with code 2.

## Architecture

- `cmd/linkcheck` (package `main`): calls `cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)` and exits with the code. Nothing else.
- `internal/cli`: `Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int` is a thin dispatcher over `parseArgs`, `readSource`, `links.Split`, `check.All`, `report.Write` and `writeFixed`, each helper under 40 lines. It never calls `os.Exit`.
- `internal/links`: `Extract(r io.Reader) ([]Link, error)`, `Rewrite(src string, repl map[string]string) string` and `Split(ls []Link) (urls []string, skipped int)`. No network, no files.
- `internal/check`: `All(ctx context.Context, urls []string, client *http.Client, workers int, timeout time.Duration) []Result` checks the URLs with a bounded worker pool and returns results in input order. Each URL gets its own `context.WithTimeout(ctx, timeout)` covering the request and all its redirects; `client.Timeout` stays unset. `One(ctx, client, url) Result` resolves one URL and `follow` walks the redirects. It does not print.
- `internal/report`: `Write(w io.Writer, results []Result, skipped int, fixed int, showFixed bool)`.

Data flow: `cli.Run` reads the input, `links.Extract` lists targets, `links.Split` drops duplicates and counts skipped, `check.All` resolves the rest, `report.Write` prints, and `links.Rewrite` produces the fixed text.

Patterns to follow:

- Functions that read input take an `io.Reader`; functions that print take an `io.Writer`. Only `main` chooses `os.Stdin`, `os.Stdout` and `os.Stderr`.
- The client passed to `check.All` does not follow redirects itself (`CheckRedirect` returns `http.ErrUseLastResponse`); `check` follows them so it can count hops and report the final URL.
- Errors are wrapped with `fmt.Errorf("...: %w", err)` and only `cli.Run` decides the exit code.

## Data

- `Link` holds `URL string`, `Start int` and `End int`.
- `Result` holds `URL string`, `Status string` (one of `ok`, `redirected`, `broken`), `Final string` (the last URL reached) and `Reason string` (the text inside the parentheses for a broken link, otherwise empty).

## Constraints

- Standard library only.
- Go 1.22 or later.
- `gofmt` clean and `go vet` clean.
- Tests are table driven and use `strings.NewReader` and `bytes.Buffer` for input and output.
- Tests that check links use `net/http/httptest` servers, never the real network; tests of `cli.Run` that need a file use `t.TempDir`.
- Timeout tests pass a timeout of 50 milliseconds to `check.All`: one against a handler that waits 500 milliseconds, one against two redirect hops of 30 milliseconds each.
- A test shows that no more than `workers` requests are in flight at once and that more than one runs at once when `workers` is 2 or more.

## Out of scope

- Checking relative links against the file system, or `#anchor` targets.
- Crawling the pages that links point to.
- HEAD requests, retries, caching, or authentication.
- Reading several files, writing a report file, or any output format other than the lines above.
- Full Markdown or HTML parsing.

## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test ./...` passes.
- `printf '' | linkcheck -` prints `checked 0 links: 0 ok, 0 redirected, 0 broken, 0 skipped` and exits with code 0.
- `printf '[a](#top) [b](#top) [c](mailto:x@y.z)\n' | linkcheck -` prints `checked 0 links: 0 ok, 0 redirected, 0 broken, 3 skipped` and exits with code 0.
- `printf '[x](http://127.0.0.1:1/) [y](http://127.0.0.1:1/)\n' | linkcheck -` prints `broken http://127.0.0.1:1/ (request failed)` once, then `checked 1 links: 0 ok, 0 redirected, 1 broken, 0 skipped`, and exits with code 1.
- `linkcheck missing.md` prints a `linkcheck: ` error on standard error and exits with code 2.
- `linkcheck` with no arguments exits with code 2.
- `printf 'x\n' | linkcheck --fix -` prints `linkcheck: --fix needs a file, not standard input` on standard error and exits with code 2.
- `printf 'x\n' | linkcheck --workers 0 -` exits with code 2.
- A test serves 200 and asserts `ok <url>`.
- A test serves a 301 to a 200 and asserts `redirected <url> -> <final>`.
- A test serves a 404 and asserts `broken <url> (HTTP 404)`.
- A test serves a chain of 11 redirects and asserts `broken <url> (too many redirects)`.
- A test runs `--fix` against a `t.TempDir` file holding a redirected link and asserts the file now holds the final URL and the output contains `fixed: 1`.
- A test runs with workers 2 and asserts at most 2 requests in flight and more than 1 at once.
