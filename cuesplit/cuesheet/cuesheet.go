// SPDX-FileCopyrightText: 2026 lusoris
// SPDX-License-Identifier: MIT

package cuesheet

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/asdfsx/gochardet"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
)

type FileType string

const (
	FileTypeWave FileType = "WAVE"
	FileTypeFlac FileType = "FLAC"
)

type DataType string

const (
	DataTypeAudio DataType = "AUDIO"
)

type Time struct {
	Min    int
	Sec    int
	Frames int
}

func (t Time) TotalFrames() int {
	return (t.Min*60+t.Sec)*75 + t.Frames
}

func (t Time) After(other Time) bool {
	return t.TotalFrames() > other.TotalFrames()
}

func (t Time) AfterOrEqual(other Time) bool {
	return t.TotalFrames() >= other.TotalFrames()
}

func (t Time) Equals(other Time) bool {
	return t.TotalFrames() == other.TotalFrames()
}

func (t Time) Samples(sampleRate int) (int, error) {
	if sampleRate%75 != 0 {
		return 0, fmt.Errorf("sample rate %d is not divisible by 75", sampleRate)
	}
	return (t.Min*60+t.Sec)*sampleRate + (t.Frames*sampleRate)/75, nil
}

type Index struct {
	Number int
	Time   Time
	Line   int
}

type Track struct {
	Number     int
	DataType   DataType
	Title      string
	Performer  string
	Songwriter string
	Composer   string
	ISRC       string
	Flags      []string
	Indexes    []*Index
	Line       int
}

func (tr *Track) HasIndex(num int) bool {
	for _, idx := range tr.Indexes {
		if idx.Number == num {
			return true
		}
	}
	return false
}

func (tr *Track) GetIndex(num int) *Index {
	for _, idx := range tr.Indexes {
		if idx.Number == num {
			return idx
		}
	}
	return nil
}

type File struct {
	Name   string
	Type   FileType
	Tracks []*Track
	Line   int
}

type Sheet struct {
	Performer  string
	Songwriter string
	Title      string
	Catalog    string
	CDTextFile string
	Comments   []string
	Date       string
	Genre      string
	Composer   string
	DiscNumber int
	TotalDiscs int
	File       *File
}

type Error struct {
	Line int
	Rule string
	Msg  string
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %s: %s", e.Line, e.Rule, e.Msg)
	}
	return fmt.Sprintf("%s: %s", e.Rule, e.Msg)
}

func detectDecoder(b []byte) *encoding.Decoder {
	detector := chardet.NewTextDetector()
	best, err := detector.DetectBest(b)
	if err == nil && best != nil && best.Confidence >= 25 {
		if !strings.EqualFold(best.Charset, "UTF-8") && !strings.EqualFold(best.Charset, "ASCII") {
			enc, err := ianaindex.IANA.Encoding(best.Charset)
			if err == nil && enc != nil {
				return enc.NewDecoder()
			}
		}
	}
	return nil
}

func decodeToUTF8(raw []byte) ([]byte, error) {
	// Strip UTF-8 BOM if present
	if bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) {
		return raw[3:], nil
	}

	if dec := detectDecoder(raw); dec != nil {
		decoded, err := dec.Bytes(raw)
		if err == nil {
			return decoded, nil
		}
	}

	return raw, nil
}

func tokenizeLine(line []byte, lineNum int) ([]string, error) {
	var tokens []string
	n := len(line)
	i := 0

	for i < n {
		// Skip whitespace
		for i < n && (line[i] == ' ' || line[i] == '\t' || line[i] == '\r' || line[i] == '\n') {
			i++
		}
		if i >= n {
			break
		}

		if line[i] == '"' {
			// Quoted string
			i++ // skip opening quote
			var sb bytes.Buffer
			closed := false
			for i < n {
				if line[i] == '\\' && i+1 < n && line[i+1] == '"' {
					sb.WriteByte('"')
					i += 2
					continue
				}
				if line[i] == '"' {
					closed = true
					i++ // skip closing quote
					break
				}
				sb.WriteByte(line[i])
				i++
			}
			if !closed {
				return nil, &Error{Line: lineNum, Rule: "syntax error", Msg: "unterminated quote"}
			}
			tokens = append(tokens, sb.String())
		} else {
			// Unquoted token
			start := i
			for i < n && line[i] != ' ' && line[i] != '\t' && line[i] != '\r' && line[i] != '\n' {
				i++
			}
			tokens = append(tokens, string(line[start:i]))
		}
	}

	return tokens, nil
}

