// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Command fakearr is a deterministic stand-in for a Sonarr- or Radarr-shaped instance,
// built for the planned end-to-end smoke test (#19). It serves system/status, wanted/missing,
// wanted/cutoff, queue and command endpoints, records every search command, and exposes
// them at /_fake/commands for assertions. It is not part of the product image.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

type command struct {
	ID     int64           `json:"id"`
	Name   string          `json:"name"`
	Status string          `json:"status"`
	Body   json.RawMessage `json:"body"`
}

type fake struct {
	mu            sync.Mutex
	kind          string
	apiKey        string
	missing       int
	cutoff        int
	importBlocked bool
	imported      bool
	commands      []command
	nextID        int64
	logger        *slog.Logger
}

func newFake(kind, apiKey string, missing, cutoff int, importBlocked bool, logger *slog.Logger) *fake {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &fake{
		kind:          kind,
		apiKey:        apiKey,
		missing:       missing,
		cutoff:        cutoff,
		importBlocked: importBlocked,
		nextID:        100,
		logger:        logger,
	}
}

func main() {
	addr := flag.String("addr", ":8989", "listen address")
	kind := flag.String("kind", "sonarr", "sonarr|radarr|whisparr-v2|whisparr-v3")
	apiKey := flag.String("api-key", "fake-api-key-0123456789", "expected X-Api-Key")
	missing := flag.Int("missing", 250, "number of missing items")
	cutoff := flag.Int("cutoff", 40, "number of cutoff-unmet items")
	importBlocked := flag.Bool("import-blocked", false, "serve import-blocked queue item and manual import candidate")
	flag.Parse()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	f := newFake(*kind, *apiKey, *missing, *cutoff, *importBlocked, logger)
	mux := http.NewServeMux()
	f.routes(mux)
	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	logger.Info("fakearr listening", slog.String("kind", f.kind), slog.String("addr", *addr), slog.Int("missing", f.missing), slog.Int("cutoff", f.cutoff))
	if err := srv.ListenAndServe(); err != nil {
		logger.Error("fakearr stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

const (
	missingBaseID = 1000
	cutoffBaseID  = 5000
)

func (f *fake) appKind() domain.AppKind {
	k := strings.ToLower(strings.TrimSpace(f.kind))
	k = strings.ReplaceAll(k, "-", "_")
	parsed, err := domain.ParseAppKind(k)
	if err != nil {
		return domain.KindSonarr
	}
	return parsed
}

func (f *fake) isMovieKind() bool {
	//exhaustive:ignore
	switch f.appKind() {
	case domain.KindRadarr, domain.KindWhisparrV3:
		return true
	default:
		return false
	}
}

func (f *fake) routes(mux *http.ServeMux) {
	prefix := "/api/v3"
	mux.HandleFunc("GET "+prefix+"/system/status", f.auth(f.status))
	mux.HandleFunc("GET "+prefix+"/wanted/missing", f.auth(func(w http.ResponseWriter, r *http.Request) { f.wanted(w, r, f.missing, missingBaseID) }))
	mux.HandleFunc("GET "+prefix+"/wanted/cutoff", f.auth(func(w http.ResponseWriter, r *http.Request) { f.wanted(w, r, f.cutoff, cutoffBaseID) }))
	mux.HandleFunc("GET "+prefix+"/queue", f.auth(f.queue))
	mux.HandleFunc("GET "+prefix+"/manualimport", f.auth(f.manualImport))
	mux.HandleFunc("GET "+prefix+"/tag", f.auth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []map[string]any{{"id": 1, "label": "anime"}})
	}))
	mux.HandleFunc("GET "+prefix+"/qualityprofile", f.auth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []map[string]any{{"id": 1, "name": "Any"}})
	}))
	mux.HandleFunc("GET "+prefix+"/history/since", f.auth(func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, []any{}) }))
	mux.HandleFunc("POST "+prefix+"/command", f.auth(f.postCommand))
	mux.HandleFunc("GET "+prefix+"/command/{id}", f.auth(f.getCommand))
	mux.HandleFunc("GET /_fake/commands", f.listCommands)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}

func (f *fake) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != f.apiKey {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		next(w, r)
	}
}

func (f *fake) status(w http.ResponseWriter, _ *http.Request) {
	version := "4.0.10.2544"
	name := "Sonarr"
	//exhaustive:ignore
	switch f.appKind() {
	case domain.KindRadarr:
		version, name = "5.14.0.9383", "Radarr"
	case domain.KindWhisparrV2:
		version, name = "2.0.0.5000", "Whisparr"
	case domain.KindWhisparrV3:
		version, name = "3.0.0.5000", "Whisparr"
	default:
	}
	writeJSON(w, map[string]any{"appName": name, "version": version, "instanceName": "fakearr"})
}

