// SPDX-FileCopyrightText: 2026 lusoris
// SPDX-License-Identifier: MIT

package cuesheet_test

import (
	"errors"
	"strings"
	"testing"

	"cauda.dev/cuesplit/cuesheet"
	"golang.org/x/text/encoding/charmap"
)

func TestGoodSheet(t *testing.T) {
	cueData := `REM DATE 1973
REM GENRE "Progressive Rock"
REM COMPOSER "Pink Floyd"
REM DISCNUMBER 1
REM TOTALDISCS 1
PERFORMER "Pink Floyd"
TITLE "The Dark Side of the Moon"
FILE "darkside.flac" FLAC
  TRACK 01 AUDIO
    TITLE "Speak to Me"
    INDEX 01 00:00:00
  TRACK 02 AUDIO
    TITLE "Breathe"
    INDEX 00 01:05:35
    INDEX 01 01:08:00
`
	sheet, err := cuesheet.Parse([]byte(cueData))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sheet.Performer != "Pink Floyd" {
		t.Errorf("expected performer 'Pink Floyd', got %q", sheet.Performer)
	}
	if sheet.Title != "The Dark Side of the Moon" {
		t.Errorf("expected title 'The Dark Side of the Moon', got %q", sheet.Title)
	}
	if sheet.Date != "1973" {
		t.Errorf("expected date '1973', got %q", sheet.Date)
	}
	if sheet.Genre != "Progressive Rock" {
		t.Errorf("expected genre 'Progressive Rock', got %q", sheet.Genre)
	}
	if sheet.DiscNumber != 1 {
		t.Errorf("expected disc 1, got %d", sheet.DiscNumber)
	}
	if sheet.TotalDiscs != 1 {
		t.Errorf("expected total discs 1, got %d", sheet.TotalDiscs)
	}
	if sheet.File == nil || sheet.File.Name != "darkside.flac" || sheet.File.Type != cuesheet.FileTypeFlac {
		t.Fatalf("unexpected file: %+v", sheet.File)
	}
	if len(sheet.File.Tracks) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(sheet.File.Tracks))
	}
	t1 := sheet.File.Tracks[0]
	if t1.Number != 1 || t1.Title != "Speak to Me" || !t1.HasIndex(1) {
		t.Errorf("unexpected track 1: %+v", t1)
	}
	t2 := sheet.File.Tracks[1]
	if t2.Number != 2 || t2.Title != "Breathe" || !t2.HasIndex(0) || !t2.HasIndex(1) {
		t.Errorf("unexpected track 2: %+v", t2)
	}
}

func TestBOM(t *testing.T) {
	cueData := []byte{0xEF, 0xBB, 0xBF}
	cueData = append(cueData, []byte(`FILE "album.wav" WAVE
  TRACK 01 AUDIO
    TITLE "Track with BOM"
    INDEX 01 00:00:00
`)...)

	sheet, err := cuesheet.Parse(cueData)
	if err != nil {
		t.Fatalf("failed to parse CUE with BOM: %v", err)
	}
	if sheet.File.Tracks[0].Title != "Track with BOM" {
		t.Errorf("unexpected title: %q", sheet.File.Tracks[0].Title)
	}
}

func TestCP1251TitleDecodes(t *testing.T) {
	// A CUE sheet with Cyrillic text in CP1251 encoding
	utf8Text := `REM GENRE "Рок"
REM DATE 1988
PERFORMER "Кино"
TITLE "Группа крови"
FILE "kino.wav" WAVE
  TRACK 01 AUDIO
    TITLE "Группа крови"
    INDEX 01 00:00:00
  TRACK 02 AUDIO
    TITLE "Закрой за мной дверь, я ухожу"
    INDEX 01 04:45:00
  TRACK 03 AUDIO
    TITLE "Война"
    INDEX 01 09:00:00
  TRACK 04 AUDIO
    TITLE "Спокойная ночь"
    INDEX 01 13:04:00
  TRACK 05 AUDIO
    TITLE "Мама, мы все тяжело больны"
    INDEX 01 19:12:00
  TRACK 07 AUDIO
    TITLE "В наших глазах"
    INDEX 01 27:27:00
  TRACK 08 AUDIO
    TITLE "Попробуй спеть вместе со мной"
    INDEX 01 31:01:00
  TRACK 09 AUDIO
    TITLE "Прохожий"
    INDEX 01 35:36:00
  TRACK 10 AUDIO
    TITLE "Дальше действовать будем мы"
    INDEX 01 39:15:00
  TRACK 11 AUDIO
    TITLE "Легенда"
    INDEX 01 43:10:00
`
	encoder := charmap.Windows1251.NewEncoder()
	cp1251Bytes, err := encoder.Bytes([]byte(utf8Text))
	if err != nil {
		t.Fatalf("failed to encode to CP1251: %v", err)
	}

	sheet, err := cuesheet.Parse(cp1251Bytes)
	if err != nil {
		t.Fatalf("failed to parse CP1251 CUE: %v", err)
	}

	if sheet.Performer != "Кино" {
		t.Errorf("expected performer 'Кино', got %q", sheet.Performer)
	}
	if sheet.Title != "Группа крови" {
		t.Errorf("expected title 'Группа крови', got %q", sheet.Title)
	}
	if sheet.File.Tracks[0].Title != "Группа крови" {
		t.Errorf("expected track 1 title 'Группа крови', got %q", sheet.File.Tracks[0].Title)
	}
	if sheet.File.Tracks[1].Title != "Закрой за мной дверь, я ухожу" {
		t.Errorf("expected track 2 title 'Закрой за мной дверь, я ухожу', got %q", sheet.File.Tracks[1].Title)
	}
	if sheet.File.Tracks[2].Title != "Война" {
		t.Errorf("expected track 3 title 'Война', got %q", sheet.File.Tracks[2].Title)
	}
}

