// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lusoris/SnatchArr/tools/upstreamspec/contract"
)

const otherCommit = "89abcdef0123456789abcdef0123456789abcdef"

func TestReceipts(t *testing.T) {
	t.Parallel()
	pins := []pin{{Name: "first", Commit: someCommit}, {Name: "second", Commit: otherCommit}}
	tests := []struct {
		name string
		// left maps a spec to the content of its receipt; a spec that is absent has none.
		left map[string]string
		want []string
	}{
		{"a receipt of the pinned commit for every pin", map[string]string{"first": someCommit + "\n", "second": otherCommit + "\n"}, nil},
		{"a receipt without a line end", map[string]string{"first": someCommit, "second": otherCommit}, nil},
		{"no receipt at all", nil, []string{
			"2 pinned spec(s) without a passed contract test",
			"first: no contract test left a receipt", "second: no contract test left a receipt",
		}},
		{"one test that did not run", map[string]string{"first": someCommit + "\n"}, []string{
			"1 pinned spec(s) without a passed contract test", "second: no contract test left a receipt",
		}},
		{"a receipt of another commit", map[string]string{"first": otherCommit + "\n", "second": otherCommit + "\n"}, []string{
			`first: the receipt is for commit "` + otherCommit + `", the manifest pins ` + someCommit,
		}},
		{"an empty receipt", map[string]string{"first": "", "second": otherCommit + "\n"}, []string{
			`first: the receipt is for commit "", the manifest pins ` + someCommit,
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cache := t.TempDir()
			for spec, content := range test.left {
				writeFile(t, filepath.Join(cache, contract.ReceiptName(spec)), content)
			}
			var out bytes.Buffer
			err := receipts(cache, pins, &out)
			if len(test.want) == 0 {
				if err != nil || !strings.Contains(out.String(), "each of 2 pinned spec(s) passed") {
					t.Fatalf("receipts = %q, %v; want the summary", out.String(), err)
				}
				return
			}
			if err == nil {
				t.Fatal("receipts accepted a pin without a passed contract test")
			}
			for _, part := range test.want {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("error %q lacks %q", err, part)
				}
			}
		})
	}
}

func TestReceiptsRefusesAReceiptItCannotRead(t *testing.T) {
	t.Parallel()
	cache := t.TempDir()
	// A directory in the place of the receipt is neither absent nor readable.
	writeFile(t, filepath.Join(cache, contract.ReceiptName("first"), "inside"), "x")
	err := receipts(cache, []pin{{Name: "first", Commit: someCommit}}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "first: read receipt") {
		t.Errorf("receipts = %v, want a read error", err)
	}
}

func TestFetchRemovesEarlierReceipts(t *testing.T) {
	t.Parallel()
	body := []byte(sampleSpec)
	up := serve(t, body)
	cache := t.TempDir()
	p := fetchPin(body)
	receipt := filepath.Join(cache, contract.ReceiptName(p.Name))
	writeFile(t, receipt, p.Commit+"\n")

	if _, err := fetchInto(t, up.URL, cache, p); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	for _, name := range entries(t, cache) {
		if name == filepath.Base(receipt) {
			t.Error("fetch kept the receipt of an earlier run")
		}
	}
	if err := receipts(cache, []pin{p}, &bytes.Buffer{}); err == nil {
		t.Error("receipts accepted a pin whose test did not run after the fetch")
	}
}
