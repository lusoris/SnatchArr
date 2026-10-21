// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package llm

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed schemas_gen.json
var rawSchemas []byte

var (
	schemasOnce sync.Once
	schemasByOp map[string]json.RawMessage
	schemasErr  error
)

func initSchemas() {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(rawSchemas, &m); err != nil {
		schemasErr = fmt.Errorf("llm: unmarshal embedded schemas: %w", err)
		return
	}
	schemasByOp = m
}

// SchemaFor returns the self-contained JSON schema for the given operationId.
func SchemaFor(operationID string) (json.RawMessage, error) {
	schemasOnce.Do(initSchemas)
	if schemasErr != nil {
		return nil, schemasErr
	}
	s, ok := schemasByOp[operationID]
	if !ok {
		return nil, fmt.Errorf("llm: no schema found for operation %q", operationID)
	}
	return s, nil
}

func resolveToolSchemas(specs []toolSpec) (map[string]json.RawMessage, error) {
	out := make(map[string]json.RawMessage, len(specs))
	for _, spec := range specs {
		s, err := SchemaFor(spec.OperationID)
		if err != nil {
			return nil, fmt.Errorf("llm: tool %q: %w", spec.Name, err)
		}
		out[spec.Name] = s
	}
	return out, nil
}
