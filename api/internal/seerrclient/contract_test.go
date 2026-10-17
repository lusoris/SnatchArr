// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package seerrclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/golusoris/goenvoy/arr/seerr"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/tools/upstreamspec/contract"
)

// pagedRequests is the list-requests response shape.
//
//nolint:tagliatelle // Seerr speaks camelCase
type pagedRequests struct {
	PageInfo seerr.PageInfo       `json:"pageInfo"`
	Results  []seerr.MediaRequest `json:"results"`
}

// decoded is one operation of the Seerr spec with the wire type the client decodes its response
// into. absent lists what the type reads and the spec does not document for this operation; an
// entry that the spec does document fails the check, so the list cannot go stale.
type decoded struct {
	operation string
	wire      reflect.Type
	absent    []string
}

// decodes lists every operation the client requests.
var decodes = []decoded{
	{operation: "GET /api/v1/status", wire: reflect.TypeFor[seerr.StatusResponse]()},
	// The Seerr server returns type, seasons, media.mediaType and requestedBy.displayName
	// (server/entity/MediaRequest.ts, Media.ts and User.ts at the pinned commit) and its spec
	// leaves them out. goenvoy marks them omitempty, which contract.Missing does not check; the
	// fixture tests in client_test.go cover them. The default sort of the operation is `added`.
	{operation: "GET /api/v1/request", wire: reflect.TypeFor[pagedRequests]()},
	{operation: "GET /api/v1/settings/sonarr", wire: reflect.TypeFor[[]seerr.ServiceSettings]()},
	{operation: "GET /api/v1/settings/radarr", wire: reflect.TypeFor[[]seerr.ServiceSettings]()},
	{operation: "GET /api/v1/movie/{movieId}", wire: reflect.TypeFor[seerr.MovieDetails](), absent: []string{
		"mediaInfo.mediaType", "mediaInfo.status4k",
	}},
	{operation: "GET /api/v1/tv/{tvId}", wire: reflect.TypeFor[seerr.TvDetails](), absent: []string{
		"mediaInfo.mediaType", "mediaInfo.status4k",
	}},
}

// contractProblems checks the client against the contract of a Seerr spec and returns one line
// per difference:
//
//   - a field a wire type reads that the documented response lacks,
//   - a request the client sends that the spec does not describe (path or query parameter),
//   - a client method that fails on the documented example response,
//   - an operation of the contract the client does not request.
func contractProblems(ctx context.Context, file contract.File) []string {
	var problems []string
	for _, d := range decodes {
		operation, ok := file.Operations[d.operation]
		if !ok {
			problems = append(problems, d.operation+": the contract lacks the operation")
			continue
		}
		missing, err := contract.Missing(operation.Response, d.wire)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", d.operation, err))
			continue
		}
		for _, field := range missing {
			if !slices.Contains(d.absent, field) {
				problems = append(problems, fmt.Sprintf("%s: %s reads %q, which the spec does not document", d.operation, d.wire, field))
			}
		}
		for _, field := range d.absent {
			if !slices.Contains(missing, field) {
				problems = append(problems, fmt.Sprintf("%s: %q is listed as absent and the spec documents it", d.operation, field))
			}
		}
	}
	return append(problems, requestProblems(ctx, file)...)
}

// requestProblems runs every client method against a server that answers with the documented
// example responses and refuses what the spec does not describe.
func requestProblems(ctx context.Context, file contract.File) []string {
	handler := contract.NewServer(file)
	server := httptest.NewServer(handler)
	defer server.Close()
	client := New(5*time.Second, "SnatchArr contract test")

	var problems []string
	failed := func(method string, err error) {
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s on the documented response: %v", method, err))
		}
	}
	status, err := client.Status(ctx, server.URL, "key")
	failed("Status", err)
	if err == nil && status.Version == "" {
		problems = append(problems, "Status read no version from the documented response")
	}
	requests, err := client.ProcessingRequests(ctx, server.URL, "key")
	failed("ProcessingRequests", err)
	if err == nil && len(requests) == 0 {
		problems = append(problems, "ProcessingRequests read no request from the documented response")
	}
	servers, err := client.Servers(ctx, server.URL, "key")
	failed("Servers", err)
	if err == nil && len(servers) != 2 {
		problems = append(problems, fmt.Sprintf("Servers read %d server(s) from the documented responses, want one per kind", len(servers)))
	}
	for _, mediaType := range []domain.SeerrMediaType{domain.SeerrMovie, domain.SeerrTV} {
		title, titleErr := client.Title(ctx, server.URL, "key", mediaType, 1)
		failed("Title of a "+string(mediaType), titleErr)
		if titleErr == nil && title == "" {
			problems = append(problems, fmt.Sprintf("Title read no title of a %s from the documented response", mediaType))
		}
	}
	problems = append(problems, handler.Problems()...)
	for _, operation := range handler.Unrequested() {
		problems = append(problems, operation+": the contract lists the operation and the client does not request it")
	}
	return problems
}

