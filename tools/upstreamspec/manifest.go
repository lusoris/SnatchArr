// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// pin is one upstream OpenAPI spec, fixed to the bytes of one file at one commit.
//
// The repository, tag or branch, commit, path, sha256, license and prefix come from the goenvoy
// manifest; Operations and Commands from the local specs.yaml. An upstream project that tags no
// release is pinned with Branch in the place of Tag.
type pin struct {
	Name       string    `yaml:"name"`
	Repository string    `yaml:"repository"`
	Tag        string    `yaml:"tag"`
	Branch     string    `yaml:"branch"`
	Commit     string    `yaml:"commit"`
	Path       string    `yaml:"path"`
	SHA256     string    `yaml:"sha256"`
	License    string    `yaml:"license"`
	Prefix     string    `yaml:"prefix"`
	Operations []string  `yaml:"operations"`
	Commands   []command `yaml:"commands"`
}

// command is one upstream C# command class the client sends a body for, pinned to the bytes of
// one file at the commit of its spec. The spec cannot describe the body: CommandResource lists the
// generic properties only, and the fields of a command live in its class.
type command struct {
	Path   string `yaml:"path"`
	SHA256 string `yaml:"sha256"`
}

// manifestSource pins the upstream specs manifest owned by golusoris/goenvoy.
type manifestSource struct {
	Repository string `yaml:"repository"`
	Commit     string `yaml:"commit"`
	Path       string `yaml:"path"`
	SHA256     string `yaml:"sha256"`
}

// sourceOf is where the goenvoy manifest is read from below base.
func (s manifestSource) sourceOf(base string) string {
	return strings.TrimSuffix(base, "/") + "/" + s.Repository + "/" + s.Commit + "/" + s.Path
}

// problem names the first field of the source pin that is not well formed, or returns "".
func (s manifestSource) problem() string {
	checks := []struct {
		ok   bool
		text string
	}{
		{repository.MatchString(s.Repository), "source repository must be <owner>/<name>"},
		{commitID.MatchString(s.Commit), "source commit must be a full 40-digit commit id"},
		{cleanRelative(s.Path), "source path must be one clean file path inside the upstream repository"},
		{sha256Hex.MatchString(s.SHA256), "source sha256 must be 64 lower-case hex digits"},
	}
	for _, check := range checks {
		if !check.ok {
			return check.text
		}
	}
	return ""
}

// localManifest is specs.yaml: the source pin and the consumer lists of each spec.
type localManifest struct {
	Source manifestSource `yaml:"source"`
	Specs  []localSpec    `yaml:"specs"`
}

// localSpec is what SnatchArr owns of one spec: the operations it calls and the command classes
// it sends.
type localSpec struct {
	Name       string    `yaml:"name"`
	Operations []string  `yaml:"operations"`
	Commands   []command `yaml:"commands"`
}

// upstreamManifest is the goenvoy manifest. Its reader knows every field, so a field goenvoy adds
// fails here until this reader learns it.
type upstreamManifest struct {
	Specs []upstreamSpec `yaml:"specs"`
}

type upstreamSpec struct {
	Name       string `yaml:"name"`
	Repository string `yaml:"repository"`
	Tag        string `yaml:"tag"`
	Branch     string `yaml:"branch"`
	Commit     string `yaml:"commit"`
	Path       string `yaml:"path"`
	SHA256     string `yaml:"sha256"`
	License    string `yaml:"license"`
	Prefix     string `yaml:"prefix"`
}

// sourceManifestFile is the name of the cached goenvoy manifest inside the cache directory.
const sourceManifestFile = "source.manifest"

