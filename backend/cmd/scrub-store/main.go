// Command scrub-store removes credentials from a production database clone
// before that clone is opened by a local development server.
package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/wago-org/registry-backend/internal/store"
)

func main() {
	source := flag.String("source", "", "path to the downloaded Pebble store")
	dest := flag.String("dest", "", "path for the sanitized Pebble clone")
	flag.Parse()
	if *source == "" || *dest == "" {
		log.Fatal("-source and -dest are required")
	}
	report, err := store.WriteSanitizedClone(*source, *dest)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("scrubbed %d users and removed %d API tokens\n", report.Users, report.APITokens)
}
