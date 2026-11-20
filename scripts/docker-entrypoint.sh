#!/bin/sh
set -eu

# Initialize only an absent config. The writable directory permits atomic saves
# from Settings; existing operator configuration is never overwritten.
if [ "$#" -eq 0 ]; then
    mkdir -p /app/runtime
    if [ ! -e /app/runtime/config.yaml ]; then
        umask 077
        cp /usr/local/share/cyberstrike-config.yaml /app/runtime/config.yaml
    fi
    set -- --config /app/runtime/config.yaml
fi
exec cyberstrike-ai "$@"
