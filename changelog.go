// Package gophermind (the module root) exists only to embed repository-root
// documents that subpackages need at runtime — currently the changelog, whose
// latest entries are shown under the startup banner.
package gophermind

import _ "embed"

// Changelog is the raw contents of CHANGELOG.md, embedded at build time.
//
//go:embed CHANGELOG.md
var Changelog string
