package main

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"flag"
	"fmt"
	"os"
	"strings"
)

// TODO: Ticket struct??
func main() {
	keyfileStr := flag.String("keyfile", "", "path to public key file")
	ticketfileStr := flag.String("ticket", "onboarding_ticket.txt", "path to onboarding ticket file")
	flag.Parse()
	if *keyfileStr == "" {
		print("ERROR: '-keyfile' is a required argument\n")
		flag.Usage()
		os.Exit(1)
	}

	fmt.Printf("Reading key from: %s\n", *keyfileStr)
	keyfile, err := os.ReadFile(*keyfileStr)
	if err != nil {
		panic(err)
	}
	pemBlock, _ := pem.Decode(keyfile)
	pubKey, err := x509.ParsePKCS1PublicKey(pemBlock.Bytes)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Reading ticket from: %s\n", *ticketfileStr)
	ticketData, err := os.ReadFile(*ticketfileStr)
	if err != nil {
		panic(err)
	}
	ticketArr := strings.Split(string(ticketData), ".")

	payload, err := base64.StdEncoding.DecodeString(ticketArr[0])
	if err != nil {
		panic(err)
	}
	sig, err := base64.StdEncoding.DecodeString(ticketArr[1])
	if err != nil {
		panic(err)
	}

	hash := sha256.Sum256(payload)

	err = rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hash[:], sig)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Printf("TICKET: %s\n", string(ticketData))
	fmt.Printf("PAYLOAD: %s\n\n", string(payload))

	println("Successfully validated data")
	os.Exit(0)
}
