// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package contract

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

type parent struct {
	Monitored bool `json:"monitored"`
}

//nolint:tagliatelle // the sample has the camelCase names of an upstream API
type record struct {
	ID      int64      `json:"id"`
	Title   string     `json:"title,omitempty"`
	Aired   *time.Time `json:"airDateUtc"`
	Parent  *parent    `json:"series"`
	Seasons []struct {
		Number int `json:"seasonNumber"`
	} `json:"seasons"`
	Untagged string
	Skipped  string `json:"-"`
	hidden   string //nolint:unused // an unexported field, which Missing must not read
}

//nolint:tagliatelle // the sample has the camelCase names of an upstream API
type page struct {
	Total   int      `json:"totalRecords"`
	Records []record `json:"records"`
}

const fullRecord = `{"id":1,"title":"t","airDateUtc":"2020-01-02T03:04:05Z","series":{"monitored":true},` +
	`"seasons":[{"seasonNumber":1}],"Untagged":"u"}`

func TestMissing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		example string
		typ     reflect.Type
		want    []string
	}{
		{"every field is documented", fullRecord, reflect.TypeFor[record](), nil},
		{"through a page and its array", `{"totalRecords":1,"records":[` + fullRecord + `]}`, reflect.TypeFor[page](), nil},
		{"a pointer type reads like its element", fullRecord, reflect.TypeFor[*record](), nil},
		{"an array at the root", `[` + fullRecord + `]`, reflect.TypeFor[[]record](), nil},
		{
			"fields the example lacks are named by their JSON path",
			`{"totalRecords":1,"records":[{"id":1,"series":{},"seasons":[{}]}]}`,
			reflect.TypeFor[page](),
			[]string{"records[].Untagged", "records[].airDateUtc", "records[].seasons[].seasonNumber", "records[].series.monitored", "records[].title"},
		},
		{
			"null and an empty array hide what is below them",
			`{"id":1,"title":"t","airDateUtc":null,"series":null,"seasons":[],"Untagged":"u"}`,
			reflect.TypeFor[record](),
			[]string{"airDateUtc: null", "seasons: empty", "series: null"},
		},
		{"a scalar where an object is read", `{"totalRecords":1,"records":[7]}`, reflect.TypeFor[page](), []string{"records[]: no object"}},
		{"an object where an array is read", `{"totalRecords":1,"records":{}}`, reflect.TypeFor[page](), []string{"records: no array"}},
		{"a null root", `null`, reflect.TypeFor[page](), []string{"(root): null"}},
		{"a scalar root", `7`, reflect.TypeFor[page](), []string{"(root): no object"}},
		{"a scalar type reads any value", `{"a":1}`, reflect.TypeFor[string](), nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := Missing([]byte(test.example), test.typ)
			if err != nil {
				t.Fatalf("Missing: %v", err)
			}
			if !slices.Equal(got, test.want) {
				t.Errorf("Missing = %q, want %q", got, test.want)
			}
		})
	}
}

func TestMissingRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()
	for _, example := range []string{`{"id":`, ``, `{"id":1} trailing`} {
		if _, err := Missing([]byte(example), reflect.TypeFor[record]()); err == nil || !strings.Contains(err.Error(), "example") {
			t.Errorf("Missing(%q) = %v, want a refusal that names the example", example, err)
		}
	}
}

func TestMatches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, template, path string
		want                 bool
	}{
		{"equal paths", "/api/v3/queue", "/api/v3/queue", true},
		{"a parameter takes one segment", "/api/v3/command/{id}", "/api/v3/command/42", true},
		{"a parameter in the middle", "/api/v1/{kind}/status", "/api/v1/tv/status", true},
		{"a parameter takes no empty segment", "/api/v3/command/{id}", "/api/v3/command/", false},
		{"a parameter takes no two segments", "/api/v3/command/{id}", "/api/v3/command/42/7", false},
		{"a shorter path", "/api/v3/command/{id}", "/api/v3/command", false},
		{"another path", "/api/v3/queue", "/api/v3/queue2", false},
		{"another prefix", "/api/v3/queue", "/api/v1/queue", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := Matches(test.template, test.path); got != test.want {
				t.Errorf("Matches(%q, %q) = %v, want %v", test.template, test.path, got, test.want)
			}
		})
	}
}

func sample() File {
	return File{Spec: "sample", Tag: "v1", Commit: "c", Operations: map[string]Operation{
		"GET /api/v3/queue":        {Parameters: []string{"page", "pageSize"}, Response: []byte(`{"totalRecords":3}`)},
		"GET /api/v3/command/{id}": {Parameters: []string{}, Response: []byte(`{"id":1}`)},
		"POST /api/v3/command":     {Parameters: []string{}, Response: []byte(`{"id":2}`)},
	}}
}

func request(t *testing.T, method, address string) (status int, body string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, address, http.NoBody)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	reply, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, address, err)
	}
	defer func() { _ = reply.Body.Close() }()
	data, err := io.ReadAll(reply.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return reply.StatusCode, string(data)
}

