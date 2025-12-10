package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: devicefinder discover")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "discover":
		if err := startDeviceDiscovery(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		fmt.Fprintln(os.Stderr, "Usage: devicefinder discover")
		os.Exit(1)
	}
}
