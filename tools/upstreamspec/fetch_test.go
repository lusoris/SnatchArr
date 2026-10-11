// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/tools/upstreamspec/contract"
)

// upstream serves body at the address of the sample pin and counts the requests it answers.
type upstream struct {
	*httptest.Server
	requests atomic.Int32
}

func serve(t *testing.T, body []byte) *upstream {
	t.Helper()
	var up upstream
	up.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up.requests.Add(1)
		if r.URL.Path != "/Owner/Project/"+someCommit+"/src/openapi.yaml" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(up.Close)
	return &up
}

func fetchPin(body []byte) pin {
	return pin{
		Name: "sample", Repository: "Owner/Project", Tag: "v1", Commit: someCommit, Path: "src/openapi.yaml",
		SHA256: digest(body), License: "MIT", Prefix: "/api/v1", Operations: []string{"GET /api/v1/status"},
	}
}

func fetchInto(t *testing.T, base, cache string, p pin) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out bytes.Buffer
	err := fetcher{client: &http.Client{Timeout: 10 * time.Second}, base: base, cache: cache, out: &out}.all(ctx, []pin{p})
	return out.String(), err
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read %s: %v", dir, err)
	}
	names := make([]string, 0, len(list))
	for _, item := range list {
		names = append(names, item.Name())
	}
	return names
}

func TestFetchDownloadsThenReusesTheCache(t *testing.T) {
	t.Parallel()
	body := []byte(sampleSpec)
	up := serve(t, body)
	cache := filepath.Join(t.TempDir(), "nested", "cache")
	p := fetchPin(body)

	out, err := fetchInto(t, up.URL, cache, p)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if want := "upstreamspec: sample v1 (0123456789ab): 1 operation(s), spec downloaded\n"; out != want {
		t.Errorf("summary = %q, want %q", out, want)
	}
	stored, err := os.ReadFile(filepath.Join(cache, "sample.spec"))
	if err != nil || !bytes.Equal(stored, body) {
		t.Fatalf("cached spec differs from the served one (%v)", err)
	}
	file, err := contract.Read(cache, "sample")
	if err != nil {
		t.Fatalf("contract: %v", err)
	}
	if _, ok := file.Operations["GET /api/v1/status"]; !ok || file.Tag != "v1" || file.Commit != someCommit {
		t.Errorf("contract = %+v", file)
	}

	out, err = fetchInto(t, up.URL, cache, p)
	if err != nil || !strings.HasSuffix(out, "spec cached\n") {
		t.Errorf("second fetch = %q, %v; want the cached spec", out, err)
	}
	if got := up.requests.Load(); got != 1 {
		t.Errorf("upstream answered %d request(s), want 1", got)
	}

	// A cached file that no longer has the pinned checksum is not trusted: it is fetched again.
	writeFile(t, filepath.Join(cache, "sample.spec"), "paths: {}\n")
	out, err = fetchInto(t, up.URL, cache, p)
	if err != nil || !strings.HasSuffix(out, "spec downloaded\n") {
		t.Errorf("fetch over a changed cache file = %q, %v; want a download", out, err)
	}
	if stored, err := os.ReadFile(filepath.Join(cache, "sample.spec")); err != nil || !bytes.Equal(stored, body) {
		t.Errorf("the changed cache file was not replaced (%v)", err)
	}
}

func TestFetchRefusesAnotherChecksum(t *testing.T) {
	t.Parallel()
	body := []byte(sampleSpec)
	up := serve(t, append(bytes.Clone(body), '\n'))
	cache := filepath.Join(t.TempDir(), "cache")
	p := fetchPin(body)

	_, err := fetchInto(t, up.URL, cache, p)
	if err == nil {
		t.Fatal("a file with another checksum was accepted")
	}
	for _, part := range []string{
		"sample: src/openapi.yaml at commit " + someCommit,
		"has sha256 " + digest(append(bytes.Clone(body), '\n')),
		"the manifest pins " + p.SHA256,
		"nothing was written",
	} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("error %q lacks %q", err, part)
		}
	}
	if left := entries(t, cache); len(left) != 0 {
		t.Errorf("the refused download left %q in the cache", left)
	}
}

