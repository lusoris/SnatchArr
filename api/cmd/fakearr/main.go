// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Command fakearr is a deterministic stand-in for a Sonarr- or Radarr-shaped instance,
// used by the end-to-end smoke test. It serves system/status, wanted/missing,
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
	"sync"
	"time"
)

type command struct {
	ID     int64           `json:"id"`
	Name   string          `json:"name"`
	Status string          `json:"status"`
	Body   json.RawMessage `json:"body"`
}

type fake struct {
	mu       sync.Mutex
	kind     string
	apiKey   string
	missing  int
	cutoff   int
	commands []command
	nextID   int64
	logger   *slog.Logger
}

func main() {
	addr := flag.String("addr", ":8989", "listen address")
	kind := flag.String("kind", "sonarr", "sonarr|radarr")
	apiKey := flag.String("api-key", "fake-api-key-0123456789", "expected X-Api-Key")
	missing := flag.Int("missing", 250, "number of missing items")
	cutoff := flag.Int("cutoff", 40, "number of cutoff-unmet items")
	flag.Parse()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	f := &fake{kind: *kind, apiKey: *apiKey, missing: *missing, cutoff: *cutoff, nextID: 100, logger: logger}
	mux := http.NewServeMux()
	f.routes(mux)
	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	logger.Info("fakearr listening", slog.String("kind", f.kind), slog.String("addr", *addr), slog.Int("missing", f.missing), slog.Int("cutoff", f.cutoff))
	if err := srv.ListenAndServe(); err != nil {
		logger.Error("fakearr stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func (f *fake) routes(mux *http.ServeMux) {
	prefix := "/api/v3"
	mux.HandleFunc("GET "+prefix+"/system/status", f.auth(f.status))
	mux.HandleFunc("GET "+prefix+"/wanted/missing", f.auth(func(w http.ResponseWriter, r *http.Request) { f.wanted(w, r, f.missing) }))
	mux.HandleFunc("GET "+prefix+"/wanted/cutoff", f.auth(func(w http.ResponseWriter, r *http.Request) { f.wanted(w, r, f.cutoff) }))
	mux.HandleFunc("GET "+prefix+"/queue", f.auth(f.queue))
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
	if f.kind == "radarr" {
		version, name = "5.14.0.9383", "Radarr"
	}
	writeJSON(w, map[string]any{"appName": name, "version": version, "instanceName": "fakearr"})
}

func (f *fake) wanted(w http.ResponseWriter, r *http.Request, total int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	page = max(page, 1)
	size = max(min(size, 1000), 1)
	start := (page - 1) * size
	records := make([]map[string]any, 0, size)
	for i := start; i < min(start+size, total); i++ {
		records = append(records, f.item(i))
	}
	writeJSON(w, map[string]any{"page": page, "pageSize": size, "totalRecords": total, "records": records})
}

func (f *fake) item(i int) map[string]any {
	id := int64(1000 + i)
	if f.kind == "radarr" {
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
	writeJSON(w, map[string]any{"page": 1, "pageSize": 1, "totalRecords": 3, "records": []any{}})
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
