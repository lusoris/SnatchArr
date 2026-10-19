// SPDX-FileCopyrightText: 2026 lusoris
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireTools(t *testing.T, tools ...string) {
	t.Helper()
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("required tool %s not on PATH", tool)
		}
	}
}

// synthesizeFLAC creates a multi-section FLAC file where each section has a distinct sine wave frequency.
func synthesizeFLAC(t *testing.T, path string, rate, bits int, durations []int, freqs []int) {
	t.Helper()
	if len(durations) != len(freqs) {
		t.Fatalf("durations and freqs length mismatch")
	}

	var inputs []string
	var filterInputs []string

	for i := range durations {
		inputs = append(inputs, "-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=%d:sample_rate=%d:duration=%d", freqs[i], rate, durations[i]))
		filterInputs = append(filterInputs, fmt.Sprintf("[%d:a]", i))
	}

	filterComplex := fmt.Sprintf("%sconcat=n=%d:v=0:a=1[out]", strings.Join(filterInputs, ""), len(durations))

	args := []string{"-nostdin", "-loglevel", "error"}
	args = append(args, inputs...)
	args = append(args, "-filter_complex", filterComplex, "-map", "[out]", "-c:a", "flac")

	if bits == 24 {
		args = append(args, "-sample_fmt", "s32", "-bits_per_raw_sample", "24")
	} else {
		args = append(args, "-sample_fmt", "s16")
	}
	args = append(args, path, "-y")

	cmd := exec.Command("ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg failed to synthesize FLAC %s: %v (%s)", path, err, string(out))
	}
}

// synthesizeWAV creates a multi-section WAV file.
func synthesizeWAV(t *testing.T, path string, rate int, durations []int, freqs []int) {
	t.Helper()
	var inputs []string
	var filterInputs []string

	for i := range durations {
		inputs = append(inputs, "-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=%d:sample_rate=%d:duration=%d", freqs[i], rate, durations[i]))
		filterInputs = append(filterInputs, fmt.Sprintf("[%d:a]", i))
	}

	filterComplex := fmt.Sprintf("%sconcat=n=%d:v=0:a=1[out]", strings.Join(filterInputs, ""), len(durations))

	args := []string{"-nostdin", "-loglevel", "error"}
	args = append(args, inputs...)
	args = append(args, "-filter_complex", filterComplex, "-map", "[out]", path, "-y")

	cmd := exec.Command("ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg failed to synthesize WAV %s: %v (%s)", path, err, string(out))
	}
}

func readTagsWithFFprobe(t *testing.T, path string) map[string]string {
	t.Helper()
	cmd := exec.Command("ffprobe", "-v", "error", "-show_entries", "format_tags", "-of", "json", path)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("ffprobe failed on %s: %v", path, err)
	}

	var data struct {
		Format struct {
			Tags map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &data); err != nil {
		t.Fatalf("failed to parse ffprobe json: %v", err)
	}

	normalized := make(map[string]string)
	for k, v := range data.Format.Tags {
		normalized[strings.ToLower(k)] = v
	}
	return normalized
}

