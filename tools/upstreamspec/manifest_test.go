// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	someCommit = "0123456789abcdef0123456789abcdef01234567"
	someSHA256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// entryLines writes lines as the first list entry under "specs:" and replaces "<key>: <value>"
// lines by key from change; an empty replacement drops the line.
func entryLines(lines []string, change map[string]string) string {
	var out strings.Builder
	out.WriteString("specs:\n")
	for i, line := range lines {
		key, _, _ := strings.Cut(line, ":")
		if replaced, ok := change[key]; ok {
			line = replaced
		}
		if line == "" {
			continue
		}
		lead := "    "
		if i == 0 {
			lead = "  - "
		}
		out.WriteString(lead + line + "\n")
	}
	return out.String()
}

// defaultUpstreamSpec is a well-formed goenvoy manifest with one entry; change replaces lines by
// key.
func defaultUpstreamSpec(change map[string]string) string {
	return entryLines([]string{
		"name: sample",
		"repository: Owner/Project",
		"tag: v1.2.3",
		"commit: " + someCommit,
		"path: src/openapi.json",
		"sha256: " + someSHA256,
		"license: GPL-3.0",
		"prefix: /api/v1",
	}, change)
}

// defaultLocalSpec is a well-formed specs.yaml that pins a goenvoy manifest of sha256 sourceSha
// and lists one entry; change replaces lines of the entry by key.
func defaultLocalSpec(sourceSha string, change map[string]string) string {
	return "source:\n" +
		"  repository: golusoris/goenvoy\n" +
		"  commit: " + someCommit + "\n" +
		"  path: upstream/specs.yaml\n" +
		"  sha256: " + sourceSha + "\n" +
		entryLines([]string{
			"name: sample",
			"operations: [GET /api/v1/status, 'POST /api/v1/command/{id}']",
		}, change)
}

// writeFile writes content to file, creating its directory.
func writeFile(t *testing.T, file, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", file, err)
	}
}

// mockServer serves a goenvoy manifest at the pinned commit and counts the requests it answers.
type mockServer struct {
	*httptest.Server
	requests atomic.Int32
}

// serveManifest serves upstreamBody at the address of the source pin of defaultLocalSpec.
func serveManifest(t *testing.T, upstreamBody []byte) *mockServer {
	t.Helper()
	var ms mockServer
	ms.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ms.requests.Add(1)
		expectedPath := "/golusoris/goenvoy/" + someCommit + "/upstream/specs.yaml"
		if r.URL.Path != expectedPath {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(upstreamBody)
	}))
	t.Cleanup(ms.Close)
	return &ms
}

