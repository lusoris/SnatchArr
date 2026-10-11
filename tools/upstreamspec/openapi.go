// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/lusoris/SnatchArr/tools/upstreamspec/contract"
)

const (
	schemaPrefix    = "#/components/schemas/"
	parameterPrefix = "#/components/parameters/"
	// maxNodes bounds one example; the largest one built from the pinned specs has a few thousand.
	maxNodes = 200_000
	// maxHops bounds the chain of $ref, anyOf and oneOf that leads to one schema.
	maxHops = 32
)

// document is the part of an OpenAPI 3.0 document the tool reads. YAML decodes JSON too.
type document struct {
	Paths      map[string]pathItem `yaml:"paths"`
	Components struct {
		Schemas    map[string]*schema   `yaml:"schemas"`
		Parameters map[string]parameter `yaml:"parameters"`
	} `yaml:"components"`
}

type pathItem struct {
	Get        *operationItem `yaml:"get"`
	Post       *operationItem `yaml:"post"`
	Parameters []parameter    `yaml:"parameters"`
}

type operationItem struct {
	Parameters []parameter         `yaml:"parameters"`
	Responses  map[string]response `yaml:"responses"`
}

type parameter struct {
	Ref  string `yaml:"$ref"`
	Name string `yaml:"name"`
	In   string `yaml:"in"`
}

type response struct {
	Content map[string]struct {
		Schema *schema `yaml:"schema"`
	} `yaml:"content"`
}

// schema is one schema object. unsupported holds the keywords that change which values are valid
// and that the example builder does not read; a schema with one of them is refused when an
// example needs it.
type schema struct {
	Ref         string
	Type        string
	Format      string
	Enum        []any
	Properties  map[string]*schema
	Items       *schema
	AnyOf       []*schema
	OneOf       []*schema
	unsupported []string
}

// annotations are the keywords that do not change which properties an example has.
var annotations = []string{
	"additionalProperties", "default", "deprecated", "description", "example", "examples",
	"exclusiveMaximum", "exclusiveMinimum", "externalDocs", "maxItems", "maxLength",
	"maxProperties", "maximum", "minItems", "minLength", "minProperties", "minimum", "multipleOf",
	"nullable", "pattern", "readOnly", "required", "title", "uniqueItems", "writeOnly", "xml",
}

// UnmarshalYAML decodes the keywords the builder reads and records every other keyword that is no
// annotation.
func (s *schema) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: a schema must be a mapping", node.Line)
	}
	targets := map[string]any{
		"$ref": &s.Ref, "type": &s.Type, "format": &s.Format, "enum": &s.Enum,
		"properties": &s.Properties, "items": &s.Items, "anyOf": &s.AnyOf, "oneOf": &s.OneOf,
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		target, read := targets[key]
		switch {
		case read:
			if err := value.Decode(target); err != nil {
				return fmt.Errorf("line %d: schema keyword %q: %w", value.Line, key, err)
			}
		case strings.HasPrefix(key, "x-") || slices.Contains(annotations, key):
		default:
			s.unsupported = append(s.unsupported, key)
		}
	}
	return nil
}

func parseDocument(data []byte) (*document, error) {
	var doc document
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if len(doc.Paths) == 0 {
		return nil, errors.New("the document has no paths")
	}
	return &doc, nil
}

// contractOf builds the contract of p from its spec: for each operation of the manifest the
// documented query parameters and an example of the documented response.
func (d *document) contractOf(p pin) (contract.File, error) {
	file := contract.File{Spec: p.Name, Tag: p.Tag, Branch: p.Branch, Commit: p.Commit, Operations: map[string]contract.Operation{}}
	for _, entry := range p.Operations {
		method, route, _ := strings.Cut(entry, " ")
		item, ok := d.Paths[strings.TrimPrefix(route, p.Prefix)]
		if !ok {
			return contract.File{}, fmt.Errorf("%s: the spec has no such path", entry)
		}
		described := item.Get
		if method == "POST" {
			described = item.Post
		}
		if described == nil {
			return contract.File{}, fmt.Errorf("%s: the spec has no such method on that path", entry)
		}
		built, err := d.describe(item, described)
		if err != nil {
			return contract.File{}, fmt.Errorf("%s: %w", entry, err)
		}
		file.Operations[entry] = built
	}
	return file, nil
}

func (d *document) describe(item pathItem, described *operationItem) (contract.Operation, error) {
	names, err := d.queryNames(slices.Concat(item.Parameters, described.Parameters))
	if err != nil {
		return contract.Operation{}, err
	}
	root, err := described.responseSchema()
	if err != nil {
		return contract.Operation{}, err
	}
	example, err := d.example(root)
	if err != nil {
		return contract.Operation{}, fmt.Errorf("response%w", err)
	}
	encoded, err := json.Marshal(example)
	if err != nil {
		return contract.Operation{}, fmt.Errorf("encode example: %w", err)
	}
	return contract.Operation{Parameters: names, Response: encoded}, nil
}