// Parse parses a CUE sheet from raw bytes.
func Parse(raw []byte) (*Sheet, error) {
	sheet, err := parseInternal(raw)
	if err != nil {
		return nil, err
	}

	// Unflac logic: collect all parsed metadata text into a buffer and check if chardet detects a non-UTF8 encoding
	buf := new(bytes.Buffer)
	buf.WriteString(sheet.Performer)
	buf.WriteString(" ")
	buf.WriteString(sheet.Songwriter)
	buf.WriteString(" ")
	buf.WriteString(sheet.Title)
	buf.WriteString(" ")
	buf.WriteString(sheet.Genre)
	buf.WriteString(" ")
	buf.WriteString(sheet.Composer)
	buf.WriteString(" ")
	if sheet.File != nil {
		for _, t := range sheet.File.Tracks {
			buf.WriteString(t.Title)
			buf.WriteString(" ")
			buf.WriteString(t.Performer)
			buf.WriteString(" ")
			buf.WriteString(t.Songwriter)
			buf.WriteString(" ")
			buf.WriteString(t.Composer)
			buf.WriteString(" ")
		}
	}
	if dec := detectDecoder(buf.Bytes()); dec != nil {
		rawDecoded, errDec := dec.Bytes(raw)
		if errDec == nil && utf8.Valid(rawDecoded) {
			if reSheet, errRe := parseInternal(rawDecoded); errRe == nil {
				return reSheet, nil
			}
		}
	}

	return sheet, nil
}