func TestReadManifest(t *testing.T) {
	t.Parallel()
	otherProject := map[string]string{"name": "name: other", "repository": "repository: Other/Project"}

	tests := []struct {
		name       string
		upstream   string
		local      func(sourceSha string) string
		serveEmpty bool
		want       string
	}{
		// Positive cases
		{
			name:     "well formed",
			upstream: defaultUpstreamSpec(nil),
			local:    func(sha string) string { return defaultLocalSpec(sha, nil) },
		},
		{
			name:     "no prefix",
			upstream: defaultUpstreamSpec(map[string]string{"prefix": ""}),
			local: func(sha string) string {
				return defaultLocalSpec(sha, map[string]string{"operations": "operations: [GET /status]"})
			},
		},
		{
			name:     "a tag with four numbers",
			upstream: defaultUpstreamSpec(map[string]string{"tag": "tag: v4.0.20.3014"}),
			local:    func(sha string) string { return defaultLocalSpec(sha, nil) },
		},
		{
			name:     "a branch in the place of the tag",
			upstream: defaultUpstreamSpec(map[string]string{"tag": "branch: develop"}),
			local:    func(sha string) string { return defaultLocalSpec(sha, nil) },
		},
		{
			name:     "a branch with a slash",
			upstream: defaultUpstreamSpec(map[string]string{"tag": "branch: release/v1"}),
			local:    func(sha string) string { return defaultLocalSpec(sha, nil) },
		},
		{
			name:     "a command class",
			upstream: defaultUpstreamSpec(nil),
			local: func(sha string) string {
				return defaultLocalSpec(sha, map[string]string{
					"operations": "operations: [POST /api/v1/command]\n    commands: [{path: src/A.cs, sha256: " + someSHA256 + "}]",
				})
			},
		},

		// Planted cases (a, b, d, e, f)
		{
			name:     "source sha256 drift (a)",
			upstream: defaultUpstreamSpec(nil),
			local: func(_ string) string {
				wrong := "0000000000000000000000000000000000000000000000000000000000000000"
				return defaultLocalSpec(wrong, nil)
			},
			want: "goenvoy manifest at " + someCommit[:12] + " has sha256 " + digest([]byte(defaultUpstreamSpec(nil))) + ", specs.yaml pins 0000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			name:     "goenvoy commit serves 404 (b)",
			upstream: defaultUpstreamSpec(nil),
			local: func(sha string) string {
				return strings.Replace(defaultLocalSpec(sha, nil), "commit: "+someCommit, "commit: ffffffffffffffffffffffffffffffffffffffff", 1)
			},
			want: "HTTP 404",
		},
		{
			name:     "local name missing upstream (d)",
			upstream: defaultUpstreamSpec(nil),
			local: func(sha string) string {
				return defaultLocalSpec(sha, map[string]string{"name": "name: absent"})
			},
			want: "local name missing upstream in goenvoy manifest",
		},
		{
			name:     "unknown field in goenvoy manifest (e)",
			upstream: defaultUpstreamSpec(nil) + "    unknown_field: true\n",
			local:    func(sha string) string { return defaultLocalSpec(sha, nil) },
			want:     "field unknown_field not found",
		},
		{
			name:     "old-shape local file with repository and commit (f)",
			upstream: defaultUpstreamSpec(nil),
			local: func(sha string) string {
				return defaultLocalSpec(sha, map[string]string{"name": "name: sample\n    repository: Owner/Project\n    commit: " + someCommit})
			},
			want: "field repository not found",
		},

		// Local manifest defects
		{
			name:     "no entry in local manifest",
			upstream: defaultUpstreamSpec(nil),
			local: func(sha string) string {
				return "source:\n  repository: golusoris/goenvoy\n  commit: " + someCommit + "\n  path: upstream/specs.yaml\n  sha256: " + sha + "\nspecs: []\n"
			},
			want: `no "specs" entry`,
		},
		{
			name:       "an empty local file",
			upstream:   defaultUpstreamSpec(nil),
			local:      func(string) string { return "" },
			serveEmpty: true,
			want:       "EOF",
		},
		{
			name:     "no source entry in local file",
			upstream: defaultUpstreamSpec(nil),
			local: func(string) string {
				return "specs:\n  - name: sample\n    operations: [GET /api/v1/status]\n"
			},
			want: `no "source" entry`,
		},
		{
			name:     "source repository invalid",
			upstream: defaultUpstreamSpec(nil),
			local: func(sha string) string {
				return strings.Replace(defaultLocalSpec(sha, nil), "repository: golusoris/goenvoy", "repository: github.com/golusoris/goenvoy", 1)
			},
			want: "source repository must be <owner>/<name>",
		},
		{
			name:     "source commit short",
			upstream: defaultUpstreamSpec(nil),
			local: func(sha string) string {
				return strings.Replace(defaultLocalSpec(sha, nil), "commit: "+someCommit, "commit: 12345", 1)
			},
			want: "source commit must be a full 40-digit commit id",
		},
		{
			name:     "source path invalid",
			upstream: defaultUpstreamSpec(nil),
			local: func(sha string) string {
				return strings.Replace(defaultLocalSpec(sha, nil), "path: upstream/specs.yaml", "path: ../specs.yaml", 1)
			},
			want: "source path must be one clean file path",
		},
		{
			name:     "source sha256 short",
			upstream: defaultUpstreamSpec(nil),
			local: func(sha string) string {
				return strings.Replace(defaultLocalSpec(sha, nil), "sha256: "+sha, "sha256: 1234", 1)
			},
			want: "source sha256 must be 64 lower-case hex digits",
		},
		{
			name:     "local duplicate name",
			upstream: defaultUpstreamSpec(nil),
			local: func(sha string) string {
				base := defaultLocalSpec(sha, nil)
				return base + "  - name: sample\n    operations: [GET /api/v1/status]\n"
			},
			want: `repeats the name "sample"`,
		},
		{
			name:     "no operations in local spec",
			upstream: defaultUpstreamSpec(nil),
			local: func(sha string) string {
				return defaultLocalSpec(sha, map[string]string{"operations": "operations: []"})
			},
			want: "operations lists nothing",
		},
		{
			name:     "operation without method",
			upstream: defaultUpstreamSpec(nil),
			local: func(sha string) string {
				return defaultLocalSpec(sha, map[string]string{"operations": "operations: [/api/v1/status]"})
			},
			want: `must be "GET <path>" or "POST <path>"`,
		},
		{
			name:     "operation with another method",
			upstream: defaultUpstreamSpec(nil),
			local: func(sha string) string {
				return defaultLocalSpec(sha, map[string]string{"operations": "operations: [DELETE /api/v1/status]"})
			},
			want: `must be "GET <path>" or "POST <path>"`,
		},
		{
			name:     "operation with query",
			upstream: defaultUpstreamSpec(nil),
			local: func(sha string) string {
				return defaultLocalSpec(sha, map[string]string{"operations": "operations: ['GET /api/v1/status?x=1']"})
			},
			want: `must be "GET <path>" or "POST <path>"`,
		},
		{
			name:     "operation outside prefix",
			upstream: defaultUpstreamSpec(nil),
			local: func(sha string) string {
				return defaultLocalSpec(sha, map[string]string{"operations": "operations: [GET /api/v3/status]"})
			},
			want: `does not start with the prefix "/api/v1"`,
		},
		{
			name:     "operation listed twice",
			upstream: defaultUpstreamSpec(nil),
			local: func(sha string) string {
				return defaultLocalSpec(sha, map[string]string{"operations": "operations: [GET /api/v1/status, GET /api/v1/status]"})
			},
			want: "is listed twice",
		},
		{
			name:     "command class not cs",
			upstream: defaultUpstreamSpec(nil),
			local: func(sha string) string {
				return defaultLocalSpec(sha, map[string]string{"operations": "operations: [GET /api/v1/status]\n    commands: [{path: src/A.json, sha256: " + someSHA256 + "}]"})
			},
			want: "one clean path of a .cs file",
		},
		{
			name:     "command class outside repository",
			upstream: defaultUpstreamSpec(nil),
			local: func(sha string) string {
				return defaultLocalSpec(sha, map[string]string{"operations": "operations: [GET /api/v1/status]\n    commands: [{path: ../A.cs, sha256: " + someSHA256 + "}]"})
			},
			want: "one clean path of a .cs file",
		},
		{
			name:     "command class without checksum",
			upstream: defaultUpstreamSpec(nil),
			local: func(sha string) string {
				return defaultLocalSpec(sha, map[string]string{"operations": "operations: [GET /api/v1/status]\n    commands: [{path: src/A.cs}]"})
			},
			want: "sha256 must be 64",
		},
		{
			name:     "two command classes with same filename",
			upstream: defaultUpstreamSpec(nil),
			local: func(sha string) string {
				return defaultLocalSpec(sha, map[string]string{"operations": "operations: [GET /api/v1/status]\n    commands: [{path: a/A.cs, sha256: " + someSHA256 + "}, {path: b/A.cs, sha256: " + someSHA256 + "}]"})
			},
			want: "repeats the file name",
		},
		{
			name:     "commands sent and no class pinned",
			upstream: defaultUpstreamSpec(nil),
			local: func(sha string) string {
				return defaultLocalSpec(sha, map[string]string{"operations": "operations: [POST /api/v1/command]"})
			},
			want: "commands pins no class",
		},

		// Upstream manifest defects
		{
			name: "upstream duplicate name",
			upstream: defaultUpstreamSpec(nil) +
				"  - name: sample\n    repository: Owner/Project\n    tag: v2.0\n    commit: " + someCommit + "\n    path: src/openapi.json\n    sha256: " + someSHA256 + "\n    license: GPL-3.0\n",
			local: func(sha string) string { return defaultLocalSpec(sha, nil) },
			want:  `upstream manifest repeats the name "sample"`,
		},
		{
			name:     "upstream repository with host",
			upstream: defaultUpstreamSpec(map[string]string{"repository": "repository: github.com/Owner/Project"}),
			local:    func(sha string) string { return defaultLocalSpec(sha, nil) },
			want:     "repository must be <owner>/<name>",
		},
		{
			name:     "upstream tag with space",
			upstream: defaultUpstreamSpec(map[string]string{"tag": "tag: v1 beta"}),
			local:    func(sha string) string { return defaultLocalSpec(sha, nil) },
			want:     "tag must name one release tag",
		},
		{
			name:     "neither tag nor branch in upstream",
			upstream: defaultUpstreamSpec(map[string]string{"tag": ""}),
			local:    func(sha string) string { return defaultLocalSpec(sha, nil) },
			want:     "exactly one of tag and branch",
		},
		{
			name:     "tag and branch in upstream",
			upstream: defaultUpstreamSpec(map[string]string{"tag": "tag: v1.2.3\n    branch: develop"}),
			local:    func(sha string) string { return defaultLocalSpec(sha, nil) },
			want:     "exactly one of tag and branch",
		},
		{
			name:     "branch with space in upstream",
			upstream: defaultUpstreamSpec(map[string]string{"tag": "branch: my branch"}),
			local:    func(sha string) string { return defaultLocalSpec(sha, nil) },
			want:     "branch must name one branch",
		},
		{
			name:     "branch ends with slash in upstream",
			upstream: defaultUpstreamSpec(map[string]string{"tag": "branch: release/"}),
			local:    func(sha string) string { return defaultLocalSpec(sha, nil) },
			want:     "branch must name one branch",
		},
		{
			name:     "commit id short in upstream",
			upstream: defaultUpstreamSpec(map[string]string{"commit": "commit: 0123456"}),
			local:    func(sha string) string { return defaultLocalSpec(sha, nil) },
			want:     "full 40-digit commit id",
		},
		{
			name:     "commit id uppercase in upstream",
			upstream: defaultUpstreamSpec(map[string]string{"commit": "commit: " + strings.ToUpper(someCommit)}),
			local:    func(sha string) string { return defaultLocalSpec(sha, nil) },
			want:     "full 40-digit commit id",
		},
		{
			name:     "path leaving repository in upstream",
			upstream: defaultUpstreamSpec(map[string]string{"path": "path: ../openapi.json"}),
			local:    func(sha string) string { return defaultLocalSpec(sha, nil) },
			want:     "one clean file path",
		},
		{
			name:     "sha256 short in upstream",
			upstream: defaultUpstreamSpec(map[string]string{"sha256": "sha256: " + someSHA256[1:]}),
			local:    func(sha string) string { return defaultLocalSpec(sha, nil) },
			want:     "sha256 must be 64",
		},
		{
			name:     "license missing in upstream",
			upstream: defaultUpstreamSpec(map[string]string{"license": ""}),
			local:    func(sha string) string { return defaultLocalSpec(sha, nil) },
			want:     "license must be the SPDX id",
		},
		{
			name:     "prefix without slash in upstream",
			upstream: defaultUpstreamSpec(map[string]string{"prefix": "prefix: api/v1"}),
			local:    func(sha string) string { return defaultLocalSpec(sha, nil) },
			want:     "prefix must be empty or a path",
		},
		{
			name: "one commit under two repositories",
			upstream: defaultUpstreamSpec(nil) +
				strings.TrimPrefix(defaultUpstreamSpec(otherProject), "specs:\n"),
			local: func(sha string) string {
				base := defaultLocalSpec(sha, nil)
				return base + "  - name: other\n    operations: [GET /api/v1/status]\n"
			},
			want: "repeats the commit of Owner/Project",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			upData := []byte(test.upstream)
			var server *mockServer
			if !test.serveEmpty {
				server = serveManifest(t, upData)
			} else {
				server = serveManifest(t, nil)
			}
			sha := digest(upData)
			localContent := test.local(sha)

			dir := t.TempDir()
			manifestFile := filepath.Join(dir, "specs.yaml")
			writeFile(t, manifestFile, localContent)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			pins, err := readManifest(ctx, manifestFile, filepath.Join(dir, "cache"), server.URL, server.Client())
			switch {
			case test.want == "" && err != nil:
				t.Fatalf("readManifest: %v", err)
			case test.want == "" && len(pins) != 1:
				t.Fatalf("readManifest returned %d pin(s), want 1", len(pins))
			case test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)):
				t.Fatalf("readManifest = %v, want an error with %q", err, test.want)
			}
		})
	}

	// Absent local file is refused.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	if _, err := readManifest(ctx, filepath.Join(t.TempDir(), "absent.yaml"), "", "https://example.test", nil); err == nil {
		t.Error("an absent manifest was accepted")
	}
}

