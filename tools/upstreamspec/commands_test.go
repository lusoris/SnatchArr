// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/tools/upstreamspec/contract"
)

// A class shaped like the upstream ones (a byte order mark, constructors, a computed override).
const episodeClass = "\xef\xbb\xbfusing System.Collections.Generic;\n" +
	"namespace NzbDrone.Core.IndexerSearch\n{\n" +
	"    public class EpisodeSearchCommand : Command\n    {\n" +
	"        public List<int> EpisodeIds { get; set; }\n\n" +
	"        public override bool SendUpdatesToClient => true;\n\n" +
	"        public EpisodeSearchCommand()\n        {\n        }\n\n" +
	"        public EpisodeSearchCommand(List<int> episodeIds)\n        {\n            EpisodeIds = episodeIds;\n        }\n    }\n}\n"

func TestParseCommand(t *testing.T) {
	t.Parallel()
	class := func(body string) string {
		return "namespace N\n{\n    public class SeasonSearchCommand : Command\n    {\n" + body + "    }\n}\n"
	}
	tests := []struct {
		name       string
		source     string
		command    string
		properties []string
		err        string
	}{
		{"an upstream class", episodeClass, "EpisodeSearch", []string{"episodeIds"}, ""},
		{
			"two properties, sorted", class("        public int SeriesId { get; set; }\n        public int SeasonNumber { get; set; }\n"),
			"SeasonSearch",
			[]string{"seasonNumber", "seriesId"},
			"",
		},
		{
			"a property of a generic or nullable type", class("        public Dictionary<string, int?> Counts { get; set; }\n        public int? Limit { get; set; }\n"),
			"SeasonSearch",
			[]string{"counts", "limit"},
			"",
		},
		{
			"a leading acronym", class("        public int ID { get; set; }\n        public string URLPath { get; set; }\n"),
			"SeasonSearch",
			[]string{"id", "urlPath"},
			"",
		},
		{
			"a computed property is no body field", class("        public int SeriesId { get; set; }\n        public bool Quick => true;\n"),
			"SeasonSearch",
			[]string{"seriesId"},
			"",
		},
		{
			"an override, a static and a get-only property are no body fields",
			class("        public int SeriesId { get; set; }\n        public override int A { get; set; }\n        public static int B { get; set; }\n        public int C { get; }\n        public int D { get; private set; }\n"),
			"SeasonSearch",
			[]string{"seriesId"},
			"",
		},
		{
			"a JsonIgnore property is no body field", class("        public int SeriesId { get; set; }\n        [JsonIgnore]\n        public int Hidden { get; set; }\n"),
			"SeasonSearch",
			[]string{"seriesId"},
			"",
		},
		{"no class", "namespace N { }\n", "", nil, "found 0"},
		{"a class that is no command", "public class SeasonSearchCommand : Other\n{\n    public int A { get; set; }\n}\n", "", nil, "found 0"},
		{"two classes", episodeClass + episodeClass, "", nil, "found 2"},
		{"no property", class("        public bool Quick => true;\n"), "", nil, "no public get/set property"},
		{"an empty file", "", "", nil, "found 0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			command, properties, err := parseCommand([]byte(test.source))
			if test.err != "" {
				if err == nil || !strings.Contains(err.Error(), test.err) {
					t.Fatalf("parseCommand = %v, want an error with %q", err, test.err)
				}
				return
			}
			if err != nil || command != test.command || !slices.Equal(properties, test.properties) {
				t.Errorf("parseCommand = %q, %q, %v; want %q, %q", command, properties, err, test.command, test.properties)
			}
		})
	}
}

func TestCamelCase(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"EpisodeIds": "episodeIds", "SeriesId": "seriesId", "ID": "id", "Id": "id", "A": "a",
		"URLPath": "urlPath", "already": "already", "": "",
	} {
		if got := camelCase(name); got != want {
			t.Errorf("camelCase(%q) = %q, want %q", name, got, want)
		}
	}
}

// serveCommand serves the sample spec and the class file below the sample pin.
func serveCommand(t *testing.T, spec, class []byte) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Owner/Project/" + someCommit + "/src/openapi.yaml":
			_, _ = w.Write(spec)
		case "/Owner/Project/" + someCommit + "/src/EpisodeSearchCommand.cs":
			_, _ = w.Write(class)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func commandPin(spec, class []byte) pin {
	p := fetchPin(spec)
	p.Commands = []command{{Path: "src/EpisodeSearchCommand.cs", SHA256: digest(class)}}
	return p
}

func TestFetchWritesTheCommandsOfTheClasses(t *testing.T) {
	t.Parallel()
	spec, class := []byte(sampleSpec), []byte(episodeClass)
	server := serveCommand(t, spec, class)
	cache := filepath.Join(t.TempDir(), "cache")

	out, err := fetchInto(t, server.URL, cache, commandPin(spec, class))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if want := "1 operation(s), 1 command class(es), spec downloaded\n"; !strings.HasSuffix(out, want) {
		t.Errorf("summary = %q, want the suffix %q", out, want)
	}
	file, err := contract.Read(cache, "sample")
	if err != nil {
		t.Fatalf("contract: %v", err)
	}
	if got := file.Commands["EpisodeSearch"]; !slices.Equal(got, []string{"episodeIds"}) || len(file.Commands) != 1 {
		t.Errorf("commands = %v, want EpisodeSearch with episodeIds", file.Commands)
	}
	if cached := entries(t, cache); !slices.Contains(cached, "sample.command.EpisodeSearchCommand.cs") {
		t.Errorf("cache holds %q, want the class file", cached)
	}
}

func TestFetchRefusesACommandClass(t *testing.T) {
	t.Parallel()
	spec, class := []byte(sampleSpec), []byte(episodeClass)
	tests := []struct {
		name  string
		serve []byte
		pin   func(*pin)
		want  string
	}{
		{"another checksum", append(bytes.Clone(class), '\n'), func(*pin) {}, "the manifest pins " + digest(class)},
		{"a file without a command", []byte("namespace N { }\n"), func(p *pin) { p.Commands[0].SHA256 = digest([]byte("namespace N { }\n")) }, "found 0"},
		{"a command declared twice", class, func(p *pin) {
			p.Commands = append(p.Commands, command{Path: "src/EpisodeSearchCommand.cs", SHA256: digest(class)})
		}, "declares the command EpisodeSearch again"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := serveCommand(t, spec, test.serve)
			p := commandPin(spec, class)
			test.pin(&p)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var out bytes.Buffer
			err := fetcher{client: &http.Client{Timeout: 10 * time.Second}, base: server.URL, cache: filepath.Join(t.TempDir(), "cache"), out: &out}.all(ctx, []pin{p})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("fetch = %v, want an error with %q", err, test.want)
			}
		})
	}
}
