// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Command idmatchparity writes the daily parity row of the id_matched shadow week (ADR-0014).
//
// During the week SnatchArr runs rule id_matched in observe mode next to the cluster's import
// unblocker, which still imports. The command reads both logs and compares, for one UTC day,
// the downloads SnatchArr would import with the downloads the unblocker imported:
//
//	idmatchparity -day 2026-10-19 -snatcharr api.log -unblocker unblocker.log -out 2026-10-19.txt
//
// Optional -out writes the same output to a file. Build the idmatchparity image target with
// `docker build --target idmatchparity -t idmatchparity .`, then mount logs and evidence:
//
//	docker run --rm --read-only -v <logs>:/in:ro -v <evidence>:/out idmatchparity -day D -snatcharr /in/api.log -unblocker /in/unblocker.log -out /out/D.txt
//
// The SnatchArr log holds the api's JSON lines, the unblocker log the lines of
// `kubectl logs --timestamps`. Both may span the day and the slack around it. The row is
// printed as one line of key=value fields, each mismatch as a line of its own. The exit status
// is 0 for a green day, 1 for a red one and 2 when an input cannot be read, either log's
// timestamp span does not overlap the requested day, or the output file cannot be written.
package main

import (
	"bytes"
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
	outPath := fs.String("out", "", "also write the output to this file")
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
	if err := writeOutput(r, stdout, *outPath); err != nil {
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
	if err := in.snatch.span.check(start, "-snatcharr", snatchPath); err != nil {
		return input{}, err
	}
	if err := readFile(unblockerPath, func(r io.Reader) (perr error) {
		in.unblocker, perr = parseUnblocker(r)
		return perr
	}); err != nil {
		return input{}, err
	}
	if err := in.unblocker.span.check(start, "-unblocker", unblockerPath); err != nil {
		return input{}, err
	}
	return in, nil
}

func (s *logSpan) check(start time.Time, flagName, path string) error {
	if !s.seen || s.last.Before(start) || !s.first.Before(start.Add(day)) {
		return fmt.Errorf("%s log %s: no lines on %s", flagName, path, start.Format(time.DateOnly))
	}
	return nil
}

func writeOutput(r row, stdout io.Writer, path string) error {
	var buf bytes.Buffer
	w := stdout
	if path != "" {
		w = io.MultiWriter(stdout, &buf)
	}
	if err := r.write(w); err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	// #nosec G306 -- parity evidence contains no secrets and permits shared read access
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write output %s: %w", path, err)
	}
	return nil
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
