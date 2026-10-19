// SPDX-FileCopyrightText: 2026 lusoris
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cauda.dev/cuesplit/cuesheet"
	"cauda.dev/cuesplit/flacmeta"
)

type CueSourceType string

const (
	CueSourceFile               CueSourceType = "file"
	CueSourceFlacVorbisCueSheet CueSourceType = "flac-vorbis-cuesheet"
	CueSourceFlacCueSheetBlock  CueSourceType = "flac-cuesheet-block"
)

type DiscoveredInput struct {
	InputPath     string
	AlbumDirName  string
	AudioPath     string
	CueSource     CueSourceType
	CueSheet      *cuesheet.Sheet
	CueSheetBlock *flacmeta.CueSheetBlock
	FlacMeta      *flacmeta.Metadata
}

func Discover(inputPath string) (*DiscoveredInput, error) {
	fi, err := os.Stat(inputPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, Refusal("missing input", "input %s does not exist", inputPath)
		}
		return nil, fmt.Errorf("failed to stat input %s: %w", inputPath, err)
	}

	absInput, err := filepath.Abs(inputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path of %s: %w", inputPath, err)
	}

	if fi.IsDir() {
		return discoverDir(inputPath, absInput)
	}

	return discoverFile(inputPath, absInput)
}

func discoverDir(inputPath, absInput string) (*DiscoveredInput, error) {
	entries, err := os.ReadDir(inputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", inputPath, err)
	}

	validAudioExts := map[string]bool{
		".flac": true,
		".wav":  true,
		".ape":  true,
		".wv":   true,
	}

	var audioFiles []string
	var cueFiles []string

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if validAudioExts[ext] {
			audioFiles = append(audioFiles, entry.Name())
		} else if ext == ".cue" {
			cueFiles = append(cueFiles, entry.Name())
		}
	}

	// Refuse if already split (more than one audio file)
	if len(audioFiles) > 1 {
		return nil, Refusal("already split", "directory contains %d audio files (%s)", len(audioFiles), strings.Join(audioFiles, ", "))
	}

	// Refuse if several cue sheets
	if len(cueFiles) > 1 {
		return nil, Refusal("several cue sheets", "directory contains %d cue sheet files", len(cueFiles))
	}

	albumDirName := filepath.Base(absInput)

	if len(cueFiles) == 1 {
		cueFileName := cueFiles[0]
		cueFilePath := filepath.Join(inputPath, cueFileName)

		// Check if any FLAC file also carries an embedded cuesheet
		for _, a := range audioFiles {
			if strings.ToLower(filepath.Ext(a)) == ".flac" {
				flacPath := filepath.Join(inputPath, a)
				meta, err := flacmeta.ReadFile(flacPath)
				if err == nil && (meta.VorbisCueSheet != "" || meta.CueSheetBlock != nil) {
					return nil, Refusal("several cue sheets", "found both a .cue file (%s) and an embedded cuesheet in %s", cueFileName, a)
				}
			}
		}

		cueRaw, err := os.ReadFile(cueFilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read cuesheet %s: %w", cueFilePath, err)
		}
		sheet, err := cuesheet.Parse(cueRaw)
		if err != nil {
			return nil, err
		}

		audioPath := filepath.Join(inputPath, sheet.File.Name)
		if _, err := os.Stat(audioPath); err != nil && os.IsNotExist(err) {
			if fallbackPath, ok := findAudioFile(inputPath, sheet.File.Name); ok {
				audioPath = fallbackPath
			} else {
				return nil, Refusal("missing audio", "audio file %q referenced by %s was not found", sheet.File.Name, cueFileName)
			}
		}

		return &DiscoveredInput{
			InputPath:    inputPath,
			AlbumDirName: albumDirName,
			AudioPath:    audioPath,
			CueSource:    CueSourceFile,
			CueSheet:     sheet,
		}, nil
	}

	// No .cue files: look for a .flac file with embedded cuesheet
	var embeddedFlac string
	var embeddedMeta *flacmeta.Metadata
	embeddedCount := 0

	for _, a := range audioFiles {
		if strings.ToLower(filepath.Ext(a)) == ".flac" {
			flacPath := filepath.Join(inputPath, a)
			meta, err := flacmeta.ReadFile(flacPath)
			if err == nil && (meta.VorbisCueSheet != "" || meta.CueSheetBlock != nil) {
				embeddedCount++
				embeddedFlac = flacPath
				embeddedMeta = meta
			}
		}
	}

	if embeddedCount > 1 {
		return nil, Refusal("several cue sheets", "found %d FLAC files with embedded cue sheets", embeddedCount)
	}
	if embeddedCount == 0 {
		return nil, Refusal("no cue sheet", "no cue sheet or FLAC with embedded cuesheet found in %s", inputPath)
	}

	return processEmbeddedFlac(inputPath, albumDirName, embeddedFlac, embeddedMeta)
}

