// Package buildinfo carries version metadata injected at build time via -ldflags.
package buildinfo

var (
	// Version is the SkinLoom release version, e.g. "1.0.0".
	Version = "0.0.0-dev"
	// Commit is the git commit the binary was built from.
	Commit = ""
	// Date is the build time in RFC 3339.
	Date = ""
)
