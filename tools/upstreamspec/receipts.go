// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/lusoris/SnatchArr/tools/upstreamspec/contract"
)

// receipts checks that the contract test of every pin passed against the pinned commit. A passing
// test leaves `<name>.checked` with that commit in the cache: the Go tests through
// contract.Passed, the Rust tests of `arr-client` with the same file. `fetch` removes the
// receipts, so one from an earlier run does not count.
//
// Without this step a contract test that is skipped, filtered out by its name or deleted would
// leave `make contracts-verify` green.
func receipts(cache string, pins []pin, out io.Writer) error {
	var problems []string
	for _, p := range pins {
		// #nosec G304 -- the cache directory is the caller's argument; the file is only compared
		data, err := os.ReadFile(filepath.Join(cache, contract.ReceiptName(p.Name)))
		switch got := strings.TrimSpace(string(data)); {
		case errors.Is(err, fs.ErrNotExist):
			problems = append(problems, p.Name+": no contract test left a receipt; it did not run or did not pass")
		case err != nil:
			return fmt.Errorf("%s: read receipt: %w", p.Name, err)
		case got != p.Commit:
			problems = append(problems, fmt.Sprintf("%s: the receipt is for commit %q, the manifest pins %s", p.Name, got, p.Commit))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d pinned spec(s) without a passed contract test:\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
	if _, err := fmt.Fprintf(out, "upstreamspec: the contract test of each of %d pinned spec(s) passed against its pinned commit\n", len(pins)); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}
	return nil
}
