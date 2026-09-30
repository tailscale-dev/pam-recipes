#!/usr/bin/env bash
#
# Set or delete a custom device posture attribute.
#
# Custom posture attributes can only be written through the API; there is no
# way to set one in the admin console. You can see the result there though, on
# the Machine Details page for the device.
#
# This approach of setting attributes should not be used in a production deployment.
# These would be written by your MDM at enrolment, or baked in at
# provisioning time with an OAuth device-provisioning key so that the user
# cannot set them on their own machine. This script is for trying the mechanic
# out by hand.
#
# Usage:
#   ./set-posture-attribute.sh deviceOwnership corporate
#   ./set-posture-attribute.sh --expiry 2026-10-01T09:00:00Z deviceOwnership corporate
#   ./set-posture-attribute.sh --device nroUUDGqX111CNTRL deviceOwnership byod
#   ./set-posture-attribute.sh --delete deviceOwnership
#
# Requires: curl, jq, and (unless you pass --device) the tailscale CLI.
# Custom attributes require a Premium or Enterprise plan.

set -euo pipefail

API="https://api.tailscale.com/api/v2"

device=""
expiry=""
delete=false
force_string=false

# Print the comment header above, up to the first non-comment line.
usage() {
	awk 'NR>2 { if (!/^#/) exit; sub(/^# ?/, ""); print }' "$0"
	exit "${1:-0}"
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--device)  device="$2"; shift 2 ;;
		--expiry)  expiry="$2"; shift 2 ;;
		--delete)  delete=true; shift ;;
		--string)  force_string=true; shift ;;
		-h|--help) usage ;;
		-*)        echo "unknown option: $1" >&2; usage 1 ;;
		*)         break ;;
	esac
done

if $delete; then
	[[ $# -eq 1 ]] || usage 1
else
	[[ $# -eq 2 ]] || usage 1
fi

# User-managed attributes live in the `custom` namespace, so add the prefix if
# it was left off. Keys are limited to letters, numbers, underscores and
# colons, and to 128 characters including the namespace.
attribute="$1"
[[ $attribute == custom:* ]] || attribute="custom:${attribute}"

if ! [[ $attribute =~ ^custom:[A-Za-z0-9_]+$ ]] || (( ${#attribute} > 128 )); then
	echo "invalid attribute name: ${attribute}" >&2
	echo "keys may contain letters, numbers and underscores, max 128 chars" >&2
	exit 1
fi

for cmd in curl jq; do
	command -v "$cmd" >/dev/null || { echo "${cmd} is required" >&2; exit 1; }
done

# Default to whichever device we are running on. The API wants a nodeId (the
# n...CNTRL form) or the legacy numeric id — a hostname will not work, and
# fails with an unhelpful "internal server error".
if [[ -z $device ]]; then
	command -v tailscale >/dev/null || { echo "tailscale CLI not found; pass --device" >&2; exit 1; }
	device=$(tailscale status --json | jq -r '.Self.ID // empty')
	[[ -n $device ]] || { echo "could not determine this device's nodeId; pass --device" >&2; exit 1; }
fi

# Prompt rather than taking the key as an argument, so it stays out of the
# shell history.
if [[ -n ${TS_API_KEY:-} ]]; then
	api_key="$TS_API_KEY"
else
	read -rsp "Tailscale API access token: " api_key
	echo
fi
[[ -n $api_key ]] || { echo "no API access token given" >&2; exit 1; }

# Feed the credential to curl as a config file on stdin instead of passing it
# with -u, which would expose it to anyone running ps while this is in flight.
api() {
	local method="$1" path="$2" body="${3:-}"
	local args=(--silent --show-error --write-out '\n%{http_code}'
	            --request "$method" "${API}${path}")
	[[ -n $body ]] && args+=(--header 'Content-Type: application/json' --data-binary "$body")

	printf 'user = "%s:"\n' "$api_key" | curl --config - "${args[@]}"
}

if $delete; then
	echo "deleting ${attribute} from ${device}"
	response=$(api DELETE "/device/${device}/attributes/${attribute}")
else
	# The value type for a key is fixed tailnet-wide by its first write, so be
	# deliberate about it: `true` and `87` are sent as a boolean and a number,
	# anything else as a string. Use --string to force quoting.
	if ! $force_string && [[ $2 =~ ^(true|false)$ ]]; then
		value=$(jq -n --argjson v "$2" '$v'); value_type=boolean
	elif ! $force_string && [[ $2 =~ ^-?[0-9]+(\.[0-9]+)?$ ]]; then
		value=$(jq -n --argjson v "$2" '$v'); value_type=number
	else
		value=$(jq -n --arg v "$2" '$v'); value_type=string
	fi

	body=$(jq -n --argjson value "$value" --arg expiry "$expiry" \
		'{value: $value} + (if $expiry == "" then {} else {expiry: $expiry} end)')

	echo "setting ${attribute} = ${value} (${value_type}) on ${device}"
	response=$(api POST "/device/${device}/attributes/${attribute}" "$body")
fi

status="${response##*$'\n'}"
if [[ $status != 2* ]]; then
	echo "request failed (HTTP ${status}): ${response%$'\n'*}" >&2
	exit 1
fi

# A successful write currently returns a null body rather than the updated
# attributes, so read them back rather than trusting the response.
# https://github.com/tailscale/tailscale/issues/18253
echo
echo "posture attributes now on ${device}:"
attrs=$(api GET "/device/${device}/attributes")
echo "${attrs%$'\n'*}" | jq '.attributes'
