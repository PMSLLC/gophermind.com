
## Tests

- `Greeting("")` returns `Hello, World!`.
- `Greeting("Ada")` returns `Hello, Ada!`.
- `Run(nil, stdout, stderr)` returns 0 and writes `Hello, World!` and a newline to stdout; stderr is empty.
- `Run([]string{}, stdout, stderr)` returns 0 and writes `Hello, World!` and a newline to stdout; stderr is empty.
- `Run([]string{"Ada"}, stdout, stderr)` returns 0 and writes `Hello, Ada!` and a newline to stdout; stderr is empty.
- `Run([]string{""}, stdout, stderr)` returns 0 and writes `Hello, World!` and a newline to stdout; stderr is empty.
- `Run([]string{"a", "b"}, stdout, stderr)` returns 2, writes `usage: hello [NAME]` and a newline to stderr, and writes nothing to stdout.
- Every case uses its own `bytes.Buffer` for stdout and for stderr.
