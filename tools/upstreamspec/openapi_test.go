// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// sampleSpec is a small document with every construct the builder reads: references, a schema
// that contains itself, enumerations, alternatives, arrays, formats and path-level parameters.
const sampleSpec = `
openapi: 3.0.1
paths:
  /status:
    get:
      parameters:
        - {name: verbose, in: query}
        - {name: X-Trace, in: header}
      responses:
        '200':
          content:
            application/json:
              schema:
                type: object
                properties:
                  version: {type: string, description: the version, nullable: true}
                  started: {type: string, format: date-time}
                  uptime: {type: string, format: date-span}
                  instance: {type: string, format: uuid}
                  day: {type: string, format: date}
  /items:
    parameters:
      - {$ref: '#/components/parameters/Page'}
    get:
      parameters:
        - {name: sortKey, in: query}
        - {name: page, in: query}
      responses:
        '404': {description: none}
        '201':
          content:
            text/json:
              schema: {$ref: '#/components/schemas/ItemPage'}
        '200':
          content:
            text/json:
              schema: {$ref: '#/components/schemas/ItemPage'}
    post:
      responses:
        '200':
          content:
            text/plain:
              schema: {type: string}
  /items/{id}:
    get:
      parameters:
        - {name: id, in: path}
      responses:
        '200':
          content:
            application/json:
              schema: {$ref: '#/components/schemas/Item'}
  /tagged:
    get:
      responses:
        '200':
          content:
            application/json:
              schema: {$ref: '#/components/schemas/Tagged'}
  /silent:
    get:
      responses:
        '500': {description: broken}
components:
  parameters:
    Page: {name: page, in: query}
  schemas:
    ItemPage:
      type: object
      properties:
        totalRecords: {type: integer, format: int32}
        records:
          type: array
          items: {$ref: '#/components/schemas/Item'}
    Item:
      x-internal: true
      properties:
        id: {type: number}
        monitored: {type: boolean}
        status: {$ref: '#/components/schemas/Status'}
        parent: {$ref: '#/components/schemas/Item'}
        release:
          anyOf:
            - {type: string, format: date-time}
            - {type: integer}
        owner:
          oneOf:
            - {$ref: '#/components/schemas/Owner'}
        anything: {description: any value}
    Status:
      type: string
      enum: [queued, done]
    Owner:
      type: object
      properties:
        name: {type: string}
    Tagged:
      allOf:
        - {$ref: '#/components/schemas/Owner'}
`

func parse(t *testing.T, spec string) *document {
	t.Helper()
	doc, err := parseDocument([]byte(spec))
	if err != nil {
		t.Fatalf("parseDocument: %v", err)
	}
	return doc
}

func samplePin(operations ...string) pin {
	return pin{Name: "sample", Tag: "v1", Commit: someCommit, Prefix: "/api/v1", Operations: operations}
}

func TestContractOf(t *testing.T) {
	t.Parallel()
	file, err := parse(t, sampleSpec).contractOf(samplePin("GET /api/v1/status", "GET /api/v1/items", "GET /api/v1/items/{id}"))
	if err != nil {
		t.Fatalf("contractOf: %v", err)
	}
	if file.Spec != "sample" || file.Tag != "v1" || file.Commit != someCommit {
		t.Errorf("contract header = %q %q %q", file.Spec, file.Tag, file.Commit)
	}
	item := `{"anything":null,"id":1,"monitored":true,"owner":{"name":"string"},` +
		`"parent":null,"release":"2020-01-02T03:04:05Z","status":"queued"}`
	want := map[string]struct {
		parameters []string
		response   string
	}{
		"GET /api/v1/status": {[]string{"verbose"}, `{"day":"2020-01-02","instance":"00000000-0000-4000-8000-000000000000",` +
			`"started":"2020-01-02T03:04:05Z","uptime":"00:00:00","version":"string"}`},
		// Path-level and operation-level parameters, once each; the response with the lowest 2xx code.
		"GET /api/v1/items":      {[]string{"page", "sortKey"}, `{"records":[` + item + `],"totalRecords":1}`},
		"GET /api/v1/items/{id}": {[]string{}, item},
	}
	if len(file.Operations) != len(want) {
		t.Fatalf("contract has %d operation(s), want %d", len(file.Operations), len(want))
	}
	for key, expected := range want {
		got := file.Operations[key]
		if !slices.Equal(got.Parameters, expected.parameters) {
			t.Errorf("%s: parameters = %q, want %q", key, got.Parameters, expected.parameters)
		}
		if string(got.Response) != expected.response {
			t.Errorf("%s: response = %s, want %s", key, got.Response, expected.response)
		}
	}
	// A contract must encode parameters as a list, never as null: the Rust side reads a sequence.
	encoded, err := json.Marshal(file)
	if err != nil || strings.Contains(string(encoded), `"parameters":null`) {
		t.Errorf("encoded contract has a null parameter list (%v): %s", err, encoded)
	}
}

