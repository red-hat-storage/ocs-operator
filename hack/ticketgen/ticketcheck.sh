#!/bin/bash

usage() {
  echo "USAGE: $0 <public_key_file> [<ticket_file>]"
}

if [ $# == 0 ]; then
  echo "Missing argument for key file!"
  usage
  exit 1
fi

KEY_FILE="${1}"
if [[ ! -f "${KEY_FILE}" ]]; then
  echo "Key file '${KEY_FILE}' not found!"
  usage
  exit 1
fi

TICKET_FILE="${2:-onboarding_ticket.txt}"
if [[ ! -f "${TICKET_FILE}" ]]; then
  echo "Ticket file '${TICKET_FILE}' not found!"
  usage
  exit 1
fi

TICKET="$(cat "${TICKET_FILE}")"

IFS='.' read -ra TICKET_ARR <<< "${TICKET}"
PAYLOAD="${TICKET_ARR[0]}"
SIG="${TICKET_ARR[1]}"

SIG_FILE="$(mktemp)"

JSON="$(echo "${PAYLOAD}" | base64 -d)"
echo "${JSON}"
echo -n "${SIG}" | base64 -d > "${SIG_FILE}"

echo -n "${JSON}" | openssl dgst -verify "${KEY_FILE}" -signature "${SIG_FILE}"

rm "${SIG_FILE}"
