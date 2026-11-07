// Package fsperm keeps files that hold secrets private where Unix mode bits do not:
// on Windows a new file takes its folder's permissions, which in a data directory
// under C:\ or a shared folder let every local user read it (ADR 0039).
package fsperm

// Private limits path to the account running SkinLoom, SYSTEM and the Administrators
// group on Windows, replacing the inherited permissions; for a folder the files
// created in it later get the same. Elsewhere it does nothing: callers create such
// files with mode 0600 or folders with 0700. Left as they are: a path that is already
// private (it no longer inherits, whoever set it up, so an administrator running a
// command keeps the service account that install added), one the account may not
// change the permissions of, and a volume without permissions (FAT).
func Private(path string) error { return private(path) }
