// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package contract is the file `upstreamspec fetch` writes for each pinned upstream OpenAPI spec,
// and what a contract test needs to check a hand-written client against it.
//
// A contract file lists the operations a client calls. Each has the names of the query parameters
// the spec documents and one example response in which every documented property is present. The
// file is built from the spec at test time and lives in a cache directory that is never committed
// (docs/upstream-contracts.md).
package contract

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
)

// EnvCache names the environment variable that holds the cache directory. A contract test runs
// against the pinned spec only when it is set.
const EnvCache = "SNATCHARR_SPEC_CACHE"

// maxSteps bounds the walk over one example.
const maxSteps = 100_000

// Operation is one operation of the spec: the documented query parameter names and an example of
// the documented response.
type Operation struct {
	Parameters []string        `json:"parameters"`
	Response   json.RawMessage `json:"response"`
}

// File is the contract of one pinned spec. Operations is keyed by "<METHOD> <path>", with the
// path as the client requests it and path parameters in braces.
type File struct {
	Spec       string               `json:"spec"`
	Tag        string               `json:"tag"`
	Commit     string               `json:"commit"`
	Operations map[string]Operation `json:"operations"`
}

// FileName is the name of the contract file of spec inside the cache directory.
func FileName(spec string) string { return spec + ".contract.json" }

// ReceiptName is the name of the file a contract test leaves for spec inside the cache directory
// when it passed.
func ReceiptName(spec string) string { return spec + ".checked" }

// Read loads the contract of spec from the cache directory dir. A file without operations is
// refused: it would make every check pass. So is a file that names another spec.
func Read(dir, spec string) (File, error) {
	// #nosec G304 G703 -- the cache directory is the caller's argument; the file is only decoded
	data, err := os.ReadFile(filepath.Join(dir, FileName(spec)))
	if err != nil {
		return File{}, fmt.Errorf("read contract: %w", err)
	}
	var file File
	if err := json.Unmarshal(data, &file); err != nil {
		return File{}, fmt.Errorf("contract of %s: %w", spec, err)
	}
	if file.Spec != spec {
		return File{}, fmt.Errorf("contract of %s names the spec %q", spec, file.Spec)
	}
	if len(file.Operations) == 0 {
		return File{}, fmt.Errorf("contract of %s lists no operation", spec)
	}
	return file, nil
}

var unmarshaler = reflect.TypeFor[json.Unmarshaler]()

// step is one value of the example together with the Go type that decodes it.
type step struct {
	value any
	typ   reflect.Type
	at    string
}

// Missing returns what the Go type typ reads and the example does not provide, sorted: the JSON
// name of every struct field the example lacks ("media.status4k"), a value that is null
// ("media: null") and an array without an element ("results: empty"). Elements of an array share
// the path "<array>[]". Only the first element of an array is compared.
func Missing(example []byte, typ reflect.Type) ([]string, error) {
	var root any
	if err := json.Unmarshal(example, &root); err != nil {
		return nil, fmt.Errorf("example: %w", err)
	}
	var missing []string
	queue := []step{{value: root, typ: typ, at: ""}}
	for steps := 0; len(queue) > 0; steps++ {
		if steps == maxSteps {
			return nil, fmt.Errorf("example needs more than %d steps", maxSteps)
		}
		current := queue[0]
		queue = queue[1:]
		found, next := current.visit()
		missing = append(missing, found...)
		queue = append(queue, next...)
	}
	sort.Strings(missing)
	return missing, nil
}

func (s step) visit() (found []string, next []step) {
	typ := s.typ
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if s.value == nil {
		return []string{s.name() + ": null"}, nil
	}
	if reflect.PointerTo(typ).Implements(unmarshaler) {
		// The type decodes itself (time.Time), so its fields say nothing about the JSON it reads.
		return nil, nil
	}
	if typ.Kind() == reflect.Struct {
		return s.fields(typ)
	}
	if typ.Kind() == reflect.Slice {
		return s.element(typ)
	}
	return nil, nil
}

func (s step) name() string {
	if s.at == "" {
		return "(root)"
	}
	return s.at
}

func (s step) child(name string) string {
	if s.at == "" {
		return name
	}
	return s.at + "." + name
}

func (s step) fields(typ reflect.Type) (found []string, next []step) {
	object, ok := s.value.(map[string]any)
	if !ok {
		return []string{s.name() + ": no object"}, nil
	}
	for i := range typ.NumField() {
		field := typ.Field(i)
		name, read := jsonName(field)
		if !read {
			continue
		}
		value, present := object[name]
		if !present {
			found = append(found, s.child(name))
			continue
		}
		next = append(next, step{value: value, typ: field.Type, at: s.child(name)})
	}
	return found, next
}

