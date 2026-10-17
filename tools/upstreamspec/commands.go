// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
)

var (
	// classPattern matches the declaration of a command: a class named <Name>Command that derives
	// from Command.
	classPattern = regexp.MustCompile(`(?m)^[ \t]*public[ \t]+(?:sealed[ \t]+)?class[ \t]+(\w+)Command[ \t]*:[ \t]*Command\b`)
	// propertyPattern matches a public auto property with a public getter and setter. A property
	// that is computed (=>), override, static or get-only is not part of the body the server reads.
	propertyPattern = regexp.MustCompile(`(?m)^[ \t]*public[ \t]+([\w<>\[\],.? ]+?)[ \t]+(\w+)[ \t]*\{[ \t]*get;[ \t]*set;[ \t]*\}`)
	// skippedModifiers keep a declaration out of the body.
	skippedModifiers = []string{"override", "static", "virtual", "new"}
)

// parseCommand returns the name of the command a C# class file declares (the class name without
// "Command", which is how upstream names it: Command.Name) and the JSON names of the properties
// the server reads from a body. It refuses a file with no class, several classes or no property,
// so an extractor that matches nothing cannot make every body pass.
func parseCommand(source []byte) (string, []string, error) {
	classes := classPattern.FindAllSubmatch(source, -1)
	if len(classes) != 1 {
		return "", nil, fmt.Errorf("expected one class <Name>Command : Command, found %d", len(classes))
	}
	var properties []string
	for _, match := range propertyPattern.FindAllSubmatchIndex(source, -1) {
		typ, name := string(source[match[2]:match[3]]), string(source[match[4]:match[5]])
		first, _, _ := strings.Cut(typ, " ")
		if slices.Contains(skippedModifiers, first) || jsonIgnored(source[:match[0]]) {
			continue
		}
		properties = append(properties, camelCase(name))
	}
	if len(properties) == 0 {
		return "", nil, errors.New("the class declares no public get/set property, so a body would be checked against nothing")
	}
	sort.Strings(properties)
	return string(classes[0][1]), slices.Compact(properties), nil
}

// jsonIgnored reports whether the last line before a declaration carries [JsonIgnore], which
// keeps the property out of the body.
func jsonIgnored(before []byte) bool {
	text := strings.TrimRight(string(before), " \t\r\n")
	return strings.Contains(text[strings.LastIndex(text, "\n")+1:], "JsonIgnore")
}

// camelCase writes a C# property name the way System.Text.Json's JsonNamingPolicy.CamelCase does:
// the leading run of upper-case letters becomes lower case, except the last one of the run when a
// lower-case letter follows ("EpisodeIds" is "episodeIds", "URLPath" is "urlPath").
func camelCase(name string) string {
	runes := []rune(name)
	for i := range runes {
		if i == 1 && !unicode.IsUpper(runes[i]) {
			break
		}
		if i > 0 && i+1 < len(runes) && !unicode.IsUpper(runes[i+1]) {
			break
		}
		runes[i] = unicode.ToLower(runes[i])
	}
	return string(runes)
}
