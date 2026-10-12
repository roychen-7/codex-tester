#!/bin/sh
set -eu
# Refresh the dependency volume when the lockfile changes.
pnpm install --frozen-lockfile
exec "$@"
