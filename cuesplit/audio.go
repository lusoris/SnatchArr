// SPDX-FileCopyrightText: 2026 lusoris
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

type AudioInfo struct {
	Path          string
	SampleRate    int
	BitsPerSample int
	TotalSamples  uint64
	Channels      int
}

type ffprobeStream struct {
	SampleRate       string `json:"sample_rate"`
	BitsPerRawSample string `json:"bits_per_raw_sample"`
	SampleFmt        string `json:"sample_fmt"`
	DurationTs       int64  `json:"duration_ts"`
	NbSamples        string `json:"nb_samples"`
	Channels         int    `json:"channels"`
}

type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
}

func ProbeAudio(ctx context.Context, path string) (*AudioInfo, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=sample_rate,bits_per_raw_sample,sample_fmt,duration_ts,nb_samples,channels",
		"-of", "json",
		path,
	)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed on %s: %w", path, err)
	}

	var parsed ffprobeOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	if len(parsed.Streams) == 0 {
		return nil, fmt.Errorf("no audio streams found in %s", path)
	}

	stream := parsed.Streams[0]

	// Sample Rate
	rate, err := strconv.Atoi(strings.TrimSpace(stream.SampleRate))
	if err != nil || rate <= 0 {
		return nil, Refusal("invalid sample rate", "sample rate %q is invalid", stream.SampleRate)
	}
	if rate > 192000 {
		return nil, Refusal("refuse rates above 192000", "sample rate %d exceeds 192000 Hz limit", rate)
	}
	if rate%75 != 0 {
		return nil, Refusal("refuse rates not divisible by 75", "sample rate %d is not divisible by 75", rate)
	}

	// Channels
	channels := stream.Channels
	if channels <= 0 {
		channels = 2
	}

	// Bits per sample
	var bits int
	if stream.BitsPerRawSample != "" && stream.BitsPerRawSample != "N/A" && stream.BitsPerRawSample != "0" {
		if b, err := strconv.Atoi(stream.BitsPerRawSample); err == nil && b > 0 {
			bits = b
		}
	}
	if bits == 0 {
		// Fall back to sample_fmt
		fmtStr := strings.ToLower(stream.SampleFmt)
		switch {
		case strings.HasPrefix(fmtStr, "s16"):
			bits = 16
		case strings.HasPrefix(fmtStr, "s24"):
			bits = 24
		case strings.HasPrefix(fmtStr, "s32"):
			bits = 32
		default:
			return nil, Refusal("unsupported sample format", "unrecognized sample format %q", stream.SampleFmt)
		}
	}

	if bits == 32 {
		return nil, Refusal("32-bit", "32-bit audio is not supported")
	}
	if bits != 16 && bits != 24 {
		return nil, Refusal("unsupported bit depth", "bit depth %d is not supported (only 16-bit and 24-bit are accepted)", bits)
	}

	// Total Samples
	var totalSamples uint64
	if stream.DurationTs > 0 {
		totalSamples = uint64(stream.DurationTs)
	} else if stream.NbSamples != "" && stream.NbSamples != "N/A" {
		if s, err := strconv.ParseUint(stream.NbSamples, 10, 64); err == nil && s > 0 {
			totalSamples = s
		}
	}

	if totalSamples == 0 {
		// Decode-count
		cnt, err := decodeCountSamples(ctx, path, channels)
		if err != nil {
			return nil, fmt.Errorf("failed to count samples by decoding: %w", err)
		}
		totalSamples = cnt
	}

	return &AudioInfo{
		Path:          path,
		SampleRate:    rate,
		BitsPerSample: bits,
		TotalSamples:  totalSamples,
		Channels:      channels,
	}, nil
}

func decodeCountSamples(ctx context.Context, path string, channels int) (uint64, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-nostdin",
		"-loglevel", "error",
		"-i", path,
		"-map", "0:a:0",
		"-f", "s32le",
		"-c:a", "pcm_s32le",
		"-",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}

	var byteCount uint64
	buf := make([]byte, 64*1024)
	for {
		n, err := stdout.Read(buf)
		if n > 0 {
			byteCount += uint64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
	}

	if err := cmd.Wait(); err != nil {
		return 0, err
	}

	bytesPerSample := uint64(channels * 4) // 32-bit (4 bytes) per channel
	if bytesPerSample == 0 {
		return 0, fmt.Errorf("invalid channel count %d", channels)
	}

	return byteCount / bytesPerSample, nil
}

// ComputePCM_MD5 computes the MD5 hash of decoded PCM audio from ffmpeg.
func ComputePCM_MD5(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-nostdin",
		"-loglevel", "error",
		"-i", path,
		"-map", "0:a:0",
		"-f", "s32le",
		"-c:a", "pcm_s32le",
		"-",
	)

	hasher := md5.New()
	cmd.Stdout = hasher

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to decode PCM for %s: %w", path, err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// ComputeConcatenatedPCM_MD5 computes the MD5 hash of multiple decoded PCM audio streams concatenated.
func ComputeConcatenatedPCM_MD5(ctx context.Context, paths []string) (string, error) {
	hasher := md5.New()

	for _, p := range paths {
		cmd := exec.CommandContext(ctx, "ffmpeg",
			"-nostdin",
			"-loglevel", "error",
			"-i", p,
			"-map", "0:a:0",
			"-f", "s32le",
			"-c:a", "pcm_s32le",
			"-",
		)
		cmd.Stdout = hasher
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("failed to decode PCM for %s: %w", p, err)
		}
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}
