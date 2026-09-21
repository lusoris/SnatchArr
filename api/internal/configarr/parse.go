// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package configarr imports *arr instances from a Configarr config.yml (with its
// secrets.yml), so instances are defined once (ADR-0003). Only base_url, api_key and the
// enabled flags are read; everything else in the file belongs to Configarr.
package configarr

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Apps lists the Configarr sections that define instances, in import order.
var Apps = []string{"sonarr", "radarr", "lidarr", "readarr", "whisparr"}

// MaxFileValue bounds a `!file` value (HISS-02).
const MaxFileValue = 64 * 1024

// maxYAMLBytes bounds the files read (HISS-02).
const maxYAMLBytes = 4 * 1024 * 1024

// Definition is one instance as Configarr defines it.
type Definition struct {
	// Key is "<app>/<name>", stable across imports.
	Key     string
	App     string
	Name    string
	BaseURL string
	APIKey  string
	Enabled bool
}

// Parsed is the result of reading a Configarr configuration.
type Parsed struct {
	Instances []Definition
	Warnings  []string
}

// ErrParse marks a configuration that could not be read.
var ErrParse = errors.New("configarr: parse")

// Parser resolves tagged values against secrets, the environment and files.
type Parser struct {
	secrets   map[string]string
	lookupEnv func(string) (string, bool)
	baseDir   string
}

// Parse reads config.yml (and secrets.yml when given) and returns the instances.
func Parse(configPath, secretsPath string) (Parsed, error) {
	return ParseWithEnv(configPath, secretsPath, os.LookupEnv)
}

// ParseWithEnv is Parse with an injectable environment (tests).
func ParseWithEnv(configPath, secretsPath string, lookupEnv func(string) (string, bool)) (Parsed, error) {
	p := &Parser{secrets: map[string]string{}, lookupEnv: lookupEnv, baseDir: filepath.Dir(configPath)}
	if secretsPath != "" {
		if err := p.loadSecrets(secretsPath); err != nil {
			return Parsed{}, err
		}
	}
	raw, err := readBounded(configPath, maxYAMLBytes)
	if err != nil {
		return Parsed{}, fmt.Errorf("%w: %w", ErrParse, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return Parsed{}, fmt.Errorf("%w: %s: %w", ErrParse, filepath.Base(configPath), err)
	}
	root := documentRoot(&doc)
	if root == nil || root.Kind != yaml.MappingNode {
		return Parsed{}, fmt.Errorf("%w: %s: top level must be a mapping", ErrParse, filepath.Base(configPath))
	}
	return p.parseRoot(root)
}

func (p *Parser) loadSecrets(path string) error {
	raw, err := readBounded(path, maxYAMLBytes)
	if err != nil {
		return fmt.Errorf("%w: secrets: %w", ErrParse, err)
	}
	var m map[string]string
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("%w: secrets: %w", ErrParse, err)
	}
	p.secrets = m
	return nil
}

func readBounded(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, limit)
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- operator-supplied config path
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return raw, nil
}

func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	return doc
}

// mappingEntries walks a mapping node's key/value pairs.
func mappingEntries(n *yaml.Node) [][2]*yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	out := make([][2]*yaml.Node, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, [2]*yaml.Node{n.Content[i], n.Content[i+1]})
	}
	return out
}

// collectSections finds the per-app instance maps and the app-level enabled switches.
func collectSections(root *yaml.Node) (map[string]*yaml.Node, map[string]bool) {
	sections := map[string]*yaml.Node{}
	disabled := map[string]bool{}
	for _, kv := range mappingEntries(root) {
		key := kv[0].Value
		for _, app := range Apps {
			if key == app {
				sections[app] = kv[1]
			}
			if key == app+"Enabled" && strings.EqualFold(kv[1].Value, "false") {
				disabled[app] = true
			}
		}
	}
	return sections, disabled
}

func (p *Parser) parseRoot(root *yaml.Node) (Parsed, error) {
	var out Parsed
	sections, disabledApps := collectSections(root)
	for _, app := range Apps {
		section, ok := sections[app]
		if !ok || disabledApps[app] {
			continue
		}
		if err := p.parseApp(app, section, &out); err != nil {
			return Parsed{}, err
		}
	}
	return out, nil
}

func (p *Parser) parseApp(app string, section *yaml.Node, out *Parsed) error {
	if section.Kind != yaml.MappingNode {
		return fmt.Errorf("%w: %s must be a mapping of instances", ErrParse, app)
	}
	for _, kv := range mappingEntries(section) {
		name := kv[0].Value
		def, err := p.parseInstance(app, name, kv[1])
		if err != nil {
			return err
		}
		if def.BaseURL == "" || def.APIKey == "" {
			out.Warnings = append(out.Warnings, fmt.Sprintf("%s/%s: base_url or api_key missing; skipped", app, name))
			continue
		}
		out.Instances = append(out.Instances, def)
	}
	return nil
}

func (p *Parser) parseInstance(app, name string, node *yaml.Node) (Definition, error) {
	def := Definition{Key: app + "/" + name, App: app, Name: name, Enabled: true}
	if node.Kind != yaml.MappingNode {
		return Definition{}, fmt.Errorf("%w: %s/%s must be a mapping", ErrParse, app, name)
	}
	for _, kv := range mappingEntries(node) {
		var err error
		switch kv[0].Value {
		case "base_url":
			def.BaseURL, err = p.resolve(def.Key+".base_url", kv[1])
		case "api_key":
			def.APIKey, err = p.resolve(def.Key+".api_key", kv[1])
		case "enabled":
			def.Enabled = !strings.EqualFold(kv[1].Value, "false")
		}
		if err != nil {
			return Definition{}, err
		}
	}
	return def, nil
}

// resolve turns a scalar node into its value, honouring Configarr's !secret, !env and
// !file tags.
func (p *Parser) resolve(path string, n *yaml.Node) (string, error) {
	if n.Kind != yaml.ScalarNode {
		return "", fmt.Errorf("%w: %s must be a scalar", ErrParse, path)
	}
	arg := strings.TrimSpace(n.Value)
	switch n.Tag {
	case "!secret":
		v, ok := p.secrets[arg]
		if !ok {
			return "", fmt.Errorf("%w: %s: secret %q not found in secrets.yml", ErrParse, path, arg)
		}
		return strings.TrimSpace(v), nil
	case "!env":
		v, ok := p.lookupEnv(arg)
		if !ok {
			return "", fmt.Errorf("%w: %s: environment variable %q is not set", ErrParse, path, arg)
		}
		return strings.TrimSpace(v), nil
	case "!file":
		file := arg
		if !filepath.IsAbs(file) {
			file = filepath.Join(p.baseDir, file)
		}
		raw, err := readBounded(file, MaxFileValue)
		if err != nil {
			return "", fmt.Errorf("%w: %s: %w", ErrParse, path, err)
		}
		return strings.TrimSpace(string(raw)), nil
	case "", "!", "!!str", "!!int", "!!bool", "!!null":
		return arg, nil
	default:
		return "", fmt.Errorf("%w: %s: unsupported tag %s", ErrParse, path, n.Tag)
	}
}
