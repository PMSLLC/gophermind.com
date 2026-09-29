package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/tree"
	"gophermind/gophermind-lib/briefv2/vault"
	"gophermind/gophermind-lib/config"
)

// vaultOptions is a variable so tests can lower the scrypt work factor.
var vaultOptions vault.Options

const briefUsage = `usage:
  gophermind brief validate <brief.md>
  gophermind brief vault set <NAME>      (value from a terminal prompt or stdin)
  gophermind brief vault list
  gophermind brief tree check <run-dir>`

// runBrief implements `gophermind brief ...` and returns the process exit
// code: 0 ok, 1 error, 2 invalid brief.
func runBrief(args []string, in *os.File, out, errw io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errw, briefUsage)
		return 1
	}
	switch args[0] {
	case "validate":
		if len(args) != 2 {
			fmt.Fprintln(errw, briefUsage)
			return 1
		}
		return briefValidate(args[1], out, errw)
	case "vault":
		return briefVault(args[1:], in, out, errw)
	case "tree":
		if len(args) == 3 && args[1] == "check" {
			return briefTreeCheck(args[2], out, errw)
		}
	}
	fmt.Fprintln(errw, briefUsage)
	return 1
}

func briefValidate(path string, out, errw io.Writer) int {
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return 1
	}
	b, err := brief.Parse(src)
	if err != nil {
		var inv *brief.InvalidError
		if errors.As(err, &inv) {
			fmt.Fprintf(errw, "invalid brief: %v\n", err)
			return 2
		}
		fmt.Fprintf(errw, "error: %v\n", err)
		return 1
	}
	for _, w := range b.UndeclaredSecrets() {
		fmt.Fprintf(errw, "warning: line %d: %s looks like a secret name but is not declared under secrets\n", w.Line, w.Token)
	}
	fmt.Fprintf(out, "ok: %s (%s), %d features\n", b.Front.ID, b.Front.Title, len(b.Features))
	return 0
}

func vaultPath() (string, error) {
	if p := os.Getenv("GOPHERMIND_VAULT_PATH"); p != "" {
		return p, nil
	}
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "vault.age"), nil
}

func briefVault(args []string, in *os.File, out, errw io.Writer) int {
	if len(args) == 0 || (args[0] == "set" && len(args) != 2) || (args[0] == "list" && len(args) != 1) || (args[0] != "set" && args[0] != "list") {
		fmt.Fprintln(errw, briefUsage)
		return 1
	}
	path, err := vaultPath()
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return 1
	}
	// When stdin is a pipe the passphrase must come from
	// GOPHERMIND_VAULT_PASSPHRASE: the value is all of stdin, so it would
	// swallow a passphrase sent on the same pipe.
	pass, err := vault.Passphrase("Vault passphrase: ", in, errw)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return 1
	}
	v, err := vault.Open(path, pass, vaultOptions)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return 1
	}
	if args[0] == "list" {
		for _, n := range v.Names(vault.HarnessScope) {
			fmt.Fprintln(out, n)
		}
		return 0
	}
	val, err := vault.ReadSecret("Value for "+args[1]+": ", in, errw)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return 1
	}
	if err := v.Set(vault.HarnessScope, args[1], val); err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "stored %s\n", args[1])
	return 0
}

func briefTreeCheck(dir string, out, errw io.Writer) int {
	tr, err := tree.NewStore(dir).Load()
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return 1
	}
	if len(tr.Nodes) == 0 {
		fmt.Fprintf(errw, "error: no nodes found in %s\n", dir)
		return 1
	}
	if err := tr.CheckStructure(); err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return 1
	}
	if err := tr.CheckWaves(); err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return 1
	}
	waves, _ := tr.ComputeWaves()
	top := 0
	for _, w := range waves {
		if w > top {
			top = w
		}
	}
	fmt.Fprintf(out, "ok: %d nodes, waves 0-%d\n", len(tr.Nodes), top)
	return 0
}
