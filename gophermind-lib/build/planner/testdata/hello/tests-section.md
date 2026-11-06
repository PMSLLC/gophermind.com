## Tests


- `Greeting("")` returns exactly `Hello, World!`.
- `Greeting("Ada")` returns exactly `Hello, Ada!`.
- `Greeting("A B")` returns exactly `Hello, A B!`.
- `Greeting(" Alice ")` returns exactly `Hello,  Alice !`.
- No `Greeting` result ends with a newline.
- `Run(nil, stdout, stderr)` and `Run([]string{}, stdout, stderr)` return `0`, write exactly `Hello, World!\n` to stdout and nothing to stderr.
- `Run([]string{"Ada"}, stdout, stderr)` returns `0`, writes exactly `Hello, Ada!\n` to stdout and nothing to stderr.
- `Run([]string{""}, stdout, stderr)` returns `0`, writes exactly `Hello, World!\n` to stdout and nothing to stderr.
- `Run([]string{"a", "b"}, stdout, stderr)` returns `2`, writes nothing to stdout and exactly `usage: hello [NAME]\n` to stderr.