func discoverFile(inputPath, absInput string) (*DiscoveredInput, error) {
	ext := strings.ToLower(filepath.Ext(inputPath))
	dir := filepath.Dir(absInput)
	albumDirName := filepath.Base(dir)

	switch ext {
	case ".cue":
		cueRaw, err := os.ReadFile(inputPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read cuesheet %s: %w", inputPath, err)
		}
		sheet, err := cuesheet.Parse(cueRaw)
		if err != nil {
			return nil, err
		}

		audioPath := filepath.Join(dir, sheet.File.Name)
		if _, err := os.Stat(audioPath); err != nil && os.IsNotExist(err) {
			if fallbackPath, ok := findAudioFile(dir, sheet.File.Name); ok {
				audioPath = fallbackPath
			} else {
				return nil, Refusal("missing audio", "audio file %q referenced by %s was not found", sheet.File.Name, inputPath)
			}
		}

		return &DiscoveredInput{
			InputPath:    inputPath,
			AlbumDirName: albumDirName,
			AudioPath:    audioPath,
			CueSource:    CueSourceFile,
			CueSheet:     sheet,
		}, nil

	case ".flac":
		meta, err := flacmeta.ReadFile(inputPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read FLAC metadata from %s: %w", inputPath, err)
		}
		if meta.VorbisCueSheet == "" && meta.CueSheetBlock == nil {
			return nil, Refusal("no embedded cue sheet", "FLAC file %s contains no embedded cuesheet", inputPath)
		}
		return processEmbeddedFlac(inputPath, albumDirName, inputPath, meta)

	default:
		return nil, Refusal("unsupported input", "unsupported input file format %q (expected .cue or .flac)", inputPath)
	}
}

func processEmbeddedFlac(inputPath, albumDirName, flacPath string, meta *flacmeta.Metadata) (*DiscoveredInput, error) {
	if meta.VorbisCueSheet != "" {
		sheet, err := cuesheet.Parse([]byte(meta.VorbisCueSheet))
		if err != nil {
			return nil, fmt.Errorf("failed to parse embedded VORBIS_COMMENT cuesheet: %w", err)
		}
		return &DiscoveredInput{
			InputPath:    inputPath,
			AlbumDirName: albumDirName,
			AudioPath:    flacPath,
			CueSource:    CueSourceFlacVorbisCueSheet,
			CueSheet:     sheet,
			FlacMeta:     meta,
		}, nil
	}

	if meta.CueSheetBlock != nil {
		return &DiscoveredInput{
			InputPath:     inputPath,
			AlbumDirName:  albumDirName,
			AudioPath:     flacPath,
			CueSource:     CueSourceFlacCueSheetBlock,
			CueSheetBlock: meta.CueSheetBlock,
			FlacMeta:      meta,
		}, nil
	}

	return nil, Refusal("no embedded cue sheet", "no embedded cuesheet found in %s", flacPath)
}
