#!/usr/bin/env bash
# suggest-address is allowed to fail — offline, slow, or asked nothing — but
# it must always leave a well-formed answer behind, because the panel reads
# the file it writes and an unreadable one would freeze the last list on
# screen. No test here touches the network.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

out="$tmp/location-suggest.json"

read_field() {
  python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))[sys.argv[2]])' "$out" "$1"
}

# Nothing asked: an empty answer, not a stale one.
MAILBOX_CLOCK_CACHE_DIR="$tmp" "$root/suggest-address" "" >/dev/null
[[ "$(read_field query)" == "" ]] || { echo "empty query not echoed back" >&2; exit 1; }
[[ "$(read_field features)" == "[]" ]] || { echo "empty query returned features" >&2; exit 1; }

# Offline: the query still comes back so the panel can match it, with no rows.
https_proxy="http://127.0.0.1:9" HTTPS_PROXY="http://127.0.0.1:9" \
  MAILBOX_CLOCK_CACHE_DIR="$tmp" "$root/suggest-address" "Garbe Biergarten" >/dev/null
[[ "$(read_field query)" == "Garbe Biergarten" ]] || { echo "query lost on failure" >&2; exit 1; }
[[ "$(read_field features)" == "[]" ]] || { echo "offline run invented rows" >&2; exit 1; }
[[ "$(read_field country)" == "DE" ]] || { echo "home country not recorded" >&2; exit 1; }

# The home country is the panel's sort key, so it must travel with the reply.
MAILBOX_CLOCK_SUGGEST_COUNTRY=AT https_proxy="http://127.0.0.1:9" HTTPS_PROXY="http://127.0.0.1:9" \
  MAILBOX_CLOCK_CACHE_DIR="$tmp" "$root/suggest-address" "Prater" >/dev/null
[[ "$(read_field country)" == "AT" ]] || { echo "MAILBOX_CLOCK_SUGGEST_COUNTRY ignored" >&2; exit 1; }

# The write is atomic: no temp files left in the cache dir.
leftovers="$(find "$tmp" -name 'location-suggest.*' -not -name 'location-suggest.json' | wc -l)"
[[ "$leftovers" == "0" ]] || { echo "left $leftovers temp files behind" >&2; exit 1; }

echo "SUGGEST-OK"