// TestUpstreamContract checks the client against the Seerr spec the manifest pins
// (docs/upstream-contracts.md). It runs in `make contracts-verify`, which fills the cache and
// fails when this test left no receipt.
func TestUpstreamContract(t *testing.T) {
	t.Parallel()
	file, err := contract.Load("seerr")
	if errors.Is(err, contract.ErrNoCache) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range contractProblems(t.Context(), file) {
		t.Error(problem)
	}
	if t.Failed() {
		return
	}
	if err := contract.Passed(file); err != nil {
		t.Fatal(err)
	}
}

const (
	sampleMovie = `{"id":1,"adult":true,"backdropPath":"string","budget":1,"genres":[{"id":1,"name":"string"}],"homepage":"string","imdbId":"string","originalLanguage":"string","originalTitle":"string","overview":"string","popularity":1,"posterPath":"string","productionCompanies":[{"id":1,"name":"string","logoPath":"string","originCountry":"string"}],"releaseDate":"string","revenue":1,"runtime":1,"status":"string","tagline":"string","title":"string","video":true,"voteAverage":1,"voteCount":1,"mediaInfo":{"id":1,"status":1,"createdAt":"string","updatedAt":"string","requests":[{"id":1}],"tmdbId":1,"tvdbId":1}}`
	sampleTV    = `{"id":1,"backdropPath":"string","createdBy":[{"id":1,"name":"string","gender":1,"profilePath":"string"}],"firstAirDate":"string","genres":[{"id":1,"name":"string"}],"homepage":"string","inProduction":false,"lastAirDate":"string","mediaInfo":{"id":1,"status":1,"createdAt":"string","updatedAt":"string","requests":[{"id":1}],"tmdbId":1,"tvdbId":1},"name":"string","networks":[{"id":1,"name":"string","logoPath":"string","originCountry":"string"}],"numberOfEpisodes":1,"numberOfSeason":1,"originalLanguage":"string","originalName":"string","overview":"string","popularity":1,"posterPath":"string","seasons":[{"id":1,"seasonNumber":1,"airDate":"string","episodeCount":1,"episodes":[{"id":1,"name":"string","airDate":"string","episodeNumber":1,"overview":"string","seasonNumber":1,"stillPath":"string","voteAverage":1,"voteCount":1}],"name":"string","overview":"string","posterPath":"string"}],"status":"string","tagline":"string","type":"string","voteAverage":1,"voteCount":1}`
)

// sampleContract is a contract with the shape the pinned Seerr spec has for the operations of
// the client. It is written by hand: nothing of the spec is copied.
func sampleContract() contract.File {
	server := `[{"id":1,"name":"string","hostname":"string","port":1,"apiKey":"string","useSsl":true,"baseUrl":"string","is4k":true,"isDefault":true,"activeProfileId":1,"activeProfileName":"string","activeDirectory":"string","externalUrl":"string","syncEnabled":true,"preventSearch":false}]`
	operation := func(response string, parameters ...string) contract.Operation {
		return contract.Operation{Parameters: parameters, Response: json.RawMessage(response)}
	}
	return contract.File{Spec: "seerr", Tag: "v0", Commit: "0123abc", Operations: map[string]contract.Operation{
		"GET /api/v1/status": operation(`{"version":"string","commitTag":"string","updateAvailable":true,"commitsBehind":0,"restartRequired":false}`),
		"GET /api/v1/request": operation(`{"pageInfo":{"page":1,"pages":1,"results":1},"results":[{"id":1,"status":1,"is4k":true,"serverId":1,
			"createdAt":"2020-01-02T03:04:05Z","requestedBy":{"username":"string","email":"string"},
			"media":{"id":1,"tmdbId":1,"tvdbId":1,"status":1}}]}`, "filter", "skip", "sort", "take"),
		"GET /api/v1/settings/sonarr": operation(server),
		"GET /api/v1/settings/radarr": operation(server),
		"GET /api/v1/movie/{movieId}": operation(sampleMovie),
		"GET /api/v1/tv/{tvId}":       operation(sampleTV),
	}}
}