func TestMultiFILERefused(t *testing.T) {
	cueData := `FILE "file1.wav" WAVE
  TRACK 01 AUDIO
    INDEX 01 00:00:00
FILE "file2.wav" WAVE
  TRACK 02 AUDIO
    INDEX 01 00:00:00
`
	_, err := cuesheet.Parse([]byte(cueData))
	if err == nil {
		t.Fatal("expected multi-FILE error, got nil")
	}
	var cueErr *cuesheet.Error
	if !errors.As(err, &cueErr) {
		t.Fatalf("expected *cuesheet.Error, got %T: %v", err, err)
	}
	if cueErr.Line != 4 {
		t.Errorf("expected error on line 4, got line %d", cueErr.Line)
	}
	if !strings.Contains(cueErr.Msg, "multi-FILE") {
		t.Errorf("expected 'multi-FILE' in error message, got %q", cueErr.Msg)
	}
}

func TestMissingIndex01Refused(t *testing.T) {
	cueData := `FILE "file1.wav" WAVE
  TRACK 01 AUDIO
    INDEX 00 00:00:00
  TRACK 02 AUDIO
    INDEX 01 01:00:00
`
	_, err := cuesheet.Parse([]byte(cueData))
	if err == nil {
		t.Fatal("expected missing INDEX 01 error, got nil")
	}
	var cueErr *cuesheet.Error
	if !errors.As(err, &cueErr) {
		t.Fatalf("expected *cuesheet.Error, got %T: %v", err, err)
	}
	if cueErr.Line != 2 {
		t.Errorf("expected error on line 2, got line %d", cueErr.Line)
	}
	if !strings.Contains(cueErr.Rule, "INDEX 01") {
		t.Errorf("expected INDEX 01 in rule, got %q", cueErr.Rule)
	}
}

func TestNonIncreasingIndexesRefused(t *testing.T) {
	// Inside track 1: INDEX 01 followed by earlier time or smaller index
	cueData := `FILE "file1.wav" WAVE
  TRACK 01 AUDIO
    INDEX 01 01:00:00
    INDEX 02 00:30:00
`
	_, err := cuesheet.Parse([]byte(cueData))
	if err == nil {
		t.Fatal("expected non-increasing index error, got nil")
	}
	var cueErr *cuesheet.Error
	if !errors.As(err, &cueErr) {
		t.Fatalf("expected *cuesheet.Error, got %T: %v", err, err)
	}
	if cueErr.Line != 4 {
		t.Errorf("expected error on line 4, got line %d", cueErr.Line)
	}
}

func TestCrossTrackIndex01NotIncreasingRefused(t *testing.T) {
	cueData := `FILE "file1.wav" WAVE
  TRACK 01 AUDIO
    INDEX 01 02:00:00
  TRACK 02 AUDIO
    INDEX 01 01:00:00
`
	_, err := cuesheet.Parse([]byte(cueData))
	if err == nil {
		t.Fatal("expected cross-track INDEX 01 error, got nil")
	}
	var cueErr *cuesheet.Error
	if !errors.As(err, &cueErr) {
		t.Fatalf("expected *cuesheet.Error, got %T: %v", err, err)
	}
	if cueErr.Line != 4 {
		t.Errorf("expected error on line 4, got line %d", cueErr.Line)
	}
}

func TestBadTimeRefused(t *testing.T) {
	cueData := `FILE "file1.wav" WAVE
  TRACK 01 AUDIO
    INDEX 01 00:00:75
`
	_, err := cuesheet.Parse([]byte(cueData))
	if err == nil {
		t.Fatal("expected bad time error, got nil")
	}
	var cueErr *cuesheet.Error
	if !errors.As(err, &cueErr) {
		t.Fatalf("expected *cuesheet.Error, got %T: %v", err, err)
	}
	if cueErr.Line != 3 {
		t.Errorf("expected error on line 3, got line %d", cueErr.Line)
	}
	if !strings.Contains(cueErr.Msg, "75") {
		t.Errorf("expected mention of 75 in error message, got %q", cueErr.Msg)
	}
}

