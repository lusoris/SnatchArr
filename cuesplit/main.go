// SPDX-FileCopyrightText: 2026 lusoris
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func usage(w io.Writer) {
	fmt.Fprintf(w, "Usage: cuesplit split --out <dir> [--json] <input>\n\n")
	fmt.Fprintf(w, "Split a lossless audio image into individual tracks using a CUE sheet.\n\n")
	fmt.Fprintf(w, "Arguments:\n")
	fmt.Fprintf(w, "  <input>    A release directory, a .cue file, or a .flac file with embedded cuesheet\n\n")
	fmt.Fprintf(w, "Options:\n")
	fmt.Fprintf(w, "  --out, -o  Destination directory where the album directory will be created\n")
	fmt.Fprintf(w, "  --json, -j Output split metadata and verification result as JSON on stdout\n")
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	return runWithIO(args, os.Stdout, os.Stderr)
}

func runWithIO(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}

	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		usage(stdout)
		return 0
	}

	if args[0] != "split" {
		fmt.Fprintf(stderr, "cuesplit: unknown subcommand %q\n", args[0])
		usage(stderr)
		return 2
	}

	splitArgs := args[1:]
	fs := flag.NewFlagSet("split", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var outDir string
	var jsonOutput bool

	fs.StringVar(&outDir, "out", "", "Output directory")
	fs.StringVar(&outDir, "o", "", "Output directory (shorthand)")
	fs.BoolVar(&jsonOutput, "json", false, "Print JSON summary on stdout")
	fs.BoolVar(&jsonOutput, "j", false, "Print JSON summary on stdout (shorthand)")

	if err := fs.Parse(splitArgs); err != nil {
		return 2
	}

	if outDir == "" {
		fmt.Fprintf(stderr, "cuesplit: --out directory is required\n")
		return 2
	}

	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintf(stderr, "cuesplit: expected exactly one <input>, got %d\n", len(rest))
		return 2
	}
	inputPath := rest[0]

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// 1. Discover input
	discovered, err := Discover(inputPath)
	if err != nil {
		fmt.Fprintf(stderr, "cuesplit: %v\n", err)
		if IsRefusal(err) {
			return 2
		}
		return 1
	}

	// 2. Probe audio
	audioInfo, err := ProbeAudio(ctx, discovered.AudioPath)
	if err != nil {
		fmt.Fprintf(stderr, "cuesplit: %v\n", err)
		if IsRefusal(err) {
			return 2
		}
		return 1
	}

	// 3. Split, encode, and verify
	_, err = Split(ctx, discovered, audioInfo, outDir, jsonOutput, stdout, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "cuesplit: %v\n", err)
		if IsRefusal(err) {
			return 2
		}
		return 1
	}

	return 0
}
