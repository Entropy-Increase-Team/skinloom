//go:build !windows

package fsperm

func private(string) error { return nil }
