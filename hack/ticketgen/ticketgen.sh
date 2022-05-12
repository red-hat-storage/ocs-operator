#!/bin/bash

usage() {
  cat << EOF

Generate an OCS Provider/Consumer onboarding ticket to STDOUT
USAGE: $0 [-h] <private_key_file>

private_key_file:
    A file containing a valid RSA private key.

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
EXPIRATION_DATE="$(( $(date +%s) + 172800 ))"

declare JSON
add_var() {
  if [[ -n "${JSON}" ]]; then
    JSON+=","
  fi

  JSON+="$(printf '"%s":"%s"' "${1}" "${2}")"
}

## Add ticket values here
add_var "id" "${NEW_CONSUMER_ID}"
add_var "expirationDate" "${EXPIRATION_DATE}"

PAYLOAD="$(echo -n "{${JSON}}" | base64 | tr -d "\n")"
SIG="$(echo -n "{${JSON}}"| openssl dgst -sha256 -sign "${KEY_FILE}" | base64 | tr -d "\n")"
cat <<< "${PAYLOAD}.${SIG}"
