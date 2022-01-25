#!/bin/bash

usage() {
  cat << EOF

Generate an ODF-to-ODF onboarding ticket to STDOUT
USAGE: $0 <private_key_file>"

Example of how to generate a new private/public key pair:
  openssl genrsa -out key.pem 4096
  openssl rsa -in key.pem -out pubkey.pem -outform PEM -pubout

EOF
}

if [ $# == 0 ]; then
  echo "Missing argument for key file!"
  usage
  exit 1
fi

if [[ "${1}" == "-h" ]] || [[ "${1}" == "--help" ]]; then
  usage
  exit 0
fi

KEY_FILE="${1}"
if [[ ! -f "${KEY_FILE}" ]]; then
  echo "Key file '${KEY_FILE}' not found!"
  usage
  exit 1
fi

# In case the system doesn't have uuidgen, fall back to /dev/urandom
NEW_CONSUMER_ID="$(uuidgen || (tr -dc 'a-zA-Z0-9' < /dev/urandom | fold -w 36 | head -n 1) || echo "00000000-0000-0000-0000-000000000000")"

declare -A DATA
DATA=(
  ["id"]="${NEW_CONSUMER_ID}"
  ["expirationDate"]="$(( $(date +%s) + 172800 ))"
)

JSON="{"
for k in "${!DATA[@]}"; do
  JSON+="\"$k\":\"${DATA[$k]}\","
done
JSON="${JSON:0:-1}}"

PAYLOAD="$(echo -n "${JSON}" | base64 -w 0)"
SIG="$(echo -n "${JSON}"| openssl dgst -sign "${KEY_FILE}" | base64 -w 0)"
cat <<< "${PAYLOAD}.${SIG}"
