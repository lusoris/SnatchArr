// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
)

// maxRootDepth bounds the upward search for go.mod (HISS-02).
const maxRootDepth = 64

// findRepoRoot locates the root of the repository containing startDir by searching upwards for go.mod.
func findRepoRoot(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("abs %s: %w", startDir, err)
	}
	for range maxRootDepth {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("repository root (go.mod) not found within %d levels of %s", maxRootDepth, startDir)
}

// LoadRegisteredModuleIDs extracts the list of registered module IDs from the modules package
// by parsing api/internal/modules/descriptor.go and api/internal/domain/module.go without
// duplicating the registered IDs or violating Go internal package import boundaries.
func LoadRegisteredModuleIDs(repoRoot string) ([]string, error) {
	domainPath := filepath.Join(repoRoot, "api", "internal", "domain", "module.go")
	descPath := filepath.Join(repoRoot, "api", "internal", "modules", "descriptor.go")
	consts, err := parseDomainConstants(domainPath)
	if err != nil {
		return nil, fmt.Errorf("domain constants: %w", err)
	}
	return parseDescriptorIDs(descPath, consts)
}

func parseDomainConstants(path string) (map[string]string, error) {
	cleanPath := filepath.Clean(path)
	fset := token.NewFileSet()
	// #nosec G304 G703 -- path within repository
	node, err := parser.ParseFile(fset, cleanPath, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", cleanPath, err)
	}
	consts := make(map[string]string)
	for _, decl := range node.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		collectConsts(gen, consts)
	}
	return consts, nil
}

func collectConsts(gen *ast.GenDecl, dst map[string]string) {
	for _, spec := range gen.Specs {
		if vspec, ok := spec.(*ast.ValueSpec); ok {
			collectValueSpec(vspec, dst)
		}
	}
}

func collectValueSpec(vspec *ast.ValueSpec, dst map[string]string) {
	for i, name := range vspec.Names {
		if i < len(vspec.Values) {
			if s := extractStringLit(vspec.Values[i]); s != "" {
				dst[name.Name] = s
			}
		}
	}
}

func extractStringLit(expr ast.Expr) string {
	bl, ok := expr.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return ""
	}
	unquoted, err := strconv.Unquote(bl.Value)
	if err != nil {
		return ""
	}
	return unquoted
}

func parseDescriptorIDs(path string, consts map[string]string) ([]string, error) {
	cleanPath := filepath.Clean(path)
	fset := token.NewFileSet()
	// #nosec G304 G703 -- path within repository
	node, err := parser.ParseFile(fset, cleanPath, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", cleanPath, err)
	}
	var moduleIDs []string
	ast.Inspect(node, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "DefaultDescriptors" {
			return true
		}
		ast.Inspect(fn.Body, func(inner ast.Node) bool {
			if id := extractDescriptorID(inner, consts); id != "" {
				moduleIDs = append(moduleIDs, id)
			}
			return true
		})
		return false
	})
	if len(moduleIDs) == 0 {
		return nil, fmt.Errorf("no module IDs found in %s", cleanPath)
	}
	return moduleIDs, nil
}

func extractDescriptorID(n ast.Node, consts map[string]string) string {
	kv, ok := n.(*ast.KeyValueExpr)
	if !ok {
		return ""
	}
	keyIdent, ok := kv.Key.(*ast.Ident)
	if !ok || keyIdent.Name != "ID" {
		return ""
	}
	return resolveExprValue(kv.Value, consts)
}

func resolveExprValue(expr ast.Expr, consts map[string]string) string {
	switch val := expr.(type) {
	case *ast.BasicLit:
		if val.Kind == token.STRING {
			s, err := strconv.Unquote(val.Value)
			if err == nil {
				return s
			}
		}
	case *ast.SelectorExpr:
		if ident, ok := val.X.(*ast.Ident); ok && ident.Name == "domain" {
			return consts[val.Sel.Name]
		}
	case *ast.Ident:
		return consts[val.Name]
	}
	return ""
}
