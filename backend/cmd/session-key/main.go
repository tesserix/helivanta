// Command session-key prints a fresh SESSION_SIGNING_KEY value.
//
// Deliberately prints the key and nothing else — no label, no
// newline-delimited preamble — so it can be piped straight into a
// secret store without a human copying it out of decorated output and
// introducing whitespace SessionSigningKeySeed would then trim or
// refuse.
package main

import (
	"fmt"
	"os"

	"github.com/tesserix/hms/internal/config"
)

func main() {
	key, err := config.GenerateSessionSigningKey()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(key)
}