func TestContractOfRefuses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		operation string
		want      string
	}{
		{"a path the spec lacks", "GET /api/v1/absent", "GET /api/v1/absent: the spec has no such path"},
		{"a path outside the prefix of the spec", "GET /api/v1/api/v1/status", "the spec has no such path"},
		{"a method the path lacks", "POST /api/v1/status", "the spec has no such method on that path"},
		{"an operation without a successful response", "GET /api/v1/silent", "the spec documents no successful response"},
		{"a response that is no JSON", "POST /api/v1/items", "response 200 documents no JSON schema"},
		{"a keyword the builder does not read", "GET /api/v1/tagged", `GET /api/v1/tagged: response: schema keyword "allOf" is not supported`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parse(t, sampleSpec).contractOf(samplePin(test.operation))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("contractOf = %v, want an error with %q", err, test.want)
			}
		})
	}
}

// document wraps one response schema and the named schemas it refers to.
func schemaDocument(response, schemas string) string {
	return fmt.Sprintf(`
paths:
  /x:
    get:
      parameters: [%s]
      responses:
        '200':
          content:
            application/json:
              schema: %s
components:
  parameters:
    Loop: {$ref: '#/components/parameters/Loop'}
  schemas: %s
`, "%s", response, schemas)
}

func TestExampleRefuses(t *testing.T) {
	t.Parallel()
	chain := make([]string, 0, maxHops+1)
	for i := range maxHops + 1 {
		chain = append(chain, fmt.Sprintf("C%d: {$ref: '#/components/schemas/C%d'}", i, i+1))
	}
	tests := []struct {
		name       string
		parameters string
		response   string
		schemas    string
		want       string
	}{
		{"a reference to a schema the document lacks", "", "{$ref: '#/components/schemas/Absent'}", "{}", `response: reference "#/components/schemas/Absent" does not name a schema`},
		{"a reference into another document", "", "{$ref: 'other.yaml#/Item'}", "{}", "does not name a schema"},
		{"an array without items", "", "{type: object, properties: {list: {type: array}}}", "{}", "response.list[]: a schema is missing"},
		{"an unknown type", "", "{type: object, properties: {a: {type: array, items: {type: file}}}}", "{}", `response.a[]: schema type "file" is not supported`},
		{"an unread keyword below a property", "", "{type: object, properties: {a: {not: {type: string}}}}", "{}", `response.a: schema keyword "not" is not supported`},
		{"a chain of references longer than the bound", "", "{$ref: '#/components/schemas/C0'}", "{" + strings.Join(chain, ", ") + "}", "references in a row"},
		{"a parameter reference the document lacks", "{$ref: '#/components/parameters/Absent'}", "{type: string}", "{}", "does not name a parameter"},
		{"a parameter reference to a reference", "{$ref: '#/components/parameters/Loop'}", "{type: string}", "{}", "does not name a parameter"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			spec := fmt.Sprintf(schemaDocument(test.response, test.schemas), test.parameters)
			_, err := parse(t, spec).contractOf(pin{Name: "sample", Operations: []string{"GET /x"}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("contractOf = %v, want an error with %q", err, test.want)
			}
		})
	}
}

func TestExampleBounds(t *testing.T) {
	t.Parallel()
	// Boundary: a chain of exactly maxHops-1 references still resolves.
	chain := make([]string, 0, maxHops-1)
	chain = append(chain, fmt.Sprintf("C%d: {type: boolean}", maxHops-2))
	for i := range maxHops - 2 {
		chain = append(chain, fmt.Sprintf("C%d: {$ref: '#/components/schemas/C%d'}", i, i+1))
	}
	spec := fmt.Sprintf(schemaDocument("{$ref: '#/components/schemas/C0'}", "{"+strings.Join(chain, ", ")+"}"), "")
	file, err := parse(t, spec).contractOf(pin{Name: "sample", Operations: []string{"GET /x"}})
	if err != nil {
		t.Fatalf("contractOf: %v", err)
	}
	if got := string(file.Operations["GET /x"].Response); got != "true" {
		t.Errorf("response = %s, want true", got)
	}
	// An unread keyword on a schema no example reaches does not stop the others.
	if _, err = parse(t, sampleSpec).contractOf(samplePin("GET /api/v1/status")); err != nil {
		t.Errorf("a spec with allOf elsewhere was refused: %v", err)
	}
	// Two schemas that contain each other end with null where the cycle closes.
	cyclic := "{A: {type: object, properties: {b: {$ref: '#/components/schemas/B'}}}, B: {type: object, properties: {a: {$ref: '#/components/schemas/A'}, n: {type: integer}}}}"
	spec = fmt.Sprintf(schemaDocument("{type: array, items: {$ref: '#/components/schemas/A'}}", cyclic), "")
	file, err = parse(t, spec).contractOf(pin{Name: "sample", Operations: []string{"GET /x"}})
	if err != nil {
		t.Fatalf("contractOf: %v", err)
	}
	if got, want := string(file.Operations["GET /x"].Response), `[{"b":{"a":null,"n":1}}]`; got != want {
		t.Errorf("response = %s, want %s", got, want)
	}
}

func TestParseDocumentRefuses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, spec, want string
	}{
		{"a document without paths", "openapi: 3.0.1\n", "the document has no paths"},
		{"text that is no YAML", "paths: [\n", "decode"},
		{"a schema that is no mapping", fmt.Sprintf(schemaDocument("[1, 2]", "{}"), ""), "a schema must be a mapping"},
		{"a keyword of the wrong kind", fmt.Sprintf(schemaDocument("{type: [string]}", "{}"), ""), `schema keyword "type"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := parseDocument([]byte(test.spec)); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parseDocument = %v, want an error with %q", err, test.want)
			}
		})
	}
}
