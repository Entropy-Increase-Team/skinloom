#!/bin/sh
# SkinLoom (云织) start script for Linux.
# Starts the backend and the bundled frontend on one port (default :8080).
# Data (database, config, uploads) is kept in ./data. To run as a service:
#   sudo ./skinloom service install   (arrives in M4; until then use the systemd unit in the docs)
cd "$(dirname "$0")" || exit 1
exec ./skinloom up "$@"
