package tools

import (
	"strings"
	"testing"
)

func TestContainerArgv(t *testing.T) {
	argv := containerArgv("podman", "alpine:3", "/repo", "echo hi")
	joined := strings.Join(argv, " ")
	for _, want := range []string{"podman run --rm", "--network none", "-v /repo:/repo", "-w /repo", "alpine:3", "sh -c echo hi"} {
		if !strings.Contains(joined, want) {
			t.Errorf("container argv missing %q: %v", want, argv)
		}
	}
	// Default engine is docker.
	if containerArgv("", "img", "/w", "cmd")[0] != "docker" {
		t.Error("empty engine should default to docker")
	}
}

func TestNetnsArgv(t *testing.T) {
	argv := netnsArgv("echo hi")
	if argv[0] != "unshare" || argv[1] != "-n" {
		t.Errorf("netns argv should start with 'unshare -n': %v", argv)
	}
	if argv[len(argv)-1] != "echo hi" {
		t.Errorf("netns argv should end with the command: %v", argv)
	}
}
