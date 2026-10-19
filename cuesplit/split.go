// SPDX-FileCopyrightText: 2019-2020 Sigrid Solveig Haflínudóttir
// SPDX-FileCopyrightText: 2026 lusoris
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

type SplitTrack struct {
	Number      int
	Title       string
	Performer   string
	Songwriter  string
	Composer    string
	Artist      string
	Album       string
	Genre       string
	Date        string
	DiscNumber  string
	TotalDiscs  string
	AlbumArtist string
	FirstSample uint64
	EndSample   uint64
	Filename    string
	FinalPath   string
}

type JSONTrack struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	FirstSample uint64 `json:"first_sample"`
	EndSample   uint64 `json:"end_sample"`
	Path        string `json:"path"`
}

type JSONResult struct {
	Input         string      `json:"input"`
	CueSource     string      `json:"cue_source"`
	SampleRate    int         `json:"sample_rate"`
	BitsPerSample int         `json:"bits_per_sample"`
	TotalSamples  uint64      `json:"total_samples"`
	Tracks        []JSONTrack `json:"tracks"`
	PcmMd5In      string      `json:"pcm_md5_in"`
	PcmMd5Out     string      `json:"pcm_md5_out"`
	Verified      bool        `json:"verified"`
}

// Test hook for testing injected failures mid-encode
var testFailAfterTrack = 0

// Mutation hook for testing HTOA mutation proof
var mutateTrack1StartWithIndex01 = false

// Mutation hook for testing skip staging cleanup mutation proof
var mutateSkipStagingCleanup = false

func computeArtist(composer, songwriter, performer string) string {
	switch {
	case composer != "":
		return composer
	case songwriter != "":
		return songwriter
	case performer != "":
		return performer
	default:
		return "Unknown Artist"
	}
}