func (s step) element(typ reflect.Type) (found []string, next []step) {
	list, ok := s.value.([]any)
	if !ok {
		return []string{s.name() + ": no array"}, nil
	}
	if len(list) == 0 {
		return []string{s.name() + ": empty"}, nil
	}
	return nil, []step{{value: list[0], typ: typ.Elem(), at: s.at + "[]"}}
}

// jsonName returns the JSON name encoding/json reads into field, and false for a field it skips.
func jsonName(field reflect.StructField) (string, bool) {
	if !field.IsExported() {
		return "", false
	}
	name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
	switch name {
	case "-":
		return "", false
	case "":
		return field.Name, true
	default:
		return name, true
	}
}

// Server answers each operation of a contract with its example response and records every
// request the spec does not describe.
type Server struct {
	file File

	mu       sync.Mutex
	seen     map[string]bool
	problems []string
}

// NewServer returns a handler for the operations of file.
func NewServer(file File) *Server {
	return &Server{file: file, seen: map[string]bool{}}
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key, ok := s.match(r.Method, r.URL.Path)
	if !ok {
		s.record(fmt.Sprintf("%s %s: no such operation in the contract", r.Method, r.URL.Path))
		http.NotFound(w, r)
		return
	}
	operation := s.file.Operations[key]
	for name := range r.URL.Query() {
		if !slices.Contains(operation.Parameters, name) {
			s.record(fmt.Sprintf("%s: query parameter %q is not in the spec", key, name))
		}
	}
	s.mu.Lock()
	s.seen[key] = true
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(operation.Response); err != nil {
		s.record(fmt.Sprintf("%s: write response: %v", key, err))
	}
}

func (s *Server) record(problem string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.problems = append(s.problems, problem)
}

// match returns the key of the operation that method and path request.
func (s *Server) match(method, path string) (string, bool) {
	for key := range s.file.Operations {
		wantMethod, template, ok := strings.Cut(key, " ")
		if ok && wantMethod == method && Matches(template, path) {
			return key, true
		}
	}
	return "", false
}

// Matches reports whether path is template with each "{name}" segment replaced by one segment.
func Matches(template, path string) bool {
	want, got := strings.Split(template, "/"), strings.Split(path, "/")
	if len(want) != len(got) {
		return false
	}
	for i, segment := range want {
		parameter := strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}")
		if parameter && got[i] != "" {
			continue
		}
		if segment != got[i] {
			return false
		}
	}
	return true
}

// Problems returns the requests the spec does not describe, sorted.
func (s *Server) Problems() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	problems := slices.Clone(s.problems)
	sort.Strings(problems)
	return problems
}

// Unrequested returns the operations of the contract no request reached, sorted.
func (s *Server) Unrequested() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var unrequested []string
	for key := range s.file.Operations {
		if !s.seen[key] {
			unrequested = append(unrequested, key)
		}
	}
	sort.Strings(unrequested)
	return unrequested
}

// ErrNoCache reports that EnvCache is not set.
var ErrNoCache = errors.New(EnvCache + " is not set; run `make contracts-verify`")

// Load reads the contract of spec from the directory EnvCache names.
func Load(spec string) (File, error) {
	dir := os.Getenv(EnvCache)
	if dir == "" {
		return File{}, ErrNoCache
	}
	return Read(dir, spec)
}

// Passed leaves the receipt of file in the directory EnvCache names: the commit the contract was
// built from. `upstreamspec receipts` fails for a pinned spec without the receipt of its commit,
// so a contract test that was skipped, filtered out or deleted does not pass unseen.
func Passed(file File) error {
	dir := os.Getenv(EnvCache)
	if dir == "" {
		return ErrNoCache
	}
	name := ReceiptName(file.Spec)
	if file.Spec == "" || file.Commit == "" || filepath.Base(name) != name {
		return fmt.Errorf("the contract names the spec %q at commit %q; a receipt needs both", file.Spec, file.Commit)
	}
	// #nosec G703 -- the cache directory is the caller's setting; the name is one path segment
	if err := os.WriteFile(filepath.Join(dir, name), []byte(file.Commit+"\n"), 0o600); err != nil {
		return fmt.Errorf("write receipt: %w", err)
	}
	return nil
}