var (
	pinName    = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	repository = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+$`)
	tagName    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	branchName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(/[A-Za-z0-9][A-Za-z0-9._-]*)*$`)
	commitID   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Hex  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	licenseID  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+-]*$`)
	prefixPath = regexp.MustCompile(`^(/[A-Za-z0-9._-]+)*$`)
	operation  = regexp.MustCompile(`^(GET|POST) (/[A-Za-z0-9._{}-]+)+$`)
)

// manifestReader decodes the local specs.yaml, fetches or reads from cache the upstream
// manifest from goenvoy, verifies its sha256, and joins the pins by name.
type manifestReader struct {
	client *http.Client
	base   string
	cache  string
}

// readManifest reads the local manifest and the goenvoy manifest it pins, and returns the joined
// pins. A nil client or an empty base take the defaults of fetch.
func readManifest(ctx context.Context, file, cache, base string, client *http.Client) ([]pin, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if base == "" {
		base = rawBase
	}
	return manifestReader{client: client, base: base, cache: cache}.read(ctx, file)
}

func (r manifestReader) read(ctx context.Context, file string) ([]pin, error) {
	local, err := readLocal(file)
	if err != nil {
		return nil, err
	}
	data, err := r.sourceBytes(ctx, local.Source)
	if err != nil {
		return nil, err
	}
	upstream, err := decodeUpstream(data)
	if err != nil {
		return nil, err
	}
	return join(file, local.Specs, upstream)
}

// readLocal decodes specs.yaml and refuses one that is not well formed. A key the tool does not
// read is refused, so a misspelled field, or a pin field of the old shape, cannot pass.
func readLocal(file string) (localManifest, error) {
	// #nosec G304 -- the manifest path is the caller's argument; the file is only decoded
	data, err := os.ReadFile(file)
	if err != nil {
		return localManifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var manifest localManifest
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return localManifest{}, fmt.Errorf("%s: %w", file, err)
	}
	if manifest.Source.Repository == "" && manifest.Source.Commit == "" && manifest.Source.Path == "" && manifest.Source.SHA256 == "" {
		return localManifest{}, fmt.Errorf("%s: no \"source\" entry", file)
	}
	if problem := manifest.Source.problem(); problem != "" {
		return localManifest{}, fmt.Errorf("%s: source: %s", file, problem)
	}
	if len(manifest.Specs) == 0 {
		return localManifest{}, fmt.Errorf("%s: no \"specs\" entry", file)
	}
	if err := checkLocalNames(file, manifest.Specs); err != nil {
		return localManifest{}, err
	}
	return manifest, nil
}

// checkLocalNames refuses a name that two local entries share.
func checkLocalNames(file string, specs []localSpec) error {
	localSeen := map[string]bool{}
	for _, s := range specs {
		if localSeen[s.Name] {
			return fmt.Errorf("%s: repeats the name %q", file, s.Name)
		}
		localSeen[s.Name] = true
	}
	return nil
}

// sourceBytes returns the goenvoy manifest from the cache when its sha256 fits the pin, and
// downloads it otherwise.
func (r manifestReader) sourceBytes(ctx context.Context, s manifestSource) ([]byte, error) {
	if r.cache != "" {
		cachedFile := filepath.Join(r.cache, sourceManifestFile)
		// #nosec G304 -- the cache directory is the caller's argument; the file is hashed before use
		data, err := os.ReadFile(cachedFile)
		if err == nil && digest(data) == s.SHA256 {
			return data, nil
		}
	}
	data, err := r.downloadSource(ctx, s)
	if err != nil {
		return nil, err
	}
	if r.cache == "" {
		return data, nil
	}
	if err := os.MkdirAll(r.cache, 0o750); err != nil {
		return nil, fmt.Errorf("create cache: %w", err)
	}
	if err := os.WriteFile(filepath.Join(r.cache, sourceManifestFile), data, 0o600); err != nil {
		return nil, fmt.Errorf("cache the goenvoy manifest: %w", err)
	}
	return data, nil
}

// downloadSource downloads the goenvoy manifest at the pinned commit and refuses it when it is
// larger than the bound or its sha256 differs from the pin.
func (r manifestReader) downloadSource(ctx context.Context, s manifestSource) ([]byte, error) {
	sourceURL := s.sourceOf(r.base)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", sourceURL, err)
	}
	// #nosec G107 G704 -- the address is the base plus validated manifest source fields
	reply, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", sourceURL, err)
	}
	data, err := body(reply)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", sourceURL, err)
	}
	if len(data) > maxSpecBytes {
		return nil, fmt.Errorf("%s is larger than %d bytes; nothing was written", sourceURL, maxSpecBytes)
	}
	got := digest(data)
	if got != s.SHA256 {
		return nil, fmt.Errorf("goenvoy manifest at %s has sha256 %s, specs.yaml pins %s", s.Commit[:12], got, s.SHA256)
	}
	return data, nil
}

// decodeUpstream decodes the goenvoy manifest and indexes its entries by name.
func decodeUpstream(data []byte) (map[string]upstreamSpec, error) {
	var upstream upstreamManifest
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&upstream); err != nil {
		return nil, fmt.Errorf("upstream manifest: %w", err)
	}
	if len(upstream.Specs) == 0 {
		return nil, errors.New("upstream manifest has no \"specs\" entry")
	}
	byName := make(map[string]upstreamSpec, len(upstream.Specs))
	for _, u := range upstream.Specs {
		if _, exists := byName[u.Name]; exists {
			return nil, fmt.Errorf("upstream manifest repeats the name %q", u.Name)
		}
		byName[u.Name] = u
	}
	return byName, nil
}

// join takes the pin of each local entry from the goenvoy entry of the same name and refuses a
// local name goenvoy does not pin. Entries goenvoy pins and SnatchArr does not use are ignored.
func join(file string, local []localSpec, upstream map[string]upstreamSpec) ([]pin, error) {
	joined := make([]pin, 0, len(local))
	names, commits := map[string]bool{}, map[string]string{}
	for index, l := range local {
		up, found := upstream[l.Name]
		if !found {
			return nil, fmt.Errorf("%s: specs[%d] (%s): local name missing upstream in goenvoy manifest", file, index, l.Name)
		}
		p := pin{
			Name:       l.Name,
			Repository: up.Repository,
			Tag:        up.Tag,
			Branch:     up.Branch,
			Commit:     up.Commit,
			Path:       up.Path,
			SHA256:     up.SHA256,
			License:    up.License,
			Prefix:     up.Prefix,
			Operations: l.Operations,
			Commands:   l.Commands,
		}
		if problem := p.problem(); problem != "" {
			return nil, fmt.Errorf("%s: specs[%d] (%s): %s", file, index, p.Name, problem)
		}
		if problem := p.repeatProblem(names, commits); problem != "" {
			return nil, fmt.Errorf("%s: specs[%d] (%s): %s", file, index, p.Name, problem)
		}
		joined = append(joined, p)
	}
	return joined, nil
}

// repeatProblem refuses an entry that repeats the name of an earlier one, or its commit under
// another repository. A repository can carry tags of another project: Sonarr/Sonarr holds the
// v6 release tags of Radarr, so a tag lookup proposes a commit of the other project. Two
// projects never share a commit, so the second entry names the wrong one.
func (p pin) repeatProblem(names map[string]bool, commits map[string]string) string {
	owner, shared := commits[p.Commit]
	switch {
	case names[p.Name]:
		return "repeats the name of an earlier entry"
	case shared && owner != p.Repository:
		return fmt.Sprintf("repeats the commit of %s; a commit belongs to one repository, so this tag or branch names another project", owner)
	}
	names[p.Name], commits[p.Commit] = true, p.Repository
	return ""
}

func (p pin) problem() string {
	checks := []struct {
		ok   bool
		text string
	}{
		{pinName.MatchString(p.Name), "name must be lower-case letters, digits and hyphens"},
		{repository.MatchString(p.Repository), "repository must be <owner>/<name>"},
		{(p.Tag == "") != (p.Branch == ""), "exactly one of tag and branch must be set"},
		{p.Tag == "" || tagName.MatchString(p.Tag), "tag must name one release tag"},
		{p.Branch == "" || branchName.MatchString(p.Branch), "branch must name one branch"},
		{commitID.MatchString(p.Commit), "commit must be a full 40-digit commit id"},
		{cleanRelative(p.Path), "path must be one clean file path inside the upstream repository"},
		{sha256Hex.MatchString(p.SHA256), "sha256 must be 64 lower-case hex digits"},
		{licenseID.MatchString(p.License), "license must be the SPDX id of the upstream repository"},
		{prefixPath.MatchString(p.Prefix), "prefix must be empty or a path like /api/v1"},
		{len(p.Operations) > 0, "operations lists nothing, so the contract would check nothing"},
	}
	for _, check := range checks {
		if !check.ok {
			return check.text
		}
	}
	if problem := p.operationProblem(); problem != "" {
		return problem
	}
	return p.commandProblem()
}

func (p pin) operationProblem() string {
	seen := map[string]bool{}
	for _, entry := range p.Operations {
		_, route, _ := strings.Cut(entry, " ")
		switch {
		case !operation.MatchString(entry):
			return fmt.Sprintf("operation %q must be \"GET <path>\" or \"POST <path>\"", entry)
		case !strings.HasPrefix(route, p.Prefix+"/"):
			return fmt.Sprintf("operation %q does not start with the prefix %q", entry, p.Prefix)
		case seen[entry]:
			return fmt.Sprintf("operation %q is listed twice", entry)
		}
		seen[entry] = true
	}
	return ""
}

// commandProblem refuses a command class that is not pinned like a file, and an entry that sends
// commands (POST <prefix>/command) without pinning any class to compare the body with.
func (p pin) commandProblem() string {
	seen := map[string]bool{}
	for _, c := range p.Commands {
		switch {
		case !cleanRelative(c.Path) || !strings.HasSuffix(c.Path, ".cs"):
			return fmt.Sprintf("command %q must be one clean path of a .cs file", c.Path)
		case !sha256Hex.MatchString(c.SHA256):
			return fmt.Sprintf("command %q: sha256 must be 64 lower-case hex digits", c.Path)
		case seen[path.Base(c.Path)]:
			return fmt.Sprintf("command %q repeats the file name of an earlier command", c.Path)
		}
		seen[path.Base(c.Path)] = true
	}
	if len(p.Commands) == 0 && slices.Contains(p.Operations, "POST "+p.Prefix+"/command") {
		return "operations lists POST " + p.Prefix + "/command and commands pins no class, so the body would be checked against nothing"
	}
	return ""
}

// cleanRelative reports whether file names one path below a root: not absolute, no "..", no
// trailing slash, no "." segment.
func cleanRelative(file string) bool {
	if file == "" || path.IsAbs(file) || strings.ContainsAny(file, "\\ \t\r\n") {
		return false
	}
	return path.Clean(file) == file && file != "." && file != ".." && !strings.HasPrefix(file, "../")
}

// source is where the pinned spec is read from below base.
func (p pin) source(base string) string { return p.sourceOf(base, p.Path) }

// sourceOf is where the file at path, in the repository and at the commit of the pin, is read
// from below base.
func (p pin) sourceOf(base, file string) string {
	return strings.TrimSuffix(base, "/") + "/" + p.Repository + "/" + p.Commit + "/" + file
}

// ref is the tag or the branch the commit belongs to, as the summary of a fetch names it.
func (p pin) ref() string {
	if p.Branch != "" {
		return "branch " + p.Branch
	}
	return p.Tag
}

// specFile is the name of the cached spec inside the cache directory.
func (p pin) specFile() string { return p.Name + ".spec" }

// commandFile is the name of the cached class file of c inside the cache directory.
func (p pin) commandFile(c command) string { return p.Name + ".command." + path.Base(c.Path) }
