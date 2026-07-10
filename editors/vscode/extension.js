// Minimal VS Code client: pipes the current selection to `gophermind --print`
// (stream-json) and shows the answer. This is a thin driver, not a fork.
const vscode = require("vscode");
const { spawn } = require("child_process");

function activate(context) {
  const disposable = vscode.commands.registerCommand("gophermind.ask", async () => {
    const editor = vscode.window.activeTextEditor;
    if (!editor) return;
    const sel = editor.document.getText(editor.selection) || editor.document.getText();
    const question = await vscode.window.showInputBox({ prompt: "Ask gophermind about the selection" });
    if (!question) return;

    // SECURITY: read the binary path ONLY from trusted (user/global) settings,
    // never from workspace settings — otherwise a malicious repo's
    // .vscode/settings.json could point it at an arbitrary executable (ACE). The
    // setting is also declared "scope": "machine" so VS Code ignores workspace
    // overrides; inspect().globalValue is a belt-and-suspenders on top.
    const inspected = vscode.workspace.getConfiguration("gophermind").inspect("binary");
    const bin = inspected?.globalValue || "gophermind";
    const cwd = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
    // --print with stream-json output; the task includes the selection as context.
    const proc = spawn(bin, ["--output-format", "json", "ask", `${question}\n\nContext:\n${sel}`], { cwd });

    let out = "";
    proc.stdout.on("data", (d) => (out += d.toString()));
    proc.on("close", () => {
      let text = out;
      try { text = JSON.parse(out).result ?? out; } catch (_) {}
      vscode.window.showInformationMessage(text.slice(0, 500));
    });
  });
  context.subscriptions.push(disposable);
}

function deactivate() {}
module.exports = { activate, deactivate };