func TestNonAUDIOTrackRefused(t *testing.T) {
	cueData := `FILE "data.wav" WAVE
  TRACK 01 MODE1/2048
    INDEX 01 00:00:00
`
	_, err := cuesheet.Parse([]byte(cueData))
	if err == nil {
		t.Fatal("expected non-AUDIO track error, got nil")
	}
	var cueErr *cuesheet.Error
	if !errors.As(err, &cueErr) {
		t.Fatalf("expected *cuesheet.Error, got %T: %v", err, err)
	}
	if cueErr.Line != 2 {
		t.Errorf("expected error on line 2, got line %d", cueErr.Line)
	}
	if cueErr.Rule != "non-AUDIO track" {
		t.Errorf("expected rule 'non-AUDIO track', got %q", cueErr.Rule)
	}
}

func TestPregapRefused(t *testing.T) {
	cueData := `FILE "file1.wav" WAVE
  TRACK 01 AUDIO
    PREGAP 00:02:00
    INDEX 01 00:00:00
`
	_, err := cuesheet.Parse([]byte(cueData))
	if err == nil {
		t.Fatal("expected PREGAP error, got nil")
	}
	var cueErr *cuesheet.Error
	if !errors.As(err, &cueErr) {
		t.Fatalf("expected *cuesheet.Error, got %T: %v", err, err)
	}
	if cueErr.Line != 3 {
		t.Errorf("expected error on line 3, got line %d", cueErr.Line)
	}
	if cueErr.Rule != "PREGAP/POSTGAP" {
		t.Errorf("expected rule 'PREGAP/POSTGAP', got %q", cueErr.Rule)
	}
}

func TestPostgapRefused(t *testing.T) {
	cueData := `FILE "file1.wav" WAVE
  TRACK 01 AUDIO
    INDEX 01 00:00:00
    POSTGAP 00:02:00
`
	_, err := cuesheet.Parse([]byte(cueData))
	if err == nil {
		t.Fatal("expected POSTGAP error, got nil")
	}
	var cueErr *cuesheet.Error
	if !errors.As(err, &cueErr) {
		t.Fatalf("expected *cuesheet.Error, got %T: %v", err, err)
	}
	if cueErr.Line != 4 {
		t.Errorf("expected error on line 4, got line %d", cueErr.Line)
	}
	if cueErr.Rule != "PREGAP/POSTGAP" {
		t.Errorf("expected rule 'PREGAP/POSTGAP', got %q", cueErr.Rule)
	}
}

func TestUnknownCommandRefused(t *testing.T) {
	cueData := `FILE "file1.wav" WAVE
  FOOBAR 123
  TRACK 01 AUDIO
    INDEX 01 00:00:00
`
	_, err := cuesheet.Parse([]byte(cueData))
	if err == nil {
		t.Fatal("expected unknown command error, got nil")
	}
	var cueErr *cuesheet.Error
	if !errors.As(err, &cueErr) {
		t.Fatalf("expected *cuesheet.Error, got %T: %v", err, err)
	}
	if cueErr.Line != 2 {
		t.Errorf("expected error on line 2, got line %d", cueErr.Line)
	}
	if cueErr.Rule != "unknown command" {
		t.Errorf("expected rule 'unknown command', got %q", cueErr.Rule)
	}
}

func TestQuotedAndUnquotedArgs(t *testing.T) {
	cueData := `REM GENRE Pop
PERFORMER "Artist with spaces"
TITLE SimpleTitle
FILE "audio file.flac" FLAC
  TRACK 01 AUDIO
    TITLE "Track \"One\" with quotes"
    ISRC US1234567890
    FLAGS DCP 4CH
    INDEX 01 00:00:00
`
	sheet, err := cuesheet.Parse([]byte(cueData))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sheet.Genre != "Pop" {
		t.Errorf("expected genre Pop, got %q", sheet.Genre)
	}
	if sheet.Performer != "Artist with spaces" {
		t.Errorf("expected performer 'Artist with spaces', got %q", sheet.Performer)
	}
	if sheet.Title != "SimpleTitle" {
		t.Errorf("expected title 'SimpleTitle', got %q", sheet.Title)
	}
	if sheet.File.Name != "audio file.flac" {
		t.Errorf("expected file name 'audio file.flac', got %q", sheet.File.Name)
	}
	tr := sheet.File.Tracks[0]
	if tr.Title != `Track "One" with quotes` {
		t.Errorf("expected track title `Track \"One\" with quotes`, got %q", tr.Title)
	}
	if tr.ISRC != "US1234567890" {
		t.Errorf("expected ISRC US1234567890, got %q", tr.ISRC)
	}
	if len(tr.Flags) != 2 || tr.Flags[0] != "DCP" || tr.Flags[1] != "4CH" {
		t.Errorf("unexpected flags: %+v", tr.Flags)
	}
}
