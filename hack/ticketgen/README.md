# ticketgen

`ticketgen.sh` is a utility script to generate a valid onboarding ticket for OCS
Provider/Consumer Mode. It expects the user to have already generated a
public/private RSA key pair to use for signing and validating the onboarding
ticket. This directory also includes some example implementations of ticket
validation as both a shell script and a Go program.

The following files are included in this directory, run with `-h` to see the
full usage message:

* `ticketgen.sh` - Generates a new random Consumer ID with an expiration of 48
  hours after the ID was generated. It places these values in a JSON struct,
  generates a signature using the provided private key file, then concatenates
  them as base64-encoded strings. The output is sent to STDOUT.
* `ticketcheck.sh` - A sample script showing how to validate an onboarding
  ticket (as output by `ticketgen.sh`) using `openssl dgst` with the provided
  public key file. It expects the onboarding ticket to be provided as a plain
  file, defaulting to a file named `onboarding_ticket.txt` in the same
  directory.
* `ticketcheck.go` - Another example of onboarding ticket validation,
  implemented in Go to showcase the required imports and functions.