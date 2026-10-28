// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"fmt"
	"slices"
)

func buildOperationSchema(method, path string, opMap map[string]any, commonParams []any, comps Components) (string, map[string]any, error) {
	opID, _ := opMap["operationId"].(string)
	if opID == "" {
		return "", nil, fmt.Errorf("%w: %s %s", ErrMissingOpID, method, path)
	}
	opParams, err := extractParams(opMap["parameters"])
	if err != nil {
		return "", nil, fmt.Errorf("%s %s params: %w", method, path, err)
	}
	allParams := append(append([]any(nil), commonParams...), opParams...)
	props := make(map[string]any)
	required := make([]string, 0)
	defs := make(map[string]any)

	if err := buildParams(allParams, comps, props, &required); err != nil {
		return "", nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	if err := buildRequestBody(opMap["requestBody"], comps, props, &required, defs); err != nil {
		return "", nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	if isDestructive(opMap) {
		props["confirm"] = map[string]any{
			"type":        "boolean",
			"description": "Must be true to confirm this destructive operation",
		}
	}
	return opID, finishSchema(props, required, defs), nil
}

func isDestructive(opMap map[string]any) bool {
	isDestr, ok := opMap["x-destructive"].(bool)
	return ok && isDestr
}

func finishSchema(props map[string]any, req []string, defs map[string]any) map[string]any {
	schema := map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(req) > 0 {
		slices.Sort(req)
		schema["required"] = req
	}
	if len(defs) > 0 {
		schema["$defs"] = defs
	}
	return schema
}

func buildParams(params []any, comps Components, props map[string]any, req *[]string) error {
	for _, p := range params {
		pMap, err := resolveParamRef(p, comps)
		if err != nil {
			return err
		}
		in, _ := pMap["in"].(string)
		name, _ := pMap["name"].(string)
		if name == "" || (in != "path" && in != "query") {
			continue
		}
		paramProp, err := buildParamProperty(pMap, comps)
		if err != nil {
			return err
		}
		props[name] = paramProp
		isReq, _ := pMap["required"].(bool)
		if in == "path" || isReq {
			*req = append(*req, name)
		}
	}
	return nil
}

// allowedParamProps are the scalar schema keywords a parameter keeps. No keyword
// that can hold a nested $ref (items, properties) is copied: it would leave an
// unresolved reference in the tool schema.
var allowedParamProps = []string{
	"type",
	"format",
	"description",
	"minimum",
	"maximum",
	"exclusiveMinimum",
	"exclusiveMaximum",
	"enum",
	"pattern",
	"minLength",
	"maxLength",
	"default",
}

func buildParamProperty(pMap map[string]any, comps Components) (map[string]any, error) {
	prop := make(map[string]any)
	rawSchema := pMap["schema"]
	if rawSchema != nil {
		schemaMap, err := resolveSchemaRef(rawSchema, comps)
		if err != nil {
			return nil, err
		}
		for _, k := range allowedParamProps {
			if v, ok := schemaMap[k]; ok {
				prop[k] = v
			}
		}
	}
	if _, ok := prop["type"]; !ok {
		prop["type"] = "string"
	}
	if desc, ok := pMap["description"].(string); ok && desc != "" {
		prop["description"] = desc
	}
	return prop, nil
}

func resolveParamRef(raw any, comps Components) (map[string]any, error) {
	return resolveComponentRef(raw, "parameter", "#/components/parameters/", comps.Parameters)
}
