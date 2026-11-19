// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue

import (
	"context"
	"slices"
	"strings"
	"time"
)

const (
	// maxOutputChecks bounds distinct parent listings during one queue sweep.
	maxOutputChecks   = 20
	outputCheckBudget = 60 * time.Second
)

const (
	outputListed     = "listed"
	outputNotVisible = "not_visible"
	outputError      = "error"
	outputNoPath     = "no_path"
	outputOverBudget = "over_budget"
)

var outputResultNames = []string{
	outputListed,
	outputNotVisible,
	outputError,
	outputNoPath,
	outputOverBudget,
}

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

// noFilesUnlisted keeps inconclusive no-files items out of import_blocked.
func noFilesUnlisted(item Item) bool {
	lines := importLines(item)
	return hasNoFilesFound(lines) && !checkUnpackOrLocked(lines) && !item.OutputListed
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

func checkOutputs(ctx context.Context, arr ArrClient, inst Instance, s Settings, items []Item, now time.Time, start int) ([]Item, []outputResult, int) {
	ctx, cancel := context.WithTimeout(ctx, outputCheckBudget)
	defer cancel()
	out := append([]Item(nil), items...)
	parents, parentsByItem, resultsByItem := collectOutputParents(out, s, now)
	if len(parents) == 0 {
		return out, orderedOutputResults(resultsByItem), start
	}
	offset, window, next := outputWindow(len(parents), start)
	listings := listOutputWindow(ctx, arr, inst, parents, offset, window)
	applyOutputResults(out, parentsByItem, listings, resultsByItem)
	return out, orderedOutputResults(resultsByItem), next
}

func collectOutputParents(out []Item, s Settings, now time.Time) ([]string, map[int]string, map[int]outputResult) {
	parentsByItem := make(map[int]string)
	indicesByParent := make(map[string][]int)
	resultsByItem := make(map[int]outputResult)
	for i := range out {
		if !needsOutputCheck(out[i], s, now) {
			continue
		}
		resultsByItem[i] = outputResult{result: outputNoPath, downloadID: out[i].DownloadID, outputPath: out[i].OutputPath}
		parent, _, ok := splitOutputPath(out[i].OutputPath)
		if !ok {
			continue
		}
		parentsByItem[i] = parent
		indicesByParent[parent] = append(indicesByParent[parent], i)
	}
	parents := make([]string, 0, len(indicesByParent))
	for parent := range indicesByParent {
		parents = append(parents, parent)
	}
	slices.Sort(parents)
	return parents, parentsByItem, resultsByItem
}

func outputWindow(parentCount, start int) (int, int, int) {
	offset := start % parentCount
	if offset < 0 {
		offset += parentCount
	}
	window := min(maxOutputChecks, parentCount-offset)
	return offset, window, (offset + window) % parentCount
}

func listOutputWindow(ctx context.Context, arr ArrClient, inst Instance, parents []string, offset, window int) map[string]folderListing {
	listings := make(map[string]folderListing, window)
	for i := range window {
		parent := parents[offset+i]
		listing := folderListing{}
		if err := ctx.Err(); err != nil {
			listing.err = err
		} else {
			listing.names, listing.err = arr.ListFolder(ctx, inst.Kind, inst.BaseURL, inst.APIKey, parent)
		}
		listings[parent] = listing
	}
	return listings
}

func applyOutputResults(out []Item, parentsByItem map[int]string, listings map[string]folderListing, resultsByItem map[int]outputResult) {
	for i, parent := range parentsByItem {
		result := outputResult{downloadID: out[i].DownloadID, outputPath: out[i].OutputPath, dir: parent}
		listing, selected := listings[parent]
		switch {
		case !selected:
			result.result = outputOverBudget
		case listing.err != nil:
			result.result, result.err = outputError, listing.err
		default:
			_, name, _ := splitOutputPath(out[i].OutputPath)
			out[i].OutputListed = containsOutputName(listing.names, name)
			result.result = outputNotVisible
			if out[i].OutputListed {
				result.result = outputListed
			}
		}
		resultsByItem[i] = result
	}
}

type folderListing struct {
	names []string
	err   error
}

func orderedOutputResults(byItem map[int]outputResult) []outputResult {
	indices := make([]int, 0, len(byItem))
	for i := range byItem {
		indices = append(indices, i)
	}
	slices.Sort(indices)
	results := make([]outputResult, 0, len(indices))
	for _, i := range indices {
		results = append(results, byItem[i])
	}
	return results
}

func containsOutputName(names []string, name string) bool {
	// Exact comparison fails closed; case-folding could match a sibling on case-sensitive filesystems.
	return slices.Contains(names, name)
}
