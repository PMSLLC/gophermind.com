-- Minimal Neovim client for gophermind: :GophermindAsk pipes the current buffer
-- (or visual selection) plus a prompt to `gophermind ask --output-format json`
-- and echoes the answer. A thin driver over the CLI, not a reimplementation.
local M = {}

function M.ask(question)
  local bin = vim.g.gophermind_binary or "gophermind"
  local ctx = table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), "\n")
  local task = question .. "\n\nContext:\n" .. ctx
  local out = vim.fn.system({ bin, "--output-format", "json", "ask", task })
  local ok, decoded = pcall(vim.fn.json_decode, out)
  local answer = (ok and decoded and decoded.result) or out
  vim.notify(answer, vim.log.levels.INFO, { title = "gophermind" })
end

function M.setup()
  vim.api.nvim_create_user_command("GophermindAsk", function(opts)
    M.ask(opts.args)
  end, { nargs = "+", desc = "Ask gophermind about the current buffer" })
end

return M
