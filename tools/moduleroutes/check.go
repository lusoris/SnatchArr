// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"
)

// Finding represents a single rule violation in an OpenAPI operation.
type Finding struct {
	Path        string
	Method      string
	OperationID string
	Reason      string
}

// coreTags defines the standard core tags declared in api/openapi/openapi.yaml.
// Operations tagged with any tag outside this core set must declare an x-module vendor extension.
var coreTags = map[string]struct{}{
	"system":           {},
	"auth":             {},
	"instances":        {},
	"snatches":         {},
	"schedules":        {},
	"download-clients": {},
	"seerr":            {},
	"prowlarr":         {},
	"configarr":        {},
	"notifications":    {},
	"requests":         {},
}

var httpMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

type rawOpenAPISpec struct {
	Paths map[string]map[string]any `yaml:"paths"`
}

// CheckSpec audits OpenAPI operations against module rules (a), (b), and (c).
func CheckSpec(src []byte, regSet map[string]struct{}) ([]Finding, error) {
	var spec rawOpenAPISpec
	if err := yaml.Unmarshal(src, &spec); err != nil {
		return nil, fmt.Errorf("unmarshal openapi yaml: %w", err)
	}
	var findings []Finding
	paths := slices.Sorted(maps.Keys(spec.Paths))
	for _, path := range paths {
		pathItem := spec.Paths[path]
		for _, method := range httpMethods {
			rawOp, ok := pathItem[method]
			if !ok {
				continue
			}
			opMap, ok := rawOp.(map[string]any)
			if !ok {
				continue
			}
			findings = append(findings, validateOperation(path, method, opMap, regSet)...)
		}
	}
	return findings, nil
}

func validateOperation(path, method string, opMap map[string]any, regSet map[string]struct{}) []Finding {
	opID, _ := opMap["operationId"].(string)
	rawModule, hasModule := opMap["x-module"]
	if !hasModule {
		return checkNonCoreTags(path, method, opID, opMap["tags"])
	}
	return checkModuleValue(path, method, opID, rawModule, regSet)
}

func checkNonCoreTags(path, method, opID string, rawTags any) []Finding {
	tagsList, ok := rawTags.([]any)
	if !ok {
		return nil
	}
	for _, rawTag := range tagsList {
		tagStr, ok := rawTag.(string)
		if !ok {
			continue
		}
		if _, isCore := coreTags[tagStr]; !isCore {
			return []Finding{
				{
					Path:        path,
					Method:      method,
					OperationID: opID,
					Reason:      fmt.Sprintf("missing x-module for non-core tag %q", tagStr),
				},
			}
		}
	}
	return nil
}

func checkModuleValue(path, method, opID string, rawModule any, regSet map[string]struct{}) []Finding {
	modStr, ok := rawModule.(string)
	if !ok || modStr == "" {
		return []Finding{
			{
				Path:        path,
				Method:      method,
				OperationID: opID,
				Reason:      "x-module must be a non-empty string",
			},
		}
	}
	if _, isReg := regSet[modStr]; !isReg {
		return []Finding{
			{
				Path:        path,
				Method:      method,
				OperationID: opID,
				Reason:      fmt.Sprintf("unregistered module id %q", modStr),
			},
		}
	}
	return nil
}

func executeCheck(inPath string, out io.Writer) (int, error) {
	cleanIn := filepath.Clean(inPath)
	// #nosec G304 G703 -- path provided via CLI flag
	src, err := os.ReadFile(cleanIn)
	if err != nil {
		return 1, fmt.Errorf("read %s: %w", cleanIn, err)
	}
	repoRoot, err := findRepoRoot(filepath.Dir(cleanIn))
	if err != nil {
		return 1, fmt.Errorf("find repo root: %w", err)
	}
	regIDs, err := LoadRegisteredModuleIDs(repoRoot)
	if err != nil {
		return 1, fmt.Errorf("load registered modules: %w", err)
	}
	regSet := make(map[string]struct{}, len(regIDs))
	for _, id := range regIDs {
		regSet[id] = struct{}{}
	}
	findings, err := CheckSpec(src, regSet)
	if err != nil {
		return 1, fmt.Errorf("check spec: %w", err)
	}
	if len(findings) > 0 {
		for _, f := range findings {
			if _, err := fmt.Fprintf(out, "%s %s %s %s\n", f.Path, f.Method, f.OperationID, f.Reason); err != nil {
				return 1, fmt.Errorf("write finding: %w", err)
			}
		}
		return 1, nil
	}
	return 0, nil
}