func (f *fake) wanted(w http.ResponseWriter, r *http.Request, total int, baseID int64) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	page = max(page, 1)
	size = max(min(size, 1000), 1)
	start := (page - 1) * size
	records := make([]map[string]any, 0, size)
	for i := start; i < min(start+size, total); i++ {
		records = append(records, f.item(baseID+int64(i), i))
	}
	writeJSON(w, map[string]any{"page": page, "pageSize": size, "totalRecords": total, "records": records})
}

func (f *fake) item(id int64, i int) map[string]any {
	if f.isMovieKind() {
		return map[string]any{
			"id": id, "title": fmt.Sprintf("Movie %d", i), "year": 2000 + i%25, "monitored": i%7 != 0,
			"physicalRelease": "2020-01-01T00:00:00Z",
		}
	}
	return map[string]any{
		"id": id, "seriesId": int64(10 + i/10), "seasonNumber": 1 + i%3, "episodeNumber": 1 + i%10,
		"title": fmt.Sprintf("Episode %d", i), "airDateUtc": "2020-01-01T00:00:00Z", "monitored": i%7 != 0,
		"series": map[string]any{"monitored": true},
	}
}

func (f *fake) queue(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.importBlocked {
		writeJSON(w, map[string]any{"page": 1, "pageSize": 1, "totalRecords": 3, "records": []any{}})
		return
	}
	if f.imported {
		writeJSON(w, map[string]any{"page": 1, "pageSize": 1, "totalRecords": 0, "records": []any{}})
		return
	}
	writeJSON(w, map[string]any{
		"page":         1,
		"pageSize":     1,
		"totalRecords": 1,
		"records":      []any{f.queueRecord()},
	})
}

func (f *fake) queueRecord() map[string]any {
	rec := map[string]any{
		"id":                    1,
		"downloadId":            "fake-dl-1",
		"status":                "warning",
		"trackedDownloadStatus": "warning",
		"trackedDownloadState":  "importBlocked",
		"protocol":              "usenet",
		"size":                  1048576,
		"sizeleft":              0,
		"added":                 "2020-01-01T00:00:00Z",
	}
	if f.isMovieKind() {
		rec["title"] = "Fake.Movie.2020"
		rec["movieId"] = 10
		rec["movie"] = map[string]any{"id": 10, "monitored": true}
		rec["statusMessages"] = []map[string]any{
			{
				"title":    "Fake.Movie.2020",
				"messages": []string{"Movie Fake Movie was not found in the grabbed release: Fake.Movie.2020"},
			},
		}
		return rec
	}
	rec["title"] = "Fake.Episode.S01E01"
	rec["seriesId"] = 10
	rec["episodeId"] = 101
	rec["series"] = map[string]any{"id": 10, "monitored": true}
	rec["episode"] = map[string]any{"id": 101, "monitored": true}
	rec["statusMessages"] = []map[string]any{
		{
			"title":    "Fake.Episode.S01E01",
			"messages": []string{"Series title mismatch; automatic import is not possible."},
		},
	}
	return rec
}

func (f *fake) manualImport(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.importBlocked || r.URL.Query().Get("downloadId") != "fake-dl-1" {
		writeJSON(w, []any{})
		return
	}
	writeJSON(w, []any{f.manualImportCandidate()})
}

func (f *fake) manualImportCandidate() map[string]any {
	if f.isMovieKind() {
		cand := map[string]any{
			"id":         1,
			"path":       "/downloads/fake-dl-1/fake-movie.mkv",
			"movie":      map[string]any{"id": 10},
			"quality":    map[string]any{"quality": map[string]any{"id": 1}},
			"downloadId": "fake-dl-1",
			"rejections": []any{},
		}
		if f.appKind() == domain.KindWhisparrV3 {
			cand["indexerFlags"] = 0
		}
		return cand
	}
	return map[string]any{
		"id":         1,
		"path":       "/downloads/fake-dl-1/fake-episode.mkv",
		"series":     map[string]any{"id": 10},
		"episodes":   []map[string]any{{"id": 101}},
		"quality":    map[string]any{"quality": map[string]any{"id": 1}},
		"downloadId": "fake-dl-1",
		"rejections": []any{},
	}
}

func (f *fake) postCommand(w http.ResponseWriter, r *http.Request) {
	var body json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, `{"error":"bad body"}`, http.StatusBadRequest)
		return
	}
	var named struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &named); err != nil {
		http.Error(w, `{"error":"bad body"}`, http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.nextID++
	cmd := command{ID: f.nextID, Name: named.Name, Status: "queued", Body: body}
	f.commands = append(f.commands, cmd)
	if named.Name == "ManualImport" {
		f.imported = true
	}
	f.mu.Unlock()
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, cmd)
}

func (f *fake) getCommand(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.commands {
		if f.commands[i].ID == id {
			f.commands[i].Status = "completed"
			writeJSON(w, f.commands[i])
			return
		}
	}
	http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
}

func (f *fake) listCommands(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, f.commands)
}

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, `{"error":"encode"}`, http.StatusInternalServerError)
	}
}
