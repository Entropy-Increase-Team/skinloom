// Package themes embeds the built-in themes. Each subdirectory is a theme package
// (plugin.json with kind "theme" plus its assets), the same format third-party themes use.
package themes

import "embed"

// Builtin holds every built-in theme directory.
//
//go:embed all:default all:brocade
var Builtin embed.FS