func TestIntegration_44100_16bit_FLAC_HTOA(t *testing.T) {
	requireTools(t, "ffmpeg", "ffprobe", "flac")

	tmpDir := t.TempDir()
	inDir := filepath.Join(tmpDir, "album_in")
	outDir := filepath.Join(tmpDir, "album_out")
	if err := os.MkdirAll(inDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 44100 Hz 16-bit FLAC image, 3 tracks:
	// Total audio: 9 seconds:
	// Section 1: 2s (300 Hz) - HTOA for track 1
	// Section 2: 2s (500 Hz) - Track 1 music
	// Section 3: 3s (700 Hz) - Track 2 (includes 1s pregap between 00:04:00 and 00:05:00)
	// Section 4: 2s (900 Hz) - Track 3
	imagePath := filepath.Join(inDir, "image.flac")
	synthesizeFLAC(t, imagePath, 44100, 16, []int{2, 2, 3, 2}, []int{300, 500, 700, 900})

	cuePath := filepath.Join(inDir, "image.cue")
	cueContent := `REM DATE 2026
REM GENRE "Electronic"
PERFORMER "Lusoris Artist"
TITLE "Album HTOA Test"
FILE "image.flac" FLAC
  TRACK 01 AUDIO
    TITLE "First Track with HTOA"
    INDEX 01 00:02:00
  TRACK 02 AUDIO
    TITLE "Second Track"
    INDEX 00 00:04:00
    INDEX 01 00:05:00
  TRACK 03 AUDIO
    TITLE "Third Track"
    INDEX 01 00:07:00
`
	if err := os.WriteFile(cuePath, []byte(cueContent), 0644); err != nil {
		t.Fatal(err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	exitCode := runWithIO([]string{"split", "--out", outDir, "--json", inDir}, &stdoutBuf, &stderrBuf)
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d. stderr: %s", exitCode, stderrBuf.String())
	}

	var res JSONResult
	if err := json.Unmarshal(stdoutBuf.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse JSON result: %v\nstdout: %s", err, stdoutBuf.String())
	}

	if !res.Verified {
		t.Errorf("expected verified == true")
	}
	if res.PcmMd5In != res.PcmMd5Out {
		t.Errorf("pcm_md5_in (%s) != pcm_md5_out (%s)", res.PcmMd5In, res.PcmMd5Out)
	}
	if res.SampleRate != 44100 {
		t.Errorf("expected sample rate 44100, got %d", res.SampleRate)
	}
	if res.BitsPerSample != 16 {
		t.Errorf("expected bits 16, got %d", res.BitsPerSample)
	}
	if len(res.Tracks) != 3 {
		t.Fatalf("expected 3 tracks, got %d", len(res.Tracks))
	}

	// Verify track 1 first_sample == 0
	if res.Tracks[0].FirstSample != 0 {
		t.Errorf("expected track 1 first_sample 0, got %d", res.Tracks[0].FirstSample)
	}
	// Verify sample coverage
	if res.Tracks[0].EndSample != res.Tracks[1].FirstSample {
		t.Errorf("gap/overlap between track 1 end (%d) and track 2 start (%d)", res.Tracks[0].EndSample, res.Tracks[1].FirstSample)
	}
	if res.Tracks[1].EndSample != res.Tracks[2].FirstSample {
		t.Errorf("gap/overlap between track 2 end (%d) and track 3 start (%d)", res.Tracks[1].EndSample, res.Tracks[2].FirstSample)
	}
	if res.Tracks[2].EndSample != res.TotalSamples {
		t.Errorf("track 3 end (%d) != total samples (%d)", res.Tracks[2].EndSample, res.TotalSamples)
	}

	// Check files on disk
	albumDir := filepath.Join(outDir, "album_in")
	entries, err := os.ReadDir(albumDir)
	if err != nil {
		t.Fatalf("failed to read album directory: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 files in album dir, got %d", len(entries))
	}

	// Check tags on track 1
	tags := readTagsWithFFprobe(t, filepath.Join(albumDir, entries[0].Name()))
	if tags["artist"] != "Lusoris Artist" {
		t.Errorf("expected artist 'Lusoris Artist', got %q", tags["artist"])
	}
	if tags["title"] != "First Track with HTOA" {
		t.Errorf("expected title 'First Track with HTOA', got %q", tags["title"])
	}
	if tags["album"] != "Album HTOA Test" {
		t.Errorf("expected album 'Album HTOA Test', got %q", tags["album"])
	}
	if tags["date"] != "2026" {
		t.Errorf("expected date '2026', got %q", tags["date"])
	}
	if tags["genre"] != "Electronic" {
		t.Errorf("expected genre 'Electronic', got %q", tags["genre"])
	}
}

func TestIntegration_96000_24bit_FLAC(t *testing.T) {
	requireTools(t, "ffmpeg", "ffprobe", "flac")

	tmpDir := t.TempDir()
	inDir := filepath.Join(tmpDir, "hires_in")
	outDir := filepath.Join(tmpDir, "hires_out")
	if err := os.MkdirAll(inDir, 0755); err != nil {
		t.Fatal(err)
	}

	imagePath := filepath.Join(inDir, "hires.flac")
	synthesizeFLAC(t, imagePath, 96000, 24, []int{2, 3, 2}, []int{440, 880, 1320})

	cuePath := filepath.Join(inDir, "hires.cue")
	cueContent := `TITLE "HiRes 24bit Album"
PERFORMER "HiRes Band"
FILE "hires.flac" FLAC
  TRACK 01 AUDIO
    TITLE "Track 1"
    INDEX 01 00:00:00
  TRACK 02 AUDIO
    TITLE "Track 2"
    INDEX 01 00:02:00
  TRACK 03 AUDIO
    TITLE "Track 3"
    INDEX 01 00:05:00
`
	if err := os.WriteFile(cuePath, []byte(cueContent), 0644); err != nil {
		t.Fatal(err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	exitCode := runWithIO([]string{"split", "--out", outDir, "--json", cuePath}, &stdoutBuf, &stderrBuf)
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d. stderr: %s", exitCode, stderrBuf.String())
	}

	var res JSONResult
	if err := json.Unmarshal(stdoutBuf.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if res.SampleRate != 96000 {
		t.Errorf("expected sample rate 96000, got %d", res.SampleRate)
	}
	if res.BitsPerSample != 24 {
		t.Errorf("expected bits 24, got %d", res.BitsPerSample)
	}
	if res.PcmMd5In != res.PcmMd5Out {
		t.Errorf("pcm md5 mismatch: %s != %s", res.PcmMd5In, res.PcmMd5Out)
	}

	// Read back output bit depth using ffprobe
	albumDir := filepath.Join(outDir, "hires_in")
	info, err := ProbeAudio(t.Context(), filepath.Join(albumDir, "01 - Track 1.flac"))
	if err != nil {
		t.Fatalf("ProbeAudio failed on output: %v", err)
	}
	if info.BitsPerSample != 24 {
		t.Errorf("output file bits_per_sample expected 24, got %d", info.BitsPerSample)
	}
	if info.SampleRate != 96000 {
		t.Errorf("output file sample_rate expected 96000, got %d", info.SampleRate)
	}
}

func TestIntegration_WAV_StandaloneCue(t *testing.T) {
	requireTools(t, "ffmpeg", "ffprobe", "flac")

	tmpDir := t.TempDir()
	inDir := filepath.Join(tmpDir, "wav_in")
	outDir := filepath.Join(tmpDir, "wav_out")
	if err := os.MkdirAll(inDir, 0755); err != nil {
		t.Fatal(err)
	}

	imagePath := filepath.Join(inDir, "album.wav")
	synthesizeWAV(t, imagePath, 44100, []int{2, 2}, []int{350, 700})

	cuePath := filepath.Join(inDir, "album.cue")
	cueContent := `TITLE "WAV Album"
PERFORMER "WAV Artist"
FILE "album.wav" WAVE
  TRACK 01 AUDIO
    TITLE "WAV Track 1"
    INDEX 01 00:00:00
  TRACK 02 AUDIO
    TITLE "WAV Track 2"
    INDEX 01 00:02:00
`
	if err := os.WriteFile(cuePath, []byte(cueContent), 0644); err != nil {
		t.Fatal(err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	exitCode := runWithIO([]string{"split", "--out", outDir, "--json", inDir}, &stdoutBuf, &stderrBuf)
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d. stderr: %s", exitCode, stderrBuf.String())
	}

	var res JSONResult
	if err := json.Unmarshal(stdoutBuf.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if res.PcmMd5In != res.PcmMd5Out {
		t.Errorf("pcm md5 mismatch: %s != %s", res.PcmMd5In, res.PcmMd5Out)
	}
	if len(res.Tracks) != 2 {
		t.Errorf("expected 2 tracks, got %d", len(res.Tracks))
	}
}

func TestIntegration_FLAC_EmbeddedCuesheet(t *testing.T) {
	requireTools(t, "ffmpeg", "ffprobe", "flac", "metaflac")

	tmpDir := t.TempDir()
	inDir := filepath.Join(tmpDir, "embedded_in")
	outDir1 := filepath.Join(tmpDir, "out_vorbis")
	outDir2 := filepath.Join(tmpDir, "out_block")
	if err := os.MkdirAll(inDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 1. Create source FLAC image
	baseFlac := filepath.Join(inDir, "source.flac")
	synthesizeFLAC(t, baseFlac, 44100, 16, []int{2, 3}, []int{400, 800})

	cuePath := filepath.Join(tmpDir, "test.cue")
	cueContent := `TITLE "Embedded Album"
PERFORMER "Embedded Artist"
FILE "source.flac" FLAC
  TRACK 01 AUDIO
    TITLE "Track One"
    INDEX 01 00:00:00
  TRACK 02 AUDIO
    TITLE "Track Two"
    INDEX 01 00:02:00
`
	if err := os.WriteFile(cuePath, []byte(cueContent), 0644); err != nil {
		t.Fatal(err)
	}

	// FLAC 1: Embedded Vorbis comment CUESHEET=
	flacVorbis := filepath.Join(tmpDir, "vorbis.flac")
	if err := copyFile(baseFlac, flacVorbis); err != nil {
		t.Fatal(err)
	}
	cmdTag := exec.Command("metaflac", "--set-tag-from-file=CUESHEET="+cuePath, flacVorbis)
	if out, err := cmdTag.CombinedOutput(); err != nil {
		t.Fatalf("metaflac --set-tag-from-file failed: %v (%s)", err, string(out))
	}

	// FLAC 2: Embedded CUESHEET block (block type 5)
	flacBlock := filepath.Join(tmpDir, "block.flac")
	if err := copyFile(baseFlac, flacBlock); err != nil {
		t.Fatal(err)
	}
	cmdBlock := exec.Command("metaflac", "--import-cuesheet-from="+cuePath, flacBlock)
	if out, err := cmdBlock.CombinedOutput(); err != nil {
		t.Fatalf("metaflac --import-cuesheet-from failed: %v (%s)", err, string(out))
	}

	// Split FLAC 1 (Vorbis comment)
	var stdout1, stderr1 bytes.Buffer
	exit1 := runWithIO([]string{"split", "--out", outDir1, "--json", flacVorbis}, &stdout1, &stderr1)
	if exit1 != 0 {
		t.Fatalf("flacVorbis split failed: exit %d, stderr: %s", exit1, stderr1.String())
	}
	var res1 JSONResult
	if err := json.Unmarshal(stdout1.Bytes(), &res1); err != nil {
		t.Fatalf("failed to parse json 1: %v", err)
	}
	if res1.CueSource != "flac-vorbis-cuesheet" {
		t.Errorf("expected cue_source 'flac-vorbis-cuesheet', got %q", res1.CueSource)
	}

	// Split FLAC 2 (CUESHEET block)
	var stdout2, stderr2 bytes.Buffer
	exit2 := runWithIO([]string{"split", "--out", outDir2, "--json", flacBlock}, &stdout2, &stderr2)
	if exit2 != 0 {
		t.Fatalf("flacBlock split failed: exit %d, stderr: %s", exit2, stderr2.String())
	}
	var res2 JSONResult
	if err := json.Unmarshal(stdout2.Bytes(), &res2); err != nil {
		t.Fatalf("failed to parse json 2: %v", err)
	}
	if res2.CueSource != "flac-cuesheet-block" {
		t.Errorf("expected cue_source 'flac-cuesheet-block', got %q", res2.CueSource)
	}

	// Both split, md5 equal
	if res1.PcmMd5In != res2.PcmMd5In {
		t.Errorf("input PCM MD5 mismatch between Vorbis and Block forms: %s != %s", res1.PcmMd5In, res2.PcmMd5In)
	}
	if res1.PcmMd5Out != res2.PcmMd5Out {
		t.Errorf("output PCM MD5 mismatch between Vorbis and Block forms: %s != %s", res1.PcmMd5Out, res2.PcmMd5Out)
	}
	if res1.PcmMd5In != res1.PcmMd5Out {
		t.Errorf("PCM MD5 roundtrip failed for Vorbis form: %s != %s", res1.PcmMd5In, res1.PcmMd5Out)
	}
}

func TestIntegration_MalformedCue_Refused(t *testing.T) {
	requireTools(t, "ffmpeg", "ffprobe", "flac")

	tmpDir := t.TempDir()
	inDir := filepath.Join(tmpDir, "malformed_in")
	outDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(inDir, 0755); err != nil {
		t.Fatal(err)
	}

	imagePath := filepath.Join(inDir, "test.flac")
	synthesizeFLAC(t, imagePath, 44100, 16, []int{2}, []int{440})

	cuePath := filepath.Join(inDir, "test.cue")
	// Missing INDEX 01
	cueContent := `FILE "test.flac" FLAC
  TRACK 01 AUDIO
    TITLE "Bad Track"
    INDEX 00 00:00:00
`
	if err := os.WriteFile(cuePath, []byte(cueContent), 0644); err != nil {
		t.Fatal(err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	exitCode := runWithIO([]string{"split", "--out", outDir, inDir}, &stdoutBuf, &stderrBuf)
	if exitCode != 2 {
		t.Fatalf("expected exit 2, got %d. stderr: %s", exitCode, stderrBuf.String())
	}

	// Assert --out is empty afterwards
	entries, err := os.ReadDir(outDir)
	if err == nil && len(entries) > 0 {
		t.Errorf("expected out directory to be empty, found %d entries", len(entries))
	}
}

func TestIntegration_IndexBeyondAudioLength_Refused(t *testing.T) {
	requireTools(t, "ffmpeg", "ffprobe", "flac")

	tmpDir := t.TempDir()
	inDir := filepath.Join(tmpDir, "beyond_in")
	outDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(inDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 2 seconds audio
	imagePath := filepath.Join(inDir, "test.flac")
	synthesizeFLAC(t, imagePath, 44100, 16, []int{2}, []int{440})

	// CUE has index at 10 seconds
	cuePath := filepath.Join(inDir, "test.cue")
	cueContent := `FILE "test.flac" FLAC
  TRACK 01 AUDIO
    INDEX 01 00:10:00
`
	if err := os.WriteFile(cuePath, []byte(cueContent), 0644); err != nil {
		t.Fatal(err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	exitCode := runWithIO([]string{"split", "--out", outDir, inDir}, &stdoutBuf, &stderrBuf)
	if exitCode != 2 {
		t.Fatalf("expected exit 2, got %d. stderr: %s", exitCode, stderrBuf.String())
	}

	// Assert --out is empty
	entries, err := os.ReadDir(outDir)
	if err == nil && len(entries) > 0 {
		t.Errorf("expected out directory to be empty, found %d entries", len(entries))
	}
}

func TestIntegration_InjectedFailure_Cleanup(t *testing.T) {
	requireTools(t, "ffmpeg", "ffprobe", "flac")

	tmpDir := t.TempDir()
	inDir := filepath.Join(tmpDir, "fail_in")
	outDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(inDir, 0755); err != nil {
		t.Fatal(err)
	}

	imagePath := filepath.Join(inDir, "test.flac")
	synthesizeFLAC(t, imagePath, 44100, 16, []int{2, 2, 2}, []int{300, 600, 900})

	cuePath := filepath.Join(inDir, "test.cue")
	cueContent := `FILE "test.flac" FLAC
  TRACK 01 AUDIO
    INDEX 01 00:00:00
  TRACK 02 AUDIO
    INDEX 01 00:02:00
  TRACK 03 AUDIO
    INDEX 01 00:04:00
`
	if err := os.WriteFile(cuePath, []byte(cueContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Inject failure after track 1 (before or during track 2)
	t.Setenv("CUESPLIT_TEST_FAIL_AFTER_TRACK", "2")

	var stdoutBuf, stderrBuf bytes.Buffer
	exitCode := runWithIO([]string{"split", "--out", outDir, inDir}, &stdoutBuf, &stderrBuf)
	if exitCode != 1 {
		t.Fatalf("expected exit 1 on injected failure, got %d. stderr: %s", exitCode, stderrBuf.String())
	}

	// Verify --out is empty and no staging directory is left
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("failed to read outDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected --out to be empty after failure, but found %d entries: %+v", len(entries), entries)
	}
}

func TestIntegration_TargetExists_Refused(t *testing.T) {
	requireTools(t, "ffmpeg", "ffprobe", "flac")

	tmpDir := t.TempDir()
	inDir := filepath.Join(tmpDir, "my_album")
	outDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(inDir, 0755); err != nil {
		t.Fatal(err)
	}

	imagePath := filepath.Join(inDir, "test.flac")
	synthesizeFLAC(t, imagePath, 44100, 16, []int{2}, []int{440})

	cuePath := filepath.Join(inDir, "test.cue")
	cueContent := `FILE "test.flac" FLAC
  TRACK 01 AUDIO
    INDEX 01 00:00:00
`
	if err := os.WriteFile(cuePath, []byte(cueContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Pre-create target directory: <out>/my_album with a sentinel file
	existingTarget := filepath.Join(outDir, "my_album")
	if err := os.MkdirAll(existingTarget, 0755); err != nil {
		t.Fatal(err)
	}
	sentinelPath := filepath.Join(existingTarget, "sentinel.txt")
	if err := os.WriteFile(sentinelPath, []byte("pre-existing data"), 0644); err != nil {
		t.Fatal(err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	exitCode := runWithIO([]string{"split", "--out", outDir, inDir}, &stdoutBuf, &stderrBuf)
	if exitCode != 2 {
		t.Fatalf("expected exit 2 (target exists), got %d. stderr: %s", exitCode, stderrBuf.String())
	}
	if !strings.Contains(stderrBuf.String(), "target exists") {
		t.Errorf("expected 'target exists' in stderr, got %q", stderrBuf.String())
	}

	// Verify existing directory untouched
	data, err := os.ReadFile(sentinelPath)
	if err != nil || string(data) != "pre-existing data" {
		t.Errorf("pre-existing sentinel file was modified or deleted: %v, data: %q", err, string(data))
	}
}

func copyFile(src, dst string) error {
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, in, 0644)
}
