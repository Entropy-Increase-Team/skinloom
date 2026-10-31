#!/bin/sh
# SkinLoom (云织) start script for Linux.
# Starts the backend and the bundled frontend on one port (default :17546).
# Data (database, config, uploads) is kept in ./data. To run as a service:
#   sudo ./skinloom service install && sudo ./skinloom service start
cd "$(dirname "$0")" || exit 1
exec ./skinloom up "$@"
