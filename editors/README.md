# Editor clients

Thin clients that drive the `gophermind` CLI (`ask --output-format json`) from an
editor — no logic is duplicated, they just shell out.

- `vscode/` — a VS Code extension exposing the `gophermind: Ask about selection`
  command. Load it with "Run Extension" or package via `vsce`.
- `nvim/` — a Neovim plugin exposing `:GophermindAsk <question>`. Add
  `editors/nvim` to your runtimepath and call `require("gophermind").setup()`.