func BuildTrackPlan(discovered *DiscoveredInput, audioInfo *AudioInfo) ([]*SplitTrack, error) {
	rate := audioInfo.SampleRate
	totalSamples := audioInfo.TotalSamples

	var tracks []*SplitTrack

	if discovered.CueSheet != nil {
		sheet := discovered.CueSheet
		if sheet.File == nil || len(sheet.File.Tracks) == 0 {
			return nil, Refusal("no TRACK", "no tracks found in cue sheet")
		}

		numTracks := len(sheet.File.Tracks)
		trackFmt := "%02d"
		if numTracks > 99 {
			trackFmt = "%03d"
		}

		discNumStr := ""
		if sheet.DiscNumber > 0 {
			discNumStr = strconv.Itoa(sheet.DiscNumber)
		}
		totalDiscsStr := ""
		if sheet.TotalDiscs > 0 {
			totalDiscsStr = strconv.Itoa(sheet.TotalDiscs)
		}

		for i, tr := range sheet.File.Tracks {
			// Check that all index times are within totalSamples
			for _, idx := range tr.Indexes {
				s, err := idx.Time.Samples(rate)
				if err != nil {
					return nil, Refusal("refuse rates not divisible by 75", "%v", err)
				}
				if uint64(s) > totalSamples {
					return nil, Refusal("index beyond audio length", "track %d index %d at sample %d is beyond audio length %d", tr.Number, idx.Number, s, totalSamples)
				}
			}

			st := &SplitTrack{
				Number:      tr.Number,
				Title:       tr.Title,
				Performer:   tr.Performer,
				Songwriter:  tr.Songwriter,
				Composer:    tr.Composer,
				Artist:      computeArtist(tr.Composer, tr.Songwriter, tr.Performer),
				Album:       sheet.Title,
				Genre:       sheet.Genre,
				Date:        sheet.Date,
				DiscNumber:  discNumStr,
				TotalDiscs:  totalDiscsStr,
				AlbumArtist: sheet.Performer,
			}

			// Naming
			paddedNum := fmt.Sprintf(trackFmt, tr.Number)
			if tr.Title != "" {
				st.Filename = fmt.Sprintf("%s - %s.flac", paddedNum, pathReplaceChars(tr.Title))
			} else {
				st.Filename = fmt.Sprintf("%s - Track %s.flac", paddedNum, paddedNum)
			}

			tracks = append(tracks, st)

			// Determine sample ranges
			if i == 0 {
				if mutateTrack1StartWithIndex01 {
					// Deliberately start at INDEX 01 for mutation testing
					idx01 := tr.GetIndex(1)
					s, _ := idx01.Time.Samples(rate)
					st.FirstSample = uint64(s)
				} else {
					// Track 1 starts at sample 0 (HTOA audio stays in track 1)
					st.FirstSample = 0
				}
			}
		}

		// Calculate boundaries between tracks:
		// Track n ends where track n+1 begins: at its INDEX 00 if present, else its INDEX 01
		for i := 1; i < numTracks; i++ {
			nextTr := sheet.File.Tracks[i]
			var boundary uint64
			if nextTr.HasIndex(0) {
				s, _ := nextTr.GetIndex(0).Time.Samples(rate)
				boundary = uint64(s)
			} else {
				idx01 := nextTr.GetIndex(1)
				s, _ := idx01.Time.Samples(rate)
				boundary = uint64(s)
			}
			tracks[i-1].EndSample = boundary
			tracks[i].FirstSample = boundary
		}
		// Last track ends at total sample count
		tracks[numTracks-1].EndSample = totalSamples

	} else if discovered.CueSheetBlock != nil {
		block := discovered.CueSheetBlock
		if len(block.Tracks) == 0 {
			return nil, Refusal("no TRACK", "no tracks found in CUESHEET block")
		}

		numTracks := len(block.Tracks)
		trackFmt := "%02d"
		if numTracks > 99 {
			trackFmt = "%03d"
		}

		artist := ""
		album := ""
		date := ""
		genre := ""
		composer := ""
		albumArtist := ""
		discNumStr := ""
		totalDiscsStr := ""

		if discovered.FlacMeta != nil {
			vc := discovered.FlacMeta.VorbisComments
			artist = vc["ARTIST"]
			album = vc["ALBUM"]
			date = vc["DATE"]
			genre = vc["GENRE"]
			composer = vc["COMPOSER"]
			albumArtist = vc["ALBUMARTIST"]
			if albumArtist == "" {
				albumArtist = vc["ALBUM_ARTIST"]
			}
			discNumStr = vc["DISCNUMBER"]
			totalDiscsStr = vc["TOTALDISCS"]
		}

		for i, tr := range block.Tracks {
			if tr.Type != 0 {
				return nil, Refusal("non-AUDIO track", "track %d in CUESHEET block is non-AUDIO", tr.Number)
			}
			if i > 0 && tr.Number <= block.Tracks[i-1].Number {
				return nil, Refusal("track numbers not strictly increasing", "track %d is not strictly greater than previous track", tr.Number)
			}

			// Validate has INDEX 01
			hasIdx1 := false
			var idx1Offset uint64
			var hasIdx0 bool
			var idx0Offset uint64

			for _, idx := range tr.Indexes {
				absSample := tr.Offset + idx.Offset
				if absSample > totalSamples {
					return nil, Refusal("index beyond audio length", "track %d index %d at sample %d exceeds audio length %d", tr.Number, idx.Number, absSample, totalSamples)
				}
				if idx.Number == 1 {
					hasIdx1 = true
					idx1Offset = absSample
				} else if idx.Number == 0 {
					hasIdx0 = true
					idx0Offset = absSample
				}
			}
			if !hasIdx1 {
				return nil, Refusal("a track without INDEX 01", "track %d in CUESHEET block has no INDEX 01", tr.Number)
			}

			st := &SplitTrack{
				Number:      tr.Number,
				Title:       "", // Titles are empty for block form
				Performer:   artist,
				Composer:    composer,
				Artist:      computeArtist(composer, "", artist),
				Album:       album,
				Genre:       genre,
				Date:        date,
				DiscNumber:  discNumStr,
				TotalDiscs:  totalDiscsStr,
				AlbumArtist: albumArtist,
			}

			paddedNum := fmt.Sprintf(trackFmt, tr.Number)
			st.Filename = fmt.Sprintf("%s - Track %s.flac", paddedNum, paddedNum)
			tracks = append(tracks, st)

			if i == 0 {
				if mutateTrack1StartWithIndex01 {
					st.FirstSample = idx1Offset
				} else {
					st.FirstSample = 0
				}
			}

			if i > 0 {
				var boundary uint64
				if hasIdx0 {
					boundary = idx0Offset
				} else {
					boundary = idx1Offset
				}
				tracks[i-1].EndSample = boundary
				tracks[i].FirstSample = boundary
			}
		}

		tracks[numTracks-1].EndSample = totalSamples
	} else {
		return nil, Refusal("no cue sheet", "no cuesheet found")
	}

	// Assert exact sample coverage [0, totalSamples)
	if len(tracks) == 0 {
		return nil, Refusal("no TRACK", "no tracks to split")
	}
	if tracks[0].FirstSample != 0 {
		return nil, fmt.Errorf("coverage violation: track 1 does not start at sample 0 (starts at %d)", tracks[0].FirstSample)
	}
	var totalTrackSamples uint64
	for i, tr := range tracks {
		if tr.EndSample <= tr.FirstSample {
			return nil, fmt.Errorf("coverage violation: track %d has empty or negative sample range [%d, %d)", tr.Number, tr.FirstSample, tr.EndSample)
		}
		if i < len(tracks)-1 {
			if tr.EndSample != tracks[i+1].FirstSample {
				return nil, fmt.Errorf("coverage violation: gap or overlap between track %d (ends %d) and track %d (starts %d)", tr.Number, tr.EndSample, tracks[i+1].Number, tracks[i+1].FirstSample)
			}
		}
		totalTrackSamples += (tr.EndSample - tr.FirstSample)
	}
	if tracks[len(tracks)-1].EndSample != totalSamples {
		return nil, fmt.Errorf("coverage violation: last track ends at %d instead of total samples %d", tracks[len(tracks)-1].EndSample, totalSamples)
	}
	if totalTrackSamples != totalSamples {
		return nil, fmt.Errorf("coverage violation: sum of track samples %d != total audio samples %d", totalTrackSamples, totalSamples)
	}

	return tracks, nil
}

