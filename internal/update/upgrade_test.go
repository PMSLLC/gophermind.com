package update

import (
	"strings"
	"testing"
)

func TestAssetName(t *testing.T) {
	// Matches goreleaser name_template: <name>_<version>_<os>_<arch>.<ext>
	if got := AssetName("gophermind", "1.2.3", "linux", "amd64"); got != "gophermind_1.2.3_linux_amd64.tar.gz" {
		t.Errorf("linux asset = %q", got)
	}
	if got := AssetName("gophermind", "1.2.3", "windows", "arm64"); got != "gophermind_1.2.3_windows_arm64.zip" {
		t.Errorf("windows asset should be .zip, got %q", got)
	}
	// macOS ships a universal archive.
	if got := AssetName("gophermind", "1.2.3", "darwin", "arm64"); got != "gophermind_1.2.3_darwin_all.tar.gz" {
		t.Errorf("darwin asset should be universal, got %q", got)
	}
}

func TestParseChecksums(t *testing.T) {
	data := "abc123  gophermind_1.0_linux_amd64.tar.gz\ndef456  gophermind_1.0_darwin_all.tar.gz\n"
	m := ParseChecksums(data)
	if m["gophermind_1.0_linux_amd64.tar.gz"] != "abc123" {
		t.Errorf("checksum map wrong: %v", m)
	}
}

func TestVerifyChecksum(t *testing.T) {
	content := []byte("hello world")
	// sha256("hello world") = b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9
	const sum = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if !VerifyChecksum(content, sum) {
		t.Error("correct checksum should verify")
	}
	if VerifyChecksum(content, "deadbeef") {
		t.Error("wrong checksum must not verify")
	}
	if VerifyChecksum(content, strings.ToUpper(sum)) != true {
		t.Error("checksum comparison should be case-insensitive")
	}
}
