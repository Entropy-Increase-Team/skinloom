#!/bin/sh
# SkinLoom (云织) one-click start for macOS (double-click in Finder).
# Starts the backend and the bundled frontend on one port and opens the browser.
# Data (database, config, uploads) is kept in the "data" folder next to this file.
#
# First run on an unsigned build: if macOS blocks it, right-click start.command → Open,
# or run: xattr -dr com.apple.quarantine "<this folder>"
cd "$(dirname "$0")" || exit 1
exec ./skinloom up --open "$@"
