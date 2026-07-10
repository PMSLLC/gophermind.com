package tools

import (
	"os"
	"runtime"
	"strings"
)

// containerImage returns the configured container image for run_shell isolation,
// or "" when container-exec is disabled.
func containerImage() string {
	return strings.TrimSpace(os.Getenv("GOPHERMIND_SHELL_CONTAINER"))
}

// netnsEnabled reports whether run_shell should execute in a disabled network
// namespace (Linux only, via unshare -n).
func netnsEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GOPHERMIND_SHELL_NETNS"))) {
	case "1", "true", "yes", "on":
		return runtime.GOOS == "linux"
	default:
		return false
	}
}

// containerArgv builds the argv to run shellCmd inside a disposable container
// (docker/podman), with the repo mounted read-write at workdir and networking
// disabled — strong isolation beyond ulimits.
func containerArgv(engine, image, workdir, shellCmd string) []string {
	if engine == "" {
		engine = "docker"
	}
	return []string{
		engine, "run", "--rm", "--network", "none",
		"-v", workdir + ":" + workdir,
		"-w", workdir,
		image, "sh", "-c", shellCmd,
	}
}

// netnsArgv builds the argv to run shellCmd in a fresh, network-disabled
// namespace via `unshare -n` — exfiltration-proof command execution on Linux.
func netnsArgv(shellCmd string) []string {
	return []string{"unshare", "-n", "sh", "-c", shellCmd}
}

// shellEngine returns the container engine (GOPHERMIND_SHELL_ENGINE or docker).
func shellEngine() string {
	if e := strings.TrimSpace(os.Getenv("GOPHERMIND_SHELL_ENGINE")); e != "" {
		return e
	}
	return "docker"
}