func TestFetchRefuses(t *testing.T) {
	t.Parallel()
	body := []byte(sampleSpec)
	large := bytes.Repeat([]byte("#"), maxSpecBytes+1)
	tests := []struct {
		name   string
		served []byte
		change func(*pin)
		want   string
	}{
		{"a file the upstream lacks", body, func(p *pin) { p.Path = "src/absent.yaml" }, "HTTP 404"},
		{"a file above the size bound", large, func(p *pin) { p.SHA256 = digest(large) }, "is larger than"},
		{"a pinned file that is no OpenAPI document", []byte("openapi: 3.0.1\n"), func(p *pin) { p.SHA256 = digest([]byte("openapi: 3.0.1\n")) }, "src/openapi.yaml: the document has no paths"},
		{"an operation the pinned spec lacks", body, func(p *pin) { p.Operations = []string{"GET /api/v1/absent"} }, "sample: GET /api/v1/absent: the spec has no such path"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			up := serve(t, test.served)
			cache := filepath.Join(t.TempDir(), "cache")
			p := fetchPin(body)
			test.change(&p)
			_, err := fetchInto(t, up.URL, cache, p)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("fetch = %v, want an error with %q", err, test.want)
			}
			for _, name := range entries(t, cache) {
				if name == contract.FileName("sample") {
					t.Errorf("the refused spec left a contract in the cache")
				}
			}
		})
	}
}

func TestFetchBounds(t *testing.T) {
	t.Parallel()
	// Boundary: a file of exactly the size bound is read in full and judged by its checksum.
	body := append([]byte(sampleSpec), bytes.Repeat([]byte("#"), maxSpecBytes-len(sampleSpec))...)
	up := serve(t, body)
	if _, err := fetchInto(t, up.URL, filepath.Join(t.TempDir(), "cache"), fetchPin(body)); err != nil {
		t.Errorf("a file of %d bytes was refused: %v", len(body), err)
	}

	// An upstream that does not answer ends with the deadline of the caller.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := fetcher{client: http.DefaultClient, base: up.URL, cache: t.TempDir(), out: &bytes.Buffer{}}.all(ctx, []pin{fetchPin(body)})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("fetch with an ended context = %v, want context.Canceled", err)
	}
}

func TestRun(t *testing.T) {
	t.Parallel()
	body := []byte(sampleSpec)
	up := serve(t, body)
	dir := t.TempDir()
	manifest := filepath.Join(dir, "specs.yaml")
	writeFile(t, manifest, entry(map[string]string{
		"path":       "path: src/openapi.yaml",
		"sha256":     "sha256: " + digest(body),
		"operations": "operations: [GET /api/v1/status]",
	}))
	cache := filepath.Join(dir, "cache")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var out bytes.Buffer
	if err := run(ctx, []string{"fetch", "-manifest", manifest, "-cache", cache}, &out, up.URL); err != nil {
		t.Fatalf("run fetch: %v", err)
	}
	if !strings.Contains(out.String(), "sample v1.2.3") {
		t.Errorf("summary = %q", out.String())
	}
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no command", nil, "usage: upstreamspec"},
		{"an unknown command", []string{"publish", "-manifest", manifest}, "usage: upstreamspec"},
		{"an unknown flag", []string{"fetch", "-force"}, "flag provided but not defined"},
		{"a stray argument", []string{"fetch", "-manifest", manifest, "extra"}, "usage: upstreamspec"},
		{"an absent manifest", []string{"fetch", "-manifest", filepath.Join(dir, "absent.yaml")}, "read manifest"},
		{"receipts before a contract test ran", []string{"receipts", "-manifest", manifest, "-cache", cache}, "sample: no contract test left a receipt"},
		{"a guard outside a repository", []string{"guard", "-manifest", manifest, "-cache", cache, "-repo", filepath.Join(dir, "elsewhere")}, "is not inside the repository"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := run(t.Context(), test.args, &bytes.Buffer{}, up.URL)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("run = %v, want an error with %q", err, test.want)
			}
		})
	}
}
