How root test commands are run (the harness contract):
- Each command runs with `sh -c` from the repository root, one at a time, with the brief's declared env and secrets in its environment and a time limit. It passes when it exits 0, and only then; nobody reads its output.
- Before the first command the harness builds every main package under cmd/ into a directory that is first on PATH. `venture-server serve` therefore runs the binary built from cmd/venture-server. Never write a path to a binary, and never use `go run`, `go install`, `go get` or `go generate`.
- It exports GM_ACCEPTANCE_ADDR (a free loopback host:port), GM_ACCEPTANCE_URL (http:// plus that address), GM_ACCEPTANCE_BIN (the directory of the built binaries) and GM_ACCEPTANCE_PIDFILE. Tell a server to listen on GM_ACCEPTANCE_ADDR the way the brief says it takes an address; never hard-code a port such as localhost:8080. The harness kills the command's process group afterwards.
- No placeholders: a command is run exactly as written. A variable such as $ID must be set earlier in the same command, or be one of the GM_ACCEPTANCE_* names or a declared env or secret name.

A root test must be able to fail. A reviewer rejects it, naming one of these findings, when:
- masked_failure: it throws the exit status away (`|| true`, `|| :`, `|| echo ...`, `|| exit 0`, `2>/dev/null ||`, `set +e`, a last line of `; true` or `; echo ...`). A guard that exits non-zero is fine: `|| exit 1`.
- success_echo: a trailing `&& echo OK` is the only thing that "asserts".
- placeholder: it holds `{id}`, `<id>`, `...`, TODO or "(simulate".
- undefined_variable: it uses a $VARIABLE nothing defines.
- curl_unasserted: a curl has neither -f (or --fail) nor an assertion on its output (`grep -q`, `jq -e`, `test`, `[`, `diff`, `cmp`).
- jq_unasserted: a jq has no -e and its output is not asserted.
- print_only: it only prints (echo, printf, cat, ls, find, head, wc, awk without exit).

Good (each exits non-zero when the requirement does not hold):
- `! grep -rqE 'gorm|entgo|sqlx' internal/`
- `curl -fsS "$GM_ACCEPTANCE_URL/v1/openapi.json" | jq -e '.paths | length >= 90'` and `test "$(curl -s -o /dev/null -w '%{http_code}' "$GM_ACCEPTANCE_URL/v1/ventures/none")" = 404`, each run after the server was started
- `HTTP_ADDR="$GM_ACCEPTANCE_ADDR" venture-server serve & pid=$!; for i in $(seq 1 20); do curl -fsS "$GM_ACCEPTANCE_URL/healthz" | grep -qx ok && kill $pid && exit 0; sleep 0.5; done; kill $pid; exit 1`

Bad (do not write these):
- `grep -r 'gorm' internal/ || echo "No ORM found"` always exits 0; the grep result decides nothing.
- `venture-server serve & sleep 2; curl -s localhost:8080/healthz` curl -s exits 0 on a 404, a fixed port, and nothing checks the body.
- `curl -s "$GM_ACCEPTANCE_URL/ventures/{id}/pnl" | jq '.variance != 0'` a placeholder, and jq without -e exits 0 on false. `... (simulate flow) ... && echo 'E2E OK'` is not a test at all.
