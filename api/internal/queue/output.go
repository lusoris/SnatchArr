// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue

import (
	"context"
	"strings"
	"time"
)

// maxOutputChecks bounds distinct parent listings during one queue sweep.
const maxOutputChecks = 20

const (
	outputListed     = "listed"
	outputNotVisible = "not_visible"
	outputError      = "error"
	outputNoPath     = "no_path"
	outputOverBudget = "over_budget"
)

type outputResult struct {
	result     string
	downloadID string
	outputPath string
	dir        string
	err        error
}

func needsOutputCheck(item Item, s Settings, now time.Time) bool {
	lines := importLines(item)
	return hasNoFilesFound(lines) && !checkUnpackOrLocked(lines) && isPastGrace(item.Added, s.Grace, now)
}

func hasNoFilesFound(lines []string) bool {
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if reNoFilesFound.MatchString(trimmed) || strings.EqualFold(trimmed, "No files found are eligible for import in") {
			return true
		}
	}
	return false
}

func splitOutputPath(path string) (string, string, bool) {
	trimmed := strings.TrimRight(path, "/\\")
	if trimmed == "" {
		return "", "", false
	}
	idx := strings.LastIndexAny(trimmed, "/\\")
	if idx < 0 || idx == len(trimmed)-1 {
		return "", "", false
	}
	return trimmed[:idx+1], trimmed[idx+1:], true
}

func checkOutputs(ctx context.Context, arr ArrClient, inst Instance, s Settings, items []Item, now time.Time) ([]Item, []outputResult) {
	out := append([]Item(nil), items...)
	cache := make(map[string]folderListing)
	results := make([]outputResult, 0)
	for i := range out {
		if !needsOutputCheck(out[i], s, now) {
			continue
		}
		result := checkOutput(ctx, arr, inst, &out[i], cache)
		results = append(results, result)
	}
	return out, results
}

type folderListing struct {
	names []string
	err   error
}

func checkOutput(ctx context.Context, arr ArrClient, inst Instance, item *Item, cache map[string]folderListing) outputResult {
	item.OutputListed = false
	item.outputChecked = false
	parent, name, ok := splitOutputPath(item.OutputPath)
	result := outputResult{downloadID: item.DownloadID, outputPath: item.OutputPath, dir: parent}
	if !ok {
		result.result = outputNoPath
		item.outputChecked = true
		return result
	}
	listing, known := cache[parent]
	if !known && len(cache) >= maxOutputChecks {
		result.result = outputOverBudget
		item.outputChecked = true
		return result
	}
	if !known {
		listing.names, listing.err = arr.ListFolder(ctx, inst.Kind, inst.BaseURL, inst.APIKey, parent)
		cache[parent] = listing
	}
	if listing.err != nil {
		result.result, result.err = outputError, listing.err
		item.outputChecked = true
		return result
	}
	item.OutputListed = containsOutputName(listing.names, name)
	result.result = outputNotVisible
	if item.OutputListed {
		result.result = outputListed
		return result
	}
	item.outputChecked = true
	return result
}

func containsOutputName(names []string, name string) bool {
	for _, candidate := range names {
		if candidate == name {
			return true
		}
	}
	return false
}
