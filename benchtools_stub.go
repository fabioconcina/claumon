//go:build !benchtools

package main

import (
	"fmt"
	"os"
)

// The `bench` and `diagnostics` subcommands are development/research tooling.
// They are excluded from the default (release) build to keep the shipped
// binary small. Rebuild with the benchtools tag to enable them:
//
//	go build -tags benchtools .   (or: make build-benchtools)
func runBench()       { benchtoolsDisabled("bench") }
func runDiagnostics() { benchtoolsDisabled("diagnostics") }

func benchtoolsDisabled(cmd string) {
	fmt.Fprintf(os.Stderr, "%q is unavailable in this build; rebuild with: go build -tags benchtools .\n", cmd)
	os.Exit(2)
}