func TestManifestCaching(t *testing.T) {
	t.Parallel()
	upData := []byte(defaultUpstreamSpec(nil))
	server := serveManifest(t, upData)
	sha := digest(upData)
	localContent := defaultLocalSpec(sha, nil)

	dir := t.TempDir()
	manifestFile := filepath.Join(dir, "specs.yaml")
	writeFile(t, manifestFile, localContent)
	cacheDir := filepath.Join(dir, "cache")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First read: downloads from server into cache.
	pins, err := readManifest(ctx, manifestFile, cacheDir, server.URL, server.Client())
	if err != nil {
		t.Fatalf("first readManifest: %v", err)
	}
	if len(pins) != 1 {
		t.Fatalf("pins = %d, want 1", len(pins))
	}
	if got := server.requests.Load(); got != 1 {
		t.Fatalf("server requests = %d, want 1", got)
	}

	cachedFile := filepath.Join(cacheDir, "source.manifest")
	if data, readErr := os.ReadFile(cachedFile); readErr != nil || string(data) != string(upData) {
		t.Fatalf("cached file differs from upstream: %v", readErr)
	}

	// Second read: cache has matching sha, zero new server requests.
	server.requests.Store(0)
	pins, err = readManifest(ctx, manifestFile, cacheDir, server.URL, server.Client())
	if err != nil {
		t.Fatalf("second readManifest: %v", err)
	}
	if len(pins) != 1 {
		t.Fatalf("pins = %d, want 1", len(pins))
	}
	if got := server.requests.Load(); got != 0 {
		t.Fatalf("second readManifest made %d request(s); want 0 (cached)", got)
	}

	// Corrupted cache re-downloads.
	writeFile(t, cachedFile, "corrupted content")
	server.requests.Store(0)
	pins, err = readManifest(ctx, manifestFile, cacheDir, server.URL, server.Client())
	if err != nil {
		t.Fatalf("readManifest after corrupted cache: %v", err)
	}
	if len(pins) != 1 {
		t.Fatalf("pins = %d, want 1", len(pins))
	}
	if got := server.requests.Load(); got != 1 {
		t.Fatalf("readManifest after corrupted cache made %d request(s); want 1", got)
	}
}