func TestServer(t *testing.T) {
	t.Parallel()
	handler := NewServer(sample())
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	if status, body := request(t, http.MethodGet, server.URL+"/api/v3/queue?page=1&pageSize=1"); status != http.StatusOK || body != `{"totalRecords":3}` {
		t.Errorf("queue = %d %q", status, body)
	}
	if status, body := request(t, http.MethodGet, server.URL+"/api/v3/command/42"); status != http.StatusOK || body != `{"id":1}` {
		t.Errorf("command/42 = %d %q", status, body)
	}
	if problems := handler.Problems(); len(problems) != 0 {
		t.Errorf("documented requests were recorded as problems: %q", problems)
	}
	if got, want := handler.Unrequested(), []string{"POST /api/v3/command"}; !slices.Equal(got, want) {
		t.Errorf("Unrequested = %q, want %q", got, want)
	}

	// Negative: what the spec does not describe is answered with 404 and recorded.
	if status, _ := request(t, http.MethodGet, server.URL+"/api/v3/queue?page=1&includeAll=true"); status != http.StatusOK {
		t.Errorf("queue with an undocumented parameter = %d", status)
	}
	if status, _ := request(t, http.MethodGet, server.URL+"/api/v3/wanted/missing"); status != http.StatusNotFound {
		t.Errorf("undocumented path = %d, want 404", status)
	}
	if status, _ := request(t, http.MethodPost, server.URL+"/api/v3/queue"); status != http.StatusNotFound {
		t.Errorf("undocumented method = %d, want 404", status)
	}
	want := []string{
		"GET /api/v3/queue: query parameter \"includeAll\" is not in the spec",
		"GET /api/v3/wanted/missing: no such operation in the contract",
		"POST /api/v3/queue: no such operation in the contract",
	}
	if got := handler.Problems(); !slices.Equal(got, want) {
		t.Errorf("Problems = %q, want %q", got, want)
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, dir, FileName("good"), `{"spec":"good","tag":"v1","commit":"c","operations":{"GET /a":{"parameters":["p"],"response":{"x":1}}}}`)
	write(t, dir, FileName("empty"), `{"spec":"empty","operations":{}}`)
	write(t, dir, FileName("broken"), `{"spec":`)

	file, err := Read(dir, "good")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	operation := file.Operations["GET /a"]
	if file.Tag != "v1" || !slices.Equal(operation.Parameters, []string{"p"}) || string(operation.Response) != `{"x":1}` {
		t.Errorf("Read = %+v", file)
	}
	write(t, dir, FileName("renamed"), `{"spec":"good","operations":{"GET /a":{"parameters":[],"response":1}}}`)
	for _, spec := range []string{"empty", "broken", "absent", "renamed"} {
		if _, err := Read(dir, spec); err == nil {
			t.Errorf("Read(%q) accepted a contract that checks nothing", spec)
		}
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, FileName("good"), `{"spec":"good","operations":{"GET /a":{"parameters":[],"response":1}}}`)

	t.Setenv(EnvCache, "")
	if _, err := Load("good"); !errors.Is(err, ErrNoCache) {
		t.Errorf("Load without %s = %v, want ErrNoCache", EnvCache, err)
	}
	t.Setenv(EnvCache, dir)
	if _, err := Load("good"); err != nil {
		t.Errorf("Load: %v", err)
	}
	// A cache that is named and lacks the file is an error, never the skip signal.
	if _, err := Load("absent"); err == nil || errors.Is(err, ErrNoCache) {
		t.Errorf("Load of an absent contract = %v, want an error other than ErrNoCache", err)
	}
}

func TestPassed(t *testing.T) {
	dir := t.TempDir()
	good := File{Spec: "good", Commit: "0123abc"}

	t.Setenv(EnvCache, "")
	if err := Passed(good); !errors.Is(err, ErrNoCache) {
		t.Errorf("Passed without %s = %v, want ErrNoCache", EnvCache, err)
	}
	t.Setenv(EnvCache, dir)
	if err := Passed(good); err != nil {
		t.Fatalf("Passed: %v", err)
	}
	if receipt, err := os.ReadFile(filepath.Join(dir, ReceiptName("good"))); err != nil || string(receipt) != "0123abc\n" {
		t.Errorf("receipt = %q, %v; want the commit and a line end", receipt, err)
	}
	// A contract without a spec name or a commit proves nothing, and a name with a directory
	// would write outside the cache.
	for _, file := range []File{{Commit: "0123abc"}, {Spec: "good"}, {Spec: "../outside", Commit: "0123abc"}} {
		if err := Passed(file); err == nil {
			t.Errorf("Passed(%+v) wrote a receipt", file)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), ReceiptName("outside"))); err == nil {
		t.Error("a receipt was written outside the cache directory")
	}
	// A cache directory that does not exist is an error, not a silent pass.
	t.Setenv(EnvCache, filepath.Join(dir, "absent"))
	if err := Passed(good); err == nil || errors.Is(err, ErrNoCache) {
		t.Errorf("Passed into an absent directory = %v, want a write error", err)
	}
}
