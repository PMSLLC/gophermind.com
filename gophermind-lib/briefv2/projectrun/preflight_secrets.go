package projectrun

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"gophermind/gophermind-lib/briefv2/settings"
	"gophermind/gophermind-lib/briefv2/vault"
)

type vaultResult struct {
	store        SecretStore
	vaultChecks  []Check // vault passphrase, vault
	secretChecks []Check // secret <NAME>
	keyChecks    []Check // provider <name> key
}

const passphraseFix = "export GOPHERMIND_VAULT_PASSPHRASE=<passphrase>"

// vault opens the vault when the brief declares a secret or a provider names
// an api_key_secret. It reads the harness scope only: no run folder exists yet.
// Opening never creates the file. cfg may be nil when settings failed.
func (p *pre) vault(cfg *settings.Config) vaultResult {
	var vr vaultResult
	needed := len(p.b.Front.Secrets) > 0
	if cfg != nil {
		for _, pc := range cfg.Providers {
			if pc.APIKeySecret != "" {
				needed = true
			}
		}
	}
	if !needed {
		vr.vaultChecks = append(vr.vaultChecks, pass("vault passphrase", "not needed"))
		return vr
	}

	open := false
	pass0 := p.env.Getenv(vault.PassphraseEnv)
	if pass0 == "" {
		vr.vaultChecks = append(vr.vaultChecks, fail("vault passphrase", vault.PassphraseEnv+" is empty", passphraseFix))
	} else {
		vr.vaultChecks = append(vr.vaultChecks, pass("vault passphrase", vault.PassphraseEnv+" is set"))
		path, err := p.env.VaultPath()
		if err != nil {
			vr.vaultChecks = append(vr.vaultChecks, fail("vault", "the vault path could not be resolved", "set GOPHERMIND_VAULT_PATH or HOME"))
		} else if v, err := p.env.OpenVault(path, pass0); err != nil {
			vr.vaultChecks = append(vr.vaultChecks, fail("vault", "the vault at "+path+" did not open with this passphrase", "check that "+vault.PassphraseEnv+" is the passphrase the vault was created with: "+passphraseFix))
		} else {
			vr.store = v
			open = true
			vr.vaultChecks = append(vr.vaultChecks, pass("vault", "opened "+path))
		}
	}

	if open && cfg != nil {
		lookup := func(name string) (string, error) {
			if v, ok := vr.store.Get(vault.HarnessScope, name); ok {
				return v, nil
			}
			return "", fmt.Errorf("not in the vault; run: gophermind brief vault set %s", name)
		}
		if _, err := p.env.BuildProviders(cfg, lookup); err != nil {
			name := "provider key"
			for _, pc := range cfg.Providers {
				if strings.HasPrefix(err.Error(), "settings: provider "+pc.Name+":") {
					name = "provider " + pc.Name + " key"
				}
			}
			secret := "<NAME>"
			for _, pc := range cfg.Providers {
				if pc.APIKeySecret != "" && strings.Contains(err.Error(), fmt.Sprintf("%q", pc.APIKeySecret)) {
					secret = pc.APIKeySecret
				}
			}
			vr.keyChecks = append(vr.keyChecks, fail(name, err.Error(), "gophermind brief vault set "+secret))
		}
	}

	for _, s := range Sources(vr.store, p.b, p.o.Generate, p.o.Attended) {
		label := "secret " + s.Name
		switch {
		case s.Source != SourceMissing:
			vr.secretChecks = append(vr.secretChecks, pass(label, "source: "+string(s.Source)))
		case !open:
			vr.secretChecks = append(vr.secretChecks, notChecked(label, "the vault is not open", "gophermind brief vault set "+s.Name))
		default:
			vr.secretChecks = append(vr.secretChecks, fail(label, "not in the vault (harness scope), not generated and no one to ask", "gophermind brief vault set "+s.Name))
		}
	}
	return vr
}

func isLoopbackHost(h string) bool {
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// databaseChecks covers every declared secret whose harness value is a
// postgres URL. Only host and port are ever printed.
func (p *pre) databaseChecks(store SecretStore) []Check {
	if store == nil {
		return nil
	}
	var out []Check
	seen := map[string]string{} // host:port/db -> secret name
	found := false
	for _, s := range p.b.Front.Secrets {
		val, ok := store.Get(vault.HarnessScope, s.Name)
		if !ok {
			continue // missing, or a generated value: nothing to parse
		}
		u, err := url.Parse(val)
		if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
			continue
		}
		found = true
		label := "database " + s.Name
		host, port := u.Hostname(), u.Port()
		if port == "" {
			port = "5432"
		}
		if q := u.Query(); q.Has("host") || q.Has("hostaddr") {
			out = append(out, fail(label, "the URL carries a host or hostaddr parameter that overrides the host", "store a URL without host= or hostaddr= parameters: gophermind brief vault set "+s.Name))
			continue
		}
		tunnel := "ssh -N -L " + port + ":127.0.0.1:5432 mini"
		if !isLoopbackHost(host) {
			out = append(out, fail(label, "non-loopback host "+net.JoinHostPort(host, port)+": the sandbox denies it", tunnel))
			continue
		}
		if err := p.env.Dial(p.ctx, net.JoinHostPort(host, port)); err != nil {
			out = append(out, fail(label, net.JoinHostPort(host, port)+" refused the connection", tunnel))
		} else {
			out = append(out, pass(label, net.JoinHostPort(host, port)+" accepts connections"))
		}
		key := "loopback:" + port + "/" + strings.TrimPrefix(u.Path, "/")
		if prev, dup := seen[key]; dup {
			out = append(out, fail(label, prev+" and "+s.Name+" name the same database", "store a different database for "+s.Name+": gophermind brief vault set "+s.Name))
		} else {
			seen[key] = s.Name
		}
	}
	if found {
		out = append(out, pass("database note", "database emptiness is not checked"))
	}
	return out
}