func TestContractProblems(t *testing.T) {
	t.Parallel()
	respond := func(operation, response string) func(*contract.File) {
		return func(file *contract.File) {
			changed := file.Operations[operation]
			changed.Response = json.RawMessage(response)
			file.Operations[operation] = changed
		}
	}
	tests := []struct {
		name   string
		change func(*contract.File)
		want   []string
	}{
		{"the documented shape", func(*contract.File) {}, nil},
		{"a field the client reads leaves the spec", respond("GET /api/v1/status", `{"commitTag":"string"}`), []string{
			`GET /api/v1/status: seerr.StatusResponse reads "version", which the spec does not document`,
			"Status read no version from the documented response",
		}},
		{"a nested field leaves the spec", respond("GET /api/v1/settings/radarr", `[{"id":1,"name":"string","hostname":"string","port":1,"useSsl":true,"baseUrl":"string","is4k":true,"isDefault":true,"activeProfileId":1,"activeDirectory":"string","syncEnabled":true,"preventSearch":false}]`), []string{
			`GET /api/v1/settings/radarr: []seerr.ServiceSettings reads "[].apiKey", which the spec does not document`,
		}},
		{"the spec starts to document a field listed as absent", respond("GET /api/v1/movie/{movieId}", strings.Replace(sampleMovie, `"mediaInfo":{`, `"mediaInfo":{"mediaType":"movie",`, 1)), []string{
			`GET /api/v1/movie/{movieId}: "mediaInfo.mediaType" is listed as absent and the spec documents it`,
		}},
		{"a query parameter leaves the spec", func(file *contract.File) {
			changed := file.Operations["GET /api/v1/request"]
			changed.Parameters = []string{"filter", "skip", "sort"}
			file.Operations["GET /api/v1/request"] = changed
		}, []string{`GET /api/v1/request: query parameter "take" is not in the spec`}},
		{"an operation leaves the spec", func(file *contract.File) { delete(file.Operations, "GET /api/v1/tv/{tvId}") }, []string{
			"GET /api/v1/tv/{tvId}: the contract lacks the operation",
			"GET /api/v1/tv/1: no such operation in the contract",
			"Title of a tv on the documented response",
		}},
		{"an operation the client does not request", func(file *contract.File) {
			file.Operations["GET /api/v1/user"] = contract.Operation{Response: json.RawMessage(`{}`)}
		}, []string{"GET /api/v1/user: the contract lists the operation and the client does not request it"}},
		{"a documented list without an element", respond("GET /api/v1/request", `{"pageInfo":{"pages":1,"results":0},"results":[]}`), []string{
			`GET /api/v1/request: seerrclient.pagedRequests reads "results: empty", which the spec does not document`,
			"ProcessingRequests read no request from the documented response",
		}},
		{"a response that is no JSON", respond("GET /api/v1/status", `{`), []string{
			"GET /api/v1/status: example:", "Status on the documented response",
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			file := sampleContract()
			test.change(&file)
			got := contractProblems(t.Context(), file)
			if len(test.want) == 0 {
				if len(got) != 0 {
					t.Fatalf("problems = %q, want none", got)
				}
				return
			}
			for _, want := range test.want {
				if !slices.ContainsFunc(got, func(problem string) bool { return strings.Contains(problem, want) }) {
					t.Errorf("problems %q lack %q", got, want)
				}
			}
		})
	}
}