// queryNames returns the sorted names of the query parameters among parameters.
func (d *document) queryNames(parameters []parameter) ([]string, error) {
	names := []string{}
	for _, p := range parameters {
		if p.Ref != "" {
			name, ok := strings.CutPrefix(p.Ref, parameterPrefix)
			resolved, found := d.Components.Parameters[name]
			if !ok || !found || resolved.Ref != "" {
				return nil, fmt.Errorf("parameter reference %q does not name a parameter of the document", p.Ref)
			}
			p = resolved
		}
		if p.In == "query" {
			names = append(names, p.Name)
		}
	}
	sort.Strings(names)
	return slices.Compact(names), nil
}

// responseSchema returns the JSON schema of the successful response with the lowest status code.
func (o *operationItem) responseSchema() (*schema, error) {
	codes := make([]string, 0, len(o.Responses))
	for code := range o.Responses {
		if strings.HasPrefix(code, "2") {
			codes = append(codes, code)
		}
	}
	if len(codes) == 0 {
		return nil, errors.New("the spec documents no successful response")
	}
	sort.Strings(codes)
	content := o.Responses[codes[0]].Content
	for _, mediaType := range []string{"application/json", "text/json"} {
		if media, ok := content[mediaType]; ok && media.Schema != nil {
			return media.Schema, nil
		}
	}
	return nil, fmt.Errorf("response %s documents no JSON schema", codes[0])
}

// job is one schema still to be turned into a value. put stores the value in its parent, open
// lists the named schemas that are being built above it, at is its path for an error.
type job struct {
	schema *schema
	put    func(any)
	open   []string
	at     string
}

// example returns a value in which every property the schema documents is present: one element
// per array, the first value of an enumeration, the first alternative of anyOf and oneOf. A named
// schema that contains itself is cut off with null at the inner occurrence.
func (d *document) example(root *schema) (any, error) {
	var result any
	queue := []job{{schema: root, put: func(value any) { result = value }}}
	for nodes := 0; len(queue) > 0; nodes++ {
		if nodes == maxNodes {
			return nil, fmt.Errorf(": the example needs more than %d values", maxNodes)
		}
		current := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		next, err := d.expand(current)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", current.at, err)
		}
		queue = append(queue, next...)
	}
	return result, nil
}

// expand stores the value of one job and returns the jobs for the values inside it.
func (d *document) expand(j job) ([]job, error) {
	s, open, err := d.resolve(j.schema, j.open)
	if err != nil {
		return nil, err
	}
	switch {
	case s == nil:
		j.put(nil)
	case len(s.unsupported) > 0:
		return nil, fmt.Errorf("schema keyword %q is not supported", s.unsupported[0])
	case len(s.Enum) > 0:
		j.put(s.Enum[0])
	case s.Type == "object" || len(s.Properties) > 0:
		object := make(map[string]any, len(s.Properties))
		j.put(object)
		next := make([]job, 0, len(s.Properties))
		for name, property := range s.Properties {
			next = append(next, job{schema: property, put: func(value any) { object[name] = value }, open: open, at: j.at + "." + name})
		}
		return next, nil
	case s.Type == "array":
		list := make([]any, 1)
		j.put(list)
		return []job{{schema: s.Items, put: func(value any) { list[0] = value }, open: open, at: j.at + "[]"}}, nil
	default:
		value, err := scalar(s)
		if err != nil {
			return nil, err
		}
		j.put(value)
	}
	return nil, nil
}

// resolve follows $ref, anyOf and oneOf to the schema that describes the value. It returns nil
// for a named schema that is already being built above.
func (d *document) resolve(s *schema, open []string) (*schema, []string, error) {
	for range maxHops {
		switch {
		case s == nil:
			return nil, nil, errors.New("a schema is missing (array without items, or an empty alternative)")
		case s.Ref != "":
			name, ok := strings.CutPrefix(s.Ref, schemaPrefix)
			target, found := d.Components.Schemas[name]
			if !ok || !found || target == nil {
				return nil, nil, fmt.Errorf("reference %q does not name a schema of the document", s.Ref)
			}
			if slices.Contains(open, name) {
				return nil, open, nil
			}
			s, open = target, append(slices.Clone(open), name)
		case len(s.AnyOf) > 0:
			s = s.AnyOf[0]
		case len(s.OneOf) > 0:
			s = s.OneOf[0]
		default:
			return s, open, nil
		}
	}
	return nil, nil, fmt.Errorf("more than %d references in a row", maxHops)
}

// scalar returns the example of a schema that is neither object nor array. A schema without a
// type accepts any value and yields null.
func scalar(s *schema) (any, error) {
	switch s.Type {
	case "":
		return nil, nil //nolint:nilnil // null is the example of a schema that accepts any value
	case "boolean":
		return true, nil
	case "integer", "number":
		return 1, nil
	case "string":
		formats := map[string]string{
			"date-time": "2020-01-02T03:04:05Z",
			"date":      "2020-01-02",
			"date-span": "00:00:00",
			"uuid":      "00000000-0000-4000-8000-000000000000",
		}
		if value, ok := formats[s.Format]; ok {
			return value, nil
		}
		return "string", nil
	default:
		return nil, fmt.Errorf("schema type %q is not supported", s.Type)
	}
}
