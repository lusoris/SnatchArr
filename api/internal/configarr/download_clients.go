// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package configarr

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// MaxDownloadClientEntries bounds the download client entries parsed (HISS-02).
const MaxDownloadClientEntries = 256

// ClientDefinition is one download client defined in Configarr linked mode.
type ClientDefinition struct {
	Key                string // e.g. "qbittorrent/main"
	Kind               domain.DownloadClientKind
	Name               string // e.g. "main"
	BaseURL            string
	Username           string
	Secret             string // password or api_key
	InstanceKey        string // optional Configarr instance key
	MaxActive          int
	BandwidthBudgetBPS int64
	Enabled            bool
}

// ParsedClients holds the parsed download clients and non-fatal warnings.
type ParsedClients struct {
	Clients  []ClientDefinition
	Warnings []string
}

type secretFlags struct {
	hasPassword bool
	hasAPIKey   bool
}

// ParseDownloadClients reads the download clients YAML file and resolves credentials.
func ParseDownloadClients(path, secretsPath string, lookupEnv func(string) (string, bool)) (ParsedClients, error) {
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	p := &Parser{secrets: map[string]string{}, lookupEnv: lookupEnv, baseDir: filepath.Dir(path)}
	if secretsPath != "" {
		if err := p.loadSecrets(secretsPath); err != nil {
			return ParsedClients{}, err
		}
	}
	raw, err := readBounded(path, maxYAMLBytes)
	if err != nil {
		return ParsedClients{}, fmt.Errorf("%w: %w", ErrParse, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return ParsedClients{}, fmt.Errorf("%w: %s: %w", ErrParse, filepath.Base(path), err)
	}
	root := documentRoot(&doc)
	if root == nil || root.Kind != yaml.MappingNode {
		return ParsedClients{}, fmt.Errorf("%w: %s: top level must be a mapping", ErrParse, filepath.Base(path))
	}
	return parseDownloadClientsRoot(p, root, filepath.Base(path))
}

func parseDownloadClientsRoot(p *Parser, root *yaml.Node, fileBase string) (ParsedClients, error) {
	var out ParsedClients
	total := 0
	for _, kv := range mappingEntries(root) {
		sectionName := kv[0].Value
		kind, err := domain.ParseDownloadClientKind(sectionName)
		if err != nil {
			return ParsedClients{}, fmt.Errorf("%w: %s: unknown section %q", ErrParse, fileBase, sectionName)
		}
		if err := parseClientSection(p, kind, kv[1], &total, &out); err != nil {
			return ParsedClients{}, err
		}
	}
	return out, nil
}

func parseClientSection(p *Parser, kind domain.DownloadClientKind, section *yaml.Node, total *int, out *ParsedClients) error {
	if section.Kind != yaml.MappingNode {
		return fmt.Errorf("%w: %s must be a mapping of download clients", ErrParse, kind)
	}
	for _, kv := range mappingEntries(section) {
		*total++
		if *total > MaxDownloadClientEntries {
			return fmt.Errorf("%w: exceeds maximum of %d download client entries", ErrParse, MaxDownloadClientEntries)
		}
		name := kv[0].Value
		def, ok, err := parseClientEntry(p, kind, name, kv[1])
		if err != nil {
			return err
		}
		if !ok {
			out.Warnings = append(out.Warnings, fmt.Sprintf("%s/%s: base_url or secret missing; skipped", kind, name))
			continue
		}
		out.Clients = append(out.Clients, def)
	}
	return nil
}

func parseClientEntry(p *Parser, kind domain.DownloadClientKind, name string, node *yaml.Node) (ClientDefinition, bool, error) {
	key := string(kind) + "/" + name
	if node.Kind != yaml.MappingNode {
		return ClientDefinition{}, false, fmt.Errorf("%w: %s must be a mapping", ErrParse, key)
	}
	def := ClientDefinition{
		Key:     key,
		Kind:    kind,
		Name:    name,
		Enabled: true,
	}
	var flags secretFlags
	for _, kv := range mappingEntries(node) {
		if err := parseField(p, key, kv[0].Value, kv[1], &def, &flags); err != nil {
			return ClientDefinition{}, false, err
		}
	}
	if flags.hasPassword && flags.hasAPIKey {
		return ClientDefinition{}, false, fmt.Errorf("%w: %s: cannot specify both api_key and password", ErrParse, key)
	}
	if def.BaseURL == "" || (!flags.hasPassword && !flags.hasAPIKey) || def.Secret == "" {
		return ClientDefinition{Key: key}, false, nil
	}
	return def, true, nil
}

func parseField(p *Parser, key, field string, node *yaml.Node, def *ClientDefinition, flags *secretFlags) error {
	var err error
	switch field {
	case "base_url":
		def.BaseURL, err = p.resolve(key+".base_url", node)
	case "username":
		def.Username, err = p.resolve(key+".username", node)
	case "password":
		flags.hasPassword = true
		def.Secret, err = p.resolve(key+".password", node)
	case "api_key":
		flags.hasAPIKey = true
		def.Secret, err = p.resolve(key+".api_key", node)
	case "instance":
		def.InstanceKey, err = p.resolve(key+".instance", node)
	case "enabled":
		b, bErr := strconv.ParseBool(strings.TrimSpace(node.Value))
		if bErr != nil {
			return fmt.Errorf("%w: %s: enabled must be true or false", ErrParse, key)
		}
		def.Enabled = b
	case "max_active":
		def.MaxActive, err = parseMaxActive(key, node.Value)
	case "bandwidth_budget_bps":
		def.BandwidthBudgetBPS, err = parseBandwidth(key, node.Value)
	default:
		return fmt.Errorf("%w: %s: unknown field %q", ErrParse, key, field)
	}
	return err
}

func parseMaxActive(key, val string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(val))
	if err != nil || n < 0 || n > domain.MaxActiveDownloadsLimit {
		return 0, fmt.Errorf("%w: %s: max_active must be between 0 and %d", ErrParse, key, domain.MaxActiveDownloadsLimit)
	}
	return n, nil
}

func parseBandwidth(key, val string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%w: %s: bandwidth_budget_bps must be >= 0", ErrParse, key)
	}
	return n, nil
}
