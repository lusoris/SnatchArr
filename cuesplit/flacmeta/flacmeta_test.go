// SPDX-FileCopyrightText: 2026 lusoris
// SPDX-License-Identifier: MIT

package flacmeta_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"cauda.dev/cuesplit/flacmeta"
)

func TestInvalidMarker(t *testing.T) {
	data := []byte("RIFF1234WAVE")
	_, err := flacmeta.Read(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for non-FLAC data, got nil")
	}
}

func TestVorbisCommentCueSheet(t *testing.T) {
	// Synthesize a minimal FLAC byte stream with STREAMINFO and VORBIS_COMMENT
	var buf bytes.Buffer
	buf.WriteString("fLaC")

	// Block 0: STREAMINFO (34 bytes), not last (header[0] = 0)
	streamInfo := make([]byte, 34)
	// Sample rate: 44100 (0x0AC44) -> bits 80-99
	// 44100 = 0x0AC44: data[10] = 0x0A, data[11] = 0xC4, data[12] = 0x40 (channels mono: 0, bps 16: 0xF -> (0x0 << 1) | 0x01)
	streamInfo[10] = 0x0A
	streamInfo[11] = 0xC4
	streamInfo[12] = 0x40 // sample_rate lower 4 bits is 4 (0x40), channels mono (0)
	streamInfo[13] = 0xF0 // bits per sample 16 (15=0x0F)
	// total samples: 88200 (2 seconds)
	binary.BigEndian.PutUint32(streamInfo[14:18], 88200)

	buf.WriteByte(0x00) // STREAMINFO, not last
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)
	buf.WriteByte(34)
	buf.Write(streamInfo)

	// Block 1: VORBIS_COMMENT, is last (header[0] = 0x84)
	var vcBuf bytes.Buffer
	// vendor length 4 + "test"
	binary.Write(&vcBuf, binary.LittleEndian, uint32(4))
	vcBuf.WriteString("test")
	// 3 comments
	binary.Write(&vcBuf, binary.LittleEndian, uint32(3))

	c1 := "ARTIST=Test Artist"
	binary.Write(&vcBuf, binary.LittleEndian, uint32(len(c1)))
	vcBuf.WriteString(c1)

	c2 := "ALBUM=Test Album"
	binary.Write(&vcBuf, binary.LittleEndian, uint32(len(c2)))
	vcBuf.WriteString(c2)

	c3 := "CUESHEET=FILE \"test.flac\" FLAC\n  TRACK 01 AUDIO\n    INDEX 01 00:00:00\n"
	binary.Write(&vcBuf, binary.LittleEndian, uint32(len(c3)))
	vcBuf.WriteString(c3)

	vcBytes := vcBuf.Bytes()
	buf.WriteByte(0x84) // VORBIS_COMMENT, isLast = true
	buf.WriteByte(byte(len(vcBytes) >> 16))
	buf.WriteByte(byte(len(vcBytes) >> 8))
	buf.WriteByte(byte(len(vcBytes)))
	buf.Write(vcBytes)

	meta, err := flacmeta.Read(&buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta.StreamInfo.SampleRate != 44100 {
		t.Errorf("expected sample rate 44100, got %d", meta.StreamInfo.SampleRate)
	}
	if meta.VorbisComments["ARTIST"] != "Test Artist" {
		t.Errorf("expected artist 'Test Artist', got %q", meta.VorbisComments["ARTIST"])
	}
	if meta.VorbisComments["ALBUM"] != "Test Album" {
		t.Errorf("expected album 'Test Album', got %q", meta.VorbisComments["ALBUM"])
	}
	if meta.VorbisCueSheet != "FILE \"test.flac\" FLAC\n  TRACK 01 AUDIO\n    INDEX 01 00:00:00\n" {
		t.Errorf("unexpected VorbisCueSheet: %q", meta.VorbisCueSheet)
	}
}

func TestCueSheetBlockWithMetaflac(t *testing.T) {
	if _, err := exec.LookPath("metaflac"); err != nil {
		t.Skip("metaflac not installed")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}

	tmpDir := t.TempDir()
	flacPath := filepath.Join(tmpDir, "test.flac")
	cuePath := filepath.Join(tmpDir, "test.cue")

	// Synthesize a 3-second FLAC file
	cmd := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=3:sample_rate=44100", "-c:a", "flac", flacPath, "-y")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg failed: %v (%s)", err, string(out))
	}

	cueContent := `FILE "test.flac" FLAC
  TRACK 01 AUDIO
    INDEX 01 00:00:00
  TRACK 02 AUDIO
    INDEX 00 00:01:00
    INDEX 01 00:01:10
`
	if err := os.WriteFile(cuePath, []byte(cueContent), 0644); err != nil {
		t.Fatalf("failed to write cue file: %v", err)
	}

	importCmd := exec.Command("metaflac", "--import-cuesheet-from="+cuePath, flacPath)
	if out, err := importCmd.CombinedOutput(); err != nil {
		t.Fatalf("metaflac import failed: %v (%s)", err, string(out))
	}

	meta, err := flacmeta.ReadFile(flacPath)
	if err != nil {
		t.Fatalf("flacmeta.ReadFile failed: %v", err)
	}

	if meta.CueSheetBlock == nil {
		t.Fatal("expected CueSheetBlock to be present, got nil")
	}

	if len(meta.CueSheetBlock.Tracks) != 2 {
		t.Fatalf("expected 2 audio tracks in CueSheetBlock, got %d", len(meta.CueSheetBlock.Tracks))
	}

	tr1 := meta.CueSheetBlock.Tracks[0]
	if tr1.Number != 1 || len(tr1.Indexes) != 1 {
		t.Errorf("unexpected track 1: %+v", tr1)
	}
	tr2 := meta.CueSheetBlock.Tracks[1]
	if tr2.Number != 2 || len(tr2.Indexes) != 2 {
		t.Errorf("unexpected track 2: %+v", tr2)
	}
	if meta.CueSheetBlock.LeadOutOffset != 132300 {
		t.Errorf("expected lead-out offset 132300, got %d", meta.CueSheetBlock.LeadOutOffset)
	}
}
