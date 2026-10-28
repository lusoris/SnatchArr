// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

func buildRequestBody(rawBody any, comps Components, props map[string]any, req *[]string, defs map[string]any) error {
	if rawBody == nil {
		return nil
	}
	bodyMap, err := resolveBodyRef(rawBody, comps)
	if err != nil {
		return err
	}
	content, _ := bodyMap["content"].(map[string]any)
	if content == nil {
		return nil
	}
	appJSON, _ := content["application/json"].(map[string]any)
	if appJSON == nil {
		return nil
	}
	schema, ok := appJSON["schema"].(map[string]any)
	if !ok {
		return nil
	}
	clonedBody := cloneValue(schema)
	if err := collectDefs(clonedBody, comps, defs); err != nil {
		return err
	}
	rewriteRefsInPlace(clonedBody)
	for _, defVal := range defs {
		rewriteRefsInPlace(defVal)
	}
	props["body"] = clonedBody
	if isReq, _ := bodyMap["required"].(bool); isReq {
		*req = append(*req, "body")
	}
	return nil
}

func resolveComponentRef(raw any, kind, prefix string, store map[string]any) (map[string]any, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: %s must be an object", ErrInvalidRef, kind)
	}
	ref, _ := m["$ref"].(string)
	if ref == "" {
		return m, nil
	}
	name, ok := strings.CutPrefix(ref, prefix)
	if !ok {
		return nil, fmt.Errorf("%w: unsupported %s ref %q", ErrInvalidRef, kind, ref)
	}
	target, exists := store[name]
	if !exists {
		return nil, fmt.Errorf("%w: unknown %s ref %q", ErrInvalidRef, kind, ref)
	}
	targetMap, ok := target.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: referenced %s %q is invalid", ErrInvalidRef, kind, ref)
	}
	return targetMap, nil
}

func resolveBodyRef(raw any, comps Components) (map[string]any, error) {
	return resolveComponentRef(raw, "requestBody", "#/components/requestBodies/", comps.RequestBodies)
}

func resolveSchemaRef(raw any, comps Components) (map[string]any, error) {
	return resolveComponentRef(raw, "schema", "#/components/schemas/", comps.Schemas)
}

func collectDefs(root any, comps Components, defs map[string]any) error {
	queue := filterSchemaRefs(findRefs(root))
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if _, exists := defs[name]; exists {
			continue
		}
		target, ok := comps.Schemas[name]
		if !ok {
			return fmt.Errorf("%w: unknown schema ref %q", ErrInvalidRef, name)
		}
		cloned := cloneValue(target)
		defs[name] = cloned
		queue = append(queue, filterSchemaRefs(findRefs(cloned))...)
	}
	return nil
}

func findRefs(root any) []string {
	var refs []string
	worklist := []any{root}
	for len(worklist) > 0 {
		curr := worklist[len(worklist)-1]
		worklist = worklist[:len(worklist)-1]
		switch val := curr.(type) {
		case map[string]any:
			appendMapRefs(val, &refs, &worklist)
		case []any:
			worklist = append(worklist, val...)
		}
	}
	return refs
}

func appendMapRefs(m map[string]any, refs *[]string, worklist *[]any) {
	for k, v := range m {
		if k == "$ref" {
			if refStr, ok := v.(string); ok {
				*refs = append(*refs, refStr)
			}
			continue
		}
		*worklist = append(*worklist, v)
	}
}

func filterSchemaRefs(refs []string) []string {
	const prefix = "#/components/schemas/"
	var names []string
	for _, r := range refs {
		if after, ok := strings.CutPrefix(r, prefix); ok {
			names = append(names, after)
		}
	}
	return names
}

func rewriteRefsInPlace(root any) {
	worklist := []any{root}
	for len(worklist) > 0 {
		curr := worklist[len(worklist)-1]
		worklist = worklist[:len(worklist)-1]
		switch val := curr.(type) {
		case map[string]any:
			processMapForRewrite(val, &worklist)
		case []any:
			worklist = append(worklist, val...)
		}
	}
}

func processMapForRewrite(m map[string]any, worklist *[]any) {
	for k, child := range m {
		if k == "$ref" {
			rewriteRefEntry(m, child)
			continue
		}
		*worklist = append(*worklist, child)
	}
}

func rewriteRefEntry(m map[string]any, child any) {
	refStr, ok := child.(string)
	if !ok {
		return
	}
	const prefix = "#/components/schemas/"
	if after, ok := strings.CutPrefix(refStr, prefix); ok {
		m["$ref"] = "#/$defs/" + after
	}
}

func cloneValue(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return v
	}
	return out
}