func Split(ctx context.Context, discovered *DiscoveredInput, audioInfo *AudioInfo, outDir string, jsonOutput bool, stdout, stderr io.Writer) (*JSONResult, error) {
	// Build track plan and assert coverage
	tracks, err := BuildTrackPlan(discovered, audioInfo)
	if err != nil {
		return nil, err
	}

	// Target directory
	targetAlbumDir := filepath.Join(outDir, discovered.AlbumDirName)

	// Pre-flight check: refuse before encoding if target exists (exit 2, "target exists")
	if _, err := os.Stat(targetAlbumDir); err == nil {
		return nil, Refusal("target exists", "destination album directory %s already exists", targetAlbumDir)
	}

	// Ensure outDir exists
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory %s: %w", outDir, err)
	}

	// Create staging directory: <out>/.cuesplit-staging-<random>
	stagingDir, err := os.MkdirTemp(outDir, ".cuesplit-staging-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create staging directory in %s: %w", outDir, err)
	}

	success := false
	defer func() {
		if !success && !mutateSkipStagingCleanup {
			_ = os.RemoveAll(stagingDir)
		}
	}()

	// Determine sample format arguments based on input bits per sample
	var sampleFmtArgs []string
	switch audioInfo.BitsPerSample {
	case 16:
		sampleFmtArgs = []string{"-sample_fmt", "s16"}
	case 24:
		sampleFmtArgs = []string{"-sample_fmt", "s32", "-bits_per_raw_sample", "24"}
	default:
		return nil, Refusal("unsupported bit depth", "unsupported bit depth: %d", audioInfo.BitsPerSample)
	}

	// Sequential encoding
	var stagingPaths []string
	totalTracksCount := len(tracks)

	// Check environment variable hook for failure injection
	if envVal := os.Getenv("CUESPLIT_TEST_FAIL_AFTER_TRACK"); envVal != "" {
		if n, err := strconv.Atoi(envVal); err == nil {
			testFailAfterTrack = n
		}
	}

	for i, tr := range tracks {
		// Failure injection check
		if testFailAfterTrack > 0 && i >= testFailAfterTrack {
			return nil, fmt.Errorf("injected failure mid-encode after track %d", i)
		}

		stagingTrackPath := filepath.Join(stagingDir, tr.Filename)
		tr.FinalPath = filepath.Join(targetAlbumDir, tr.Filename)
		stagingPaths = append(stagingPaths, stagingTrackPath)

		atrim := fmt.Sprintf("atrim=start_sample=%d:end_sample=%d,asetpts=PTS-STARTPTS", tr.FirstSample, tr.EndSample)

		args := []string{
			"-nostdin",
			"-loglevel", "error",
			"-i", discovered.AudioPath,
			"-map", "0:a:0",
			"-map_metadata", "-1",
			"-af", atrim,
			"-c:a", "flac",
		}
		args = append(args, sampleFmtArgs...)

		// Add metadata tags
		addTag := func(k, v string) {
			if v != "" {
				args = append(args, "-metadata", fmt.Sprintf("%s=%s", k, v))
			}
		}

		addTag("composer", tr.Composer)
		addTag("artist", tr.Artist)
		addTag("performer", tr.Performer)
		addTag("album", tr.Album)
		addTag("title", tr.Title)
		addTag("genre", tr.Genre)
		addTag("date", tr.Date)
		addTag("tracknumber", strconv.Itoa(tr.Number))
		addTag("tracktotal", strconv.Itoa(totalTracksCount))
		addTag("discnumber", tr.DiscNumber)
		addTag("totaldiscs", tr.TotalDiscs)
		addTag("albumartist", tr.AlbumArtist)

		args = append(args, stagingTrackPath)

		cmd := exec.CommandContext(ctx, "ffmpeg", args...)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("ffmpeg failed extracting track %d: %w", tr.Number, err)
		}
	}

	// Verification
	// 1. flac -t --silent for every output track
	for _, p := range stagingPaths {
		cmd := exec.CommandContext(ctx, "flac", "-t", "--silent", p)
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("flac integrity verification failed on %s: %w", p, err)
		}
	}

	// 2. ffprobe checks: bits_per_raw_sample, sample_rate, sample count sum
	var outputTotalSamples uint64
	for i, p := range stagingPaths {
		info, err := ProbeAudio(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("failed to probe output file %s: %w", p, err)
		}
		if info.SampleRate != audioInfo.SampleRate {
			return nil, fmt.Errorf("track %d sample rate mismatch: got %d, expected %d", tracks[i].Number, info.SampleRate, audioInfo.SampleRate)
		}
		if info.BitsPerSample != audioInfo.BitsPerSample {
			return nil, fmt.Errorf("track %d bit depth mismatch: got %d, expected %d", tracks[i].Number, info.BitsPerSample, audioInfo.BitsPerSample)
		}
		outputTotalSamples += info.TotalSamples
	}

	if outputTotalSamples != audioInfo.TotalSamples {
		return nil, fmt.Errorf("total output sample count mismatch: sum of outputs %d != input %d", outputTotalSamples, audioInfo.TotalSamples)
	}

	// 3. Bit-exact PCM MD5 check: input PCM MD5 vs concatenated output PCM MD5
	pcmMd5In, err := ComputePCM_MD5(ctx, discovered.AudioPath)
	if err != nil {
		return nil, fmt.Errorf("failed to compute input PCM MD5: %w", err)
	}

	pcmMd5Out, err := ComputeConcatenatedPCM_MD5(ctx, stagingPaths)
	if err != nil {
		return nil, fmt.Errorf("failed to compute concatenated output PCM MD5: %w", err)
	}

	if pcmMd5In != pcmMd5Out {
		return nil, fmt.Errorf("PCM MD5 mismatch: input %s != concatenated outputs %s", pcmMd5In, pcmMd5Out)
	}

	// All verifications passed: atomically rename staging directory to final target directory
	if err := os.Rename(stagingDir, targetAlbumDir); err != nil {
		return nil, fmt.Errorf("failed to rename staging directory %s to %s: %w", stagingDir, targetAlbumDir, err)
	}
	success = true

	// Build result
	var jsonTracks []JSONTrack
	for _, tr := range tracks {
		jsonTracks = append(jsonTracks, JSONTrack{
			Number:      tr.Number,
			Title:       tr.Title,
			FirstSample: tr.FirstSample,
			EndSample:   tr.EndSample,
			Path:        tr.FinalPath,
		})
	}

	res := &JSONResult{
		Input:         discovered.InputPath,
		CueSource:     string(discovered.CueSource),
		SampleRate:    audioInfo.SampleRate,
		BitsPerSample: audioInfo.BitsPerSample,
		TotalSamples:  audioInfo.TotalSamples,
		Tracks:        jsonTracks,
		PcmMd5In:      pcmMd5In,
		PcmMd5Out:     pcmMd5Out,
		Verified:      true,
	}

	if jsonOutput {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return nil, fmt.Errorf("failed to encode JSON result: %w", err)
		}
	} else {
		for _, tr := range tracks {
			fmt.Fprintf(stdout, "%s\n", tr.FinalPath)
		}
	}

	return res, nil
}
