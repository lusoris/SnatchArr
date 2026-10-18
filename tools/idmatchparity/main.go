// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Command idmatchparity writes the daily parity row of the id_matched shadow week (ADR-0014).
//
// During the week SnatchArr runs rule id_matched in observe mode next to the cluster's import
// unblocker, which still imports. The command reads both logs and compares, for one UTC day,
// the downloads SnatchArr would import with the downloads the unblocker imported:
//
//	idmatchparity -day 2026-10-19 -snatcharr api.log -unblocker unblocker.log
//
// The SnatchArr log holds the api's JSON lines, the unblocker log the lines of
// `kubectl logs --timestamps`. Both may span the day and the slack around it. The row is
// printed as one line of key=value fields, each mismatch as a line of its own. The exit status
// is 0 for a green day, 1 for a red one and 2 when an input cannot be read.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

const (
	exitRed   = 1
	exitInput = 2
)

func main() {
	code, err := run(os.Args[1:], os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "idmatchparity:", err)
	}
	os.Exit(code)
}

// run returns the exit status, and the error behind an exitInput status.
func run(args []string, stdout, stderr io.Writer) (int, error) {
	fs := flag.NewFlagSet("idmatchparity", flag.ContinueOnError)
	fs.SetOutput(stderr)
	day := fs.String("day", "", "UTC day to compare, YYYY-MM-DD")
	snatchPath := fs.String("snatcharr", "", "SnatchArr api log, JSON lines")
	unblockerPath := fs.String("unblocker", "", "unblocker log, kubectl logs --timestamps")
	slack := fs.Duration("slack", 2*time.Hour, "how far apart the two sides of one download may lie")
	if err := fs.Parse(args); err != nil {
		return exitInput, fmt.Errorf("flags: %w", err)
	}
	in, err := load(*day, *snatchPath, *unblockerPath)
	if err != nil {
		return exitInput, err
	}
	in.slack = *slack
	r := compare(in)
	if err := r.write(stdout); err != nil {
		return exitInput, err
	}
	if r.red() {
		return exitRed, nil
	}
	return 0, nil
}

func load(day, snatchPath, unblockerPath string) (input, error) {
	start, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return input{}, fmt.Errorf("-day: %w", err)
	}
	if snatchPath == "" || unblockerPath == "" {
		return input{}, errors.New("-snatcharr and -unblocker are required")
	}
	in := input{day: start}
	if err := readFile(snatchPath, func(r io.Reader) (perr error) {
		in.snatch, perr = parseSnatchArr(r)
		return perr
	}); err != nil {
		return input{}, err
	}
	if err := readFile(unblockerPath, func(r io.Reader) (perr error) {
		in.unblocker, perr = parseUnblocker(r)
		return perr
	}); err != nil {
		return input{}, err
	}
	return in, nil
}

func readFile(path string, parse func(io.Reader) error) (err error) {
	// #nosec G304 -- the log path is the caller's argument; the file is only parsed
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer func() {
		if cErr := f.Close(); cErr != nil && err == nil {
			err = fmt.Errorf("close %s: %w", path, cErr)
		}
	}()
	if err := parse(f); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
