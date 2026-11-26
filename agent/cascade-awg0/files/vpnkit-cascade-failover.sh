#!/bin/bash
set -euo pipefail

peer=$1
probe_ip=$2
ipv4=$3
ipv6=$4

endpoints=$(awg show awg0 endpoints 2>/dev/null) || exit 0

reachable() {
  local attempt
  for ((attempt = 0; attempt < $1; attempt++)); do
    if ping -4 -I awg0 -q -c 1 -W 1 "$probe_ip" >/dev/null 2>&1; then
      return 0
    fi
  done
  return 1
}

if reachable 3; then
  exit 0
fi

current=$(awk -v key="$peer" '$1 == key {print $2}' <<<"$endpoints")
[ -n "$current" ] || exit 1
if [[ "$current" == \[* ]]; then
  next=$ipv4
else
  next=$ipv6
fi
awg set awg0 peer "$peer" endpoint "$next"
if reachable 8; then
  printf 'Krot cascade switched endpoint to %s\n' "$next"
else
  # Leave the alternate endpoint active until the next timer tick to allow a delayed handshake.
  printf 'Krot cascade has no response after switching endpoint to %s\n' "$next" >&2
  exit 1
fi