func parseInternal(raw []byte) (*Sheet, error) {
	utf8Bytes, err := decodeToUTF8(raw)
	if err != nil {
		return nil, &Error{Line: 0, Rule: "encoding error", Msg: err.Error()}
	}

	sheet := &Sheet{}
	scanner := bufio.NewScanner(bytes.NewReader(utf8Bytes))
	lineNum := 0
	var currentTrack *Track

	for scanner.Scan() {
		lineNum++
		lineBytes := scanner.Bytes()
		trimmed := bytes.TrimSpace(lineBytes)
		if len(trimmed) == 0 {
			continue
		}

		tokens, err := tokenizeLine(lineBytes, lineNum)
		if err != nil {
			return nil, err
		}
		if len(tokens) == 0 {
			continue
		}

		cmd := strings.ToUpper(tokens[0])
		args := tokens[1:]

		switch cmd {
		case "REM":
			if len(args) == 0 {
				continue
			}
			subCmd := strings.ToUpper(args[0])
			rest := strings.TrimSpace(strings.Join(args[1:], " "))
			sheet.Comments = append(sheet.Comments, strings.Join(args, " "))

			switch subCmd {
			case "DATE":
				sheet.Date = rest
			case "GENRE":
				sheet.Genre = rest
			case "COMPOSER":
				if currentTrack != nil {
					currentTrack.Composer = rest
				} else {
					sheet.Composer = rest
				}
			case "DISCNUMBER":
				if n, err := strconv.Atoi(rest); err == nil {
					sheet.DiscNumber = n
				}
			case "TOTALDISCS":
				if n, err := strconv.Atoi(rest); err == nil {
					sheet.TotalDiscs = n
				}
			case "COMMENT":
				// ignored
			default:
				// other REM comments ignored
			}

		case "PERFORMER":
			val := ""
			if len(args) > 0 {
				val = args[0]
			}
			if currentTrack != nil {
				currentTrack.Performer = val
			} else {
				sheet.Performer = val
			}

		case "SONGWRITER":
			val := ""
			if len(args) > 0 {
				val = args[0]
			}
			if currentTrack != nil {
				currentTrack.Songwriter = val
			} else {
				sheet.Songwriter = val
			}

		case "TITLE":
			val := ""
			if len(args) > 0 {
				val = args[0]
			}
			if currentTrack != nil {
				currentTrack.Title = val
			} else {
				sheet.Title = val
			}

		case "CATALOG":
			if len(args) > 0 {
				sheet.Catalog = args[0]
			}

		case "CDTEXTFILE":
			if len(args) > 0 {
				sheet.CDTextFile = args[0]
			}

		case "FILE":
			if sheet.File != nil {
				return nil, &Error{
					Line: lineNum,
					Rule: "multi-FILE",
					Msg:  "multi-FILE cue sheets are not supported",
				}
			}
			if len(args) < 2 {
				return nil, &Error{
					Line: lineNum,
					Rule: "malformed FILE",
					Msg:  "expected filename and file type",
				}
			}
			fileName := args[0]
			fileTypeStr := strings.ToUpper(args[1])
			if fileTypeStr != "WAVE" && fileTypeStr != "FLAC" {
				return nil, &Error{
					Line: lineNum,
					Rule: "unsupported FILE type",
					Msg:  fmt.Sprintf("FILE type %q is not supported (only WAVE and FLAC are accepted)", args[1]),
				}
			}
			sheet.File = &File{
				Name: fileName,
				Type: FileType(fileTypeStr),
				Line: lineNum,
			}

		case "TRACK":
			if sheet.File == nil {
				return nil, &Error{
					Line: lineNum,
					Rule: "TRACK before FILE",
					Msg:  "TRACK specified before FILE",
				}
			}
			if len(args) < 2 {
				return nil, &Error{
					Line: lineNum,
					Rule: "malformed TRACK",
					Msg:  "expected track number and data type",
				}
			}
			num, err := strconv.Atoi(args[0])
			if err != nil || num <= 0 {
				return nil, &Error{
					Line: lineNum,
					Rule: "invalid track number",
					Msg:  fmt.Sprintf("track number %q is invalid", args[0]),
				}
			}
			// Track numbers must be strictly increasing
			if len(sheet.File.Tracks) > 0 {
				prevNum := sheet.File.Tracks[len(sheet.File.Tracks)-1].Number
				if num <= prevNum {
					return nil, &Error{
						Line: lineNum,
						Rule: "track numbers not strictly increasing",
						Msg:  fmt.Sprintf("track number %d is not strictly greater than previous track %d", num, prevNum),
					}
				}
			}
			// Verify previous track had INDEX 01
			if currentTrack != nil {
				if !currentTrack.HasIndex(1) {
					return nil, &Error{
						Line: currentTrack.Line,
						Rule: "a track without INDEX 01",
						Msg:  fmt.Sprintf("track %d has no INDEX 01", currentTrack.Number),
					}
				}
			}
			dataTypeStr := strings.ToUpper(args[1])
			if dataTypeStr != "AUDIO" {
				return nil, &Error{
					Line: lineNum,
					Rule: "non-AUDIO track",
					Msg:  fmt.Sprintf("track %d has non-AUDIO type %q", num, args[1]),
				}
			}
			currentTrack = &Track{
				Number:   num,
				DataType: DataTypeAudio,
				Line:     lineNum,
			}
			sheet.File.Tracks = append(sheet.File.Tracks, currentTrack)

		case "INDEX":
			if currentTrack == nil {
				return nil, &Error{
					Line: lineNum,
					Rule: "INDEX outside TRACK",
					Msg:  "INDEX command found outside of any TRACK",
				}
			}
			if len(args) < 2 {
				return nil, &Error{
					Line: lineNum,
					Rule: "malformed INDEX",
					Msg:  "expected index number and time stamp",
				}
			}
			idxNum, err := strconv.Atoi(args[0])
			if err != nil || idxNum < 0 {
				return nil, &Error{
					Line: lineNum,
					Rule: "invalid index number",
					Msg:  fmt.Sprintf("index number %q is invalid", args[0]),
				}
			}
			// Check index numbers strictly increasing within track
			if len(currentTrack.Indexes) > 0 {
				prevIdx := currentTrack.Indexes[len(currentTrack.Indexes)-1].Number
				if idxNum <= prevIdx {
					return nil, &Error{
						Line: lineNum,
						Rule: "indexes within a track not increasing",
						Msg:  fmt.Sprintf("index number %d is not strictly greater than previous index %d", idxNum, prevIdx),
					}
				}
			}
			// Parse mm:ss:ff
			timeParts := strings.Split(args[1], ":")
			if len(timeParts) != 3 {
				return nil, &Error{
					Line: lineNum,
					Rule: "INDEX times malformed",
					Msg:  fmt.Sprintf("expected mm:ss:ff but got %q", args[1]),
				}
			}
			min, errM := strconv.Atoi(timeParts[0])
			sec, errS := strconv.Atoi(timeParts[1])
			frames, errF := strconv.Atoi(timeParts[2])
			if errM != nil || errS != nil || errF != nil || min < 0 || sec < 0 || sec >= 60 || frames < 0 || frames >= 75 {
				return nil, &Error{
					Line: lineNum,
					Rule: "INDEX times malformed",
					Msg:  fmt.Sprintf("invalid mm:ss:ff time %q (ff must be < 75, ss < 60)", args[1]),
				}
			}
			t := Time{Min: min, Sec: sec, Frames: frames}
			// Check index time is strictly increasing within track
			if len(currentTrack.Indexes) > 0 {
				prevT := currentTrack.Indexes[len(currentTrack.Indexes)-1].Time
				if !t.After(prevT) {
					return nil, &Error{
						Line: lineNum,
						Rule: "indexes within a track not increasing",
						Msg:  fmt.Sprintf("index %d time %q is not after previous index time", idxNum, args[1]),
					}
				}
			}
			currentTrack.Indexes = append(currentTrack.Indexes, &Index{
				Number: idxNum,
				Time:   t,
				Line:   lineNum,
			})

		case "PREGAP":
			return nil, &Error{
				Line: lineNum,
				Rule: "PREGAP/POSTGAP",
				Msg:  "PREGAP is not supported (silence that is not in the image; refuse, do not synthesise)",
			}

		case "POSTGAP":
			return nil, &Error{
				Line: lineNum,
				Rule: "PREGAP/POSTGAP",
				Msg:  "POSTGAP is not supported (silence that is not in the image; refuse, do not synthesise)",
			}

		case "ISRC":
			if currentTrack != nil && len(args) > 0 {
				currentTrack.ISRC = args[0]
			}

		case "FLAGS":
			if currentTrack != nil {
				currentTrack.Flags = append(currentTrack.Flags, args...)
			}

		default:
			return nil, &Error{
				Line: lineNum,
				Rule: "unknown command",
				Msg:  fmt.Sprintf("unknown command %q", tokens[0]),
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, &Error{Line: lineNum, Rule: "read error", Msg: err.Error()}
	}

	// Validate FILE exists
	if sheet.File == nil {
		return nil, &Error{
			Line: 0,
			Rule: "no FILE",
			Msg:  "no FILE command found in cue sheet",
		}
	}

	// Validate TRACKs exist
	if len(sheet.File.Tracks) == 0 {
		return nil, &Error{
			Line: 0,
			Rule: "no TRACK",
			Msg:  "no TRACK commands found in cue sheet",
		}
	}

	// Validate last track has INDEX 01
	lastTrack := sheet.File.Tracks[len(sheet.File.Tracks)-1]
	if !lastTrack.HasIndex(1) {
		return nil, &Error{
			Line: lastTrack.Line,
			Rule: "a track without INDEX 01",
			Msg:  fmt.Sprintf("track %d has no INDEX 01", lastTrack.Number),
		}
	}

	// Validate INDEX 01 of track n+1 is strictly after INDEX 01 of track n
	for i := 0; i < len(sheet.File.Tracks)-1; i++ {
		curr := sheet.File.Tracks[i]
		next := sheet.File.Tracks[i+1]
		idxCurr := curr.GetIndex(1)
		idxNext := next.GetIndex(1)
		if idxCurr == nil || idxNext == nil || !idxNext.Time.After(idxCurr.Time) {
			return nil, &Error{
				Line: next.Line,
				Rule: "INDEX 01 of track n+1 not after INDEX 01 of track n",
				Msg:  fmt.Sprintf("track %d INDEX 01 (%02d:%02d:%02d) is not after track %d INDEX 01 (%02d:%02d:%02d)", next.Number, idxNext.Time.Min, idxNext.Time.Sec, idxNext.Time.Frames, curr.Number, idxCurr.Time.Min, idxCurr.Time.Sec, idxCurr.Time.Frames),
			}
		}
	}

	// Propagate sheet-level metadata to tracks if not set
	for _, tr := range sheet.File.Tracks {
		if tr.Performer == "" {
			tr.Performer = sheet.Performer
		}
		if tr.Songwriter == "" {
			tr.Songwriter = sheet.Songwriter
		}
	}

	return sheet, nil
}

// ParseReader parses a CUE sheet from an io.Reader.
func ParseReader(r io.Reader) (*Sheet, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}
