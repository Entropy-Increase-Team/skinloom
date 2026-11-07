//go:build !unix && !windows

package datalock

import "os"

// No file locks here (wasm, plan9): nothing is refused.
func lock(*os.File, bool) error { return nil }

func unlock(*os.File) error { return nil }