// TestCommittedManifest binds the manifest of this repository: it must be well formed and name
// the specs the contract tests load. The goenvoy manifest it pins is read and joined by
// `make contracts-verify`, which has the network; this test stays offline.
func TestCommittedManifest(t *testing.T) {
	t.Parallel()
	local, err := readLocal("specs.yaml")
	if err != nil {
		t.Fatalf("readLocal: %v", err)
	}
	names := make([]string, 0, len(local.Specs))
	for _, s := range local.Specs {
		names = append(names, s.Name)
	}
	want := []string{"sonarr", "radarr", "lidarr", "seerr", "bookshelf", "chaptarr", "readarr-faustvii", "whisparr-v2", "whisparr-v3"}
	if !slices.Equal(names, want) {
		t.Errorf("pinned specs = %q, want %q", names, want)
	}
}

func TestPinSource(t *testing.T) {
	t.Parallel()
	p := pin{Name: "sample", Repository: "Owner/Project", Commit: someCommit, Path: "src/openapi.json"}
	want := "https://example.test/Owner/Project/" + someCommit + "/src/openapi.json"
	for _, base := range []string{"https://example.test", "https://example.test/"} {
		if got := p.source(base); got != want {
			t.Errorf("source(%q) = %q, want %q", base, got, want)
		}
	}
	if got := p.specFile(); got != "sample.spec" {
		t.Errorf("specFile = %q", got)
	}
}

func TestPinRef(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		pin  pin
		want string
	}{
		{pin{Tag: "v1.2.3"}, "v1.2.3"},
		{pin{Branch: "develop"}, "branch develop"},
	} {
		if got := test.pin.ref(); got != test.want {
			t.Errorf("ref of %+v = %q, want %q", test.pin, got, test.want)
		}
	}
}
