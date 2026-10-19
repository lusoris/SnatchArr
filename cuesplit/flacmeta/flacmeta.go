// SPDX-FileCopyrightText: 2026 lusoris
// SPDX-License-Identifier: MIT

package flacmeta

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
)

type StreamInfo struct {
	SampleRate    int
	Channels      int
	BitsPerSample int
	TotalSamples  uint64
	MD5           [16]byte
}

type CueIndex struct {
	Number int
	Offset uint64 // samples relative to track offset
}

type CueTrack struct {
	Number      int
	Offset      uint64 // sample offset from start of audio
	ISRC        string
	Type        int // 0 = audio, 1 = non-audio
	PreEmphasis bool
	Indexes     []CueIndex
}

type CueSheetBlock struct {
	MediaCatalogNumber string
	LeadInSamples      uint64
	IsCD               bool
	Tracks             []CueTrack
	LeadOutOffset      uint64
}

type Metadata struct {
	StreamInfo        *StreamInfo
	VorbisComments    map[string]string // uppercase key -> value
	VorbisCommentList []string
	VorbisCueSheet    string
	CueSheetBlock     *CueSheetBlock
}

func Read(r io.Reader) (*Metadata, error) {
	// First 4 bytes must be "fLaC"
	var marker [4]byte
	if _, err := io.ReadFull(r, marker[:]); err != nil {
		return nil, fmt.Errorf("failed to read FLAC marker: %w", err)
	}
	if string(marker[:]) != "fLaC" {
		return nil, fmt.Errorf("not a FLAC file (invalid marker %q)", string(marker[:]))
	}

	meta := &Metadata{
		VorbisComments: make(map[string]string),
	}

	for {
		var header [4]byte
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return nil, fmt.Errorf("failed to read metadata block header: %w", err)
		}

		isLast := (header[0] & 0x80) != 0
		blockType := header[0] & 0x7F
		length := int(header[1])<<16 | int(header[2])<<8 | int(header[3])

		data := make([]byte, length)
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, fmt.Errorf("failed to read metadata block (type %d, len %d): %w", blockType, length, err)
		}

		switch blockType {
		case 0: // STREAMINFO
			if length < 34 {
				return nil, fmt.Errorf("invalid STREAMINFO length %d (expected 34)", length)
			}
			si := &StreamInfo{}
			// sample rate (20 bits): bits 80-99
			si.SampleRate = int(data[10])<<12 | int(data[11])<<4 | int(data[12]>>4)
			// channels (3 bits): bits 100-102
			si.Channels = int((data[12]>>1)&0x07) + 1
			// bits per sample (5 bits): bits 103-107
			si.BitsPerSample = int(((data[12]&0x01)<<4)|(data[13]>>4)) + 1
			// total samples (36 bits): bits 108-143
			si.TotalSamples = uint64(data[13]&0x0F)<<32 |
				uint64(data[14])<<24 |
				uint64(data[15])<<16 |
				uint64(data[16])<<8 |
				uint64(data[17])
			copy(si.MD5[:], data[18:34])
			meta.StreamInfo = si

		case 4: // VORBIS_COMMENT
			if err := parseVorbisComment(data, meta); err != nil {
				return nil, fmt.Errorf("failed to parse VORBIS_COMMENT: %w", err)
			}

		case 5: // CUESHEET
			cs, err := parseCueSheetBlock(data)
			if err != nil {
				return nil, fmt.Errorf("failed to parse CUESHEET block: %w", err)
			}
			meta.CueSheetBlock = cs
		}

		if isLast {
			break
		}
	}

	return meta, nil
}

func ReadFile(path string) (*Metadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f)
}

func parseVorbisComment(data []byte, meta *Metadata) error {
	reader := bytes.NewReader(data)
	var vendorLen uint32
	if err := binary.Read(reader, binary.LittleEndian, &vendorLen); err != nil {
		return err
	}
	if int(vendorLen) > reader.Len() {
		return fmt.Errorf("vendor string length %d exceeds block size", vendorLen)
	}
	vendorStr := make([]byte, vendorLen)
	if _, err := io.ReadFull(reader, vendorStr); err != nil {
		return err
	}

	var numComments uint32
	if err := binary.Read(reader, binary.LittleEndian, &numComments); err != nil {
		return err
	}

	for i := uint32(0); i < numComments; i++ {
		var commentLen uint32
		if err := binary.Read(reader, binary.LittleEndian, &commentLen); err != nil {
			return err
		}
		if int(commentLen) > reader.Len() {
			return fmt.Errorf("comment %d length %d exceeds block size", i, commentLen)
		}
		commentBytes := make([]byte, commentLen)
		if _, err := io.ReadFull(reader, commentBytes); err != nil {
			return err
		}
		commentStr := string(commentBytes)
		meta.VorbisCommentList = append(meta.VorbisCommentList, commentStr)

		parts := strings.SplitN(commentStr, "=", 2)
		if len(parts) == 2 {
			k := strings.ToUpper(strings.TrimSpace(parts[0]))
			v := parts[1]
			if k == "CUESHEET" {
				meta.VorbisCueSheet = v
			}
			// Keep first or update? First occurrence is common, but let's store if not set
			if _, exists := meta.VorbisComments[k]; !exists {
				meta.VorbisComments[k] = v
			}
		}
	}

	return nil
}

func parseCueSheetBlock(data []byte) (*CueSheetBlock, error) {
	if len(data) < 396 {
		return nil, fmt.Errorf("CUESHEET block too short (%d bytes, minimum 396)", len(data))
	}

	cs := &CueSheetBlock{}
	// 128 bytes media catalog number
	cs.MediaCatalogNumber = strings.TrimRight(string(data[:128]), "\x00")
	// 8 bytes lead-in
	cs.LeadInSamples = binary.BigEndian.Uint64(data[128:136])
	// 1 byte flags
	cs.IsCD = (data[136] & 0x80) != 0
	// 258 bytes reserved (data[137:395])
	numTracks := int(data[395])

	offset := 396
	for i := 0; i < numTracks; i++ {
		if offset+36 > len(data) {
			return nil, fmt.Errorf("unexpected EOF reading track %d in CUESHEET block", i)
		}
		trOffset := binary.BigEndian.Uint64(data[offset : offset+8])
		trNum := int(data[offset+8])
		isrc := strings.TrimRight(string(data[offset+9:offset+21]), "\x00")
		trType := int((data[offset+21] >> 7) & 0x01)
		preEmphasis := (data[offset+21] & 0x40) != 0
		numIndexPoints := int(data[offset+35])
		offset += 36

		// Track 170 (CD-DA lead-out) or 255 (non-CD lead-out)
		if trNum == 170 || trNum == 255 {
			cs.LeadOutOffset = trOffset
			continue
		}

		tr := CueTrack{
			Number:      trNum,
			Offset:      trOffset,
			ISRC:        isrc,
			Type:        trType,
			PreEmphasis: preEmphasis,
		}

		for j := 0; j < numIndexPoints; j++ {
			if offset+12 > len(data) {
				return nil, fmt.Errorf("unexpected EOF reading index point %d in track %d", j, i)
			}
			idxOffset := binary.BigEndian.Uint64(data[offset : offset+8])
			idxNum := int(data[offset+8])
			offset += 12

			tr.Indexes = append(tr.Indexes, CueIndex{
				Number: idxNum,
				Offset: idxOffset,
			})
		}

		cs.Tracks = append(cs.Tracks, tr)
	}

	return cs, nil
}
