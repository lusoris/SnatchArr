// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"gopkg.in/yaml.v3"
)

var (
	// ErrMissingOpID indicates an operation is missing operationId.
	ErrMissingOpID = errors.New("missing operationId")
	// ErrInvalidRef indicates a $ref could not be resolved.
	ErrInvalidRef = errors.New("unresolved reference")
)

// Spec represents parsed OpenAPI 3.1 subsets.
type Spec struct {
	Paths      map[string]map[string]any `yaml:"paths"`
	Components Components                `yaml:"components"`
}

// Components holds schemas, parameters, and requestBodies.
type Components struct {
	Schemas       map[string]any
	Parameters    map[string]any
	RequestBodies map[string]any
}

// UnmarshalYAML decodes components dynamically to avoid snake_case tagging conflicts.
func (c *Components) UnmarshalYAML(value *yaml.Node) error {
	var raw map[string]map[string]any
	if err := value.Decode(&raw); err != nil {
		return fmt.Errorf("decode components: %w", err)
	}
	c.Schemas = raw["schemas"]
	c.Parameters = raw["parameters"]
	c.RequestBodies = raw["requestBodies"]
	return nil
}

// Generate reads OpenAPI YAML and produces a JSON document of self-contained JSON Schemas.
func Generate(src []byte) ([]byte, error) {
	var spec Spec
	if err := yaml.Unmarshal(src, &spec); err != nil {
		return nil, fmt.Errorf("unmarshal openapi yaml: %w", err)
	}
	result := make(map[string]any)
	paths := slices.Sorted(maps.Keys(spec.Paths))
	for _, p := range paths {
		pathItem := spec.Paths[p]
		if err := processPathItem(p, pathItem, spec.Components, result); err != nil {
			return nil, err
		}
	}
	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal schemas json: %w", err)
	}
	return append(out, '\n'), nil
}

func processPathItem(path string, pathItem map[string]any, comps Components, result map[string]any) error {
	commonParams, err := extractParams(pathItem["parameters"])
	if err != nil {
		return fmt.Errorf("path %s common params: %w", path, err)
	}
	methods := []string{"get", "put", "post", "delete", "patch"}
	for _, m := range methods {
		rawOp, ok := pathItem[m]
		if !ok {
			continue
		}
		opMap, ok := rawOp.(map[string]any)
		if !ok {
			continue
		}
		opID, schema, err := buildOperationSchema(m, path, opMap, commonParams, comps)
		if err != nil {
			return err
		}
		result[opID] = schema
	}
	return nil
}

func extractParams(raw any) ([]any, error) {
	if raw == nil {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: parameters must be a list", ErrInvalidRef)
	}
	return list, nil
}
