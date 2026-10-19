# cuesplit

`cuesplit` is a strict, lossless CUE sheet and audio image splitter in Go, designed for frame-accurate extraction and bit-exact PCM preservation.

## Origin and Attribution

`cuesplit` is a fork of [unflac 1.4](https://git.sr.ht/~ft/unflac) (tag `1.4`, commit `7a0b8cac9bf313aee4dc5955e4741ced8e631ad5`), originally written by Sigrid Solveig Haflínudóttir under the MIT license.

## What Changed and Why

1. **Independent CUE Parser (`cuesplit/cuesheet`)**
   - Replaces the GPL-3.0 dependency (`git.sr.ht/~ft/cue`) with a cleanroom, MIT-licensed CUE parser in `cauda.dev/cuesplit/cuesheet`.
   - Strictly enforces CUE specifications and refuses malformed input:
     - Refuses multi-FILE cue sheets (`"multi-FILE cue sheets are not supported"`).
     - Refuses non-AUDIO tracks, invalid or non-increasing track numbers, and missing `INDEX 01`.
     - Refuses non-increasing index times within tracks, malformed timestamps (`ff >= 75`, `ss >= 60`), and out-of-order track indices.
     - Refuses `PREGAP` and `POSTGAP` commands (silence not present in the image is not synthesised).
     - Refuses unknown commands, naming the offending line number.
   - Robust charset handling: strips UTF-8 BOM, detects encoding using `gochardet` (confidence >= 25), and decodes to UTF-8.

2. **Native FLAC Metadata Parser (`cuesplit/flacmeta`)**
   - Reads FLAC metadata natively in Go without shelling out to `metaflac`.
   - Supports text cuesheets in Vorbis comments (`CUESHEET=`) as well as FLAC binary `CUESHEET` metadata blocks (block type 5; lead-out track 170/255, sample offsets).
   - In block mode, track titles remain empty and album tags (`ARTIST`, `ALBUM`, `DATE`, `GENRE`) are derived from Vorbis comments.

3. **Strict Input Discovery**
   - Directory input requires exactly one cuesheet source: either one `.cue` file or one `.flac` file with an embedded cuesheet.
   - Refuses directories with multiple cue sheets (`"several cue sheets"`) or multiple embedded cuesheets.
   - Refuses directories that already contain multiple track audio files (`"already split"`).

4. **HTOA Preservation & Exact Sample Coverage**
   - Track 1 always starts at sample 0. Non-zero `INDEX 01` on Track 1 (Hidden Track One Audio / pregap) is preserved in Track 1 rather than discarded as in unflac 1.4.
   - Inter-track pregap goes to the preceding track: Track $n$ ends where Track $n+1$ begins (`INDEX 00` if present, else `INDEX 01`).
   - The entire image sample range $[0, \text{total})$ is covered with no gaps and no overlap; verified by assertions.

5. **Audio Fidelity & Resampling Prohibition**
   - Strictly lossless bit depth preservation: 16-bit (`-sample_fmt s16`) and 24-bit (`-sample_fmt s32 -bits_per_raw_sample 24`).
   - 32-bit audio is refused.
   - Resampling is completely disabled: `-ar` is never passed. Sample rates above 192000 Hz or rates not divisible by 75 (required for exact CD frame alignment) are strictly refused instead of lossily downsampled.

6. **Atomic Staging & Verification**
   - All extraction occurs inside a temporary staging directory (`<out>/.cuesplit-staging-<random>`).
   - Pre-flight check refuses execution before encoding if the destination album directory already exists (`"target exists"`).
   - Verification suite before moving files:
     - `flac -t --silent` stream integrity check on every output track.
     - Bit depth, sample rate, and sample count checks against input audio.
     - Bit-exact PCM MD5 verification: MD5 of decoded input PCM (`pcm_s32le`) must match MD5 of concatenated output PCMs.
   - Only upon complete verification is the directory atomically renamed to `<out>/<album dir>`.
   - On error, refusal, or `SIGINT`/`SIGTERM`, the staging directory is completely removed, ensuring nothing partial is left under `--out`.

7. **CLI and Output**
   - Subcommand-based CLI: `cuesplit split --out <dir> [--json] <input>`.
   - Exit codes:
     - `0`: split completed and verified.
     - `2`: input refused (malformed or unsupported rule violated; error names the rule).
     - `1`: other runtime errors.
   - Machine-readable `--json` output reporting input path, cue source (`file`, `flac-vorbis-cuesheet`, `flac-cuesheet-block`), sample rate, bit depth, total samples, per-track sample boundaries and paths, input/output PCM MD5s, and verification status.
   - Adds `albumartist` metadata tag matching sheet `PERFORMER` when present.
   - Sequential track processing without worker pool concurrency.

## Usage

```bash
# Split an album directory into output directory
cuesplit split --out /path/to/output /path/to/album/

# Split from a standalone .cue file with JSON output
cuesplit split --out /path/to/output --json /path/to/album/album.cue

# Split a FLAC file containing an embedded cuesheet
cuesplit split --out /path/to/output /path/to/album/image.flac
```

## Licence

MIT License. See [LICENSES/MIT.txt](LICENSES/MIT.txt).
Non-code assets are annotated in [REUSE.toml](REUSE.toml).
