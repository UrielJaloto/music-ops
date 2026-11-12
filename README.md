# music-ops

Turns a folder of downloaded music into library-ready `.m4a` files in one unattended run.

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![Platform](https://img.shields.io/badge/platform-Windows%20x64-0078D6)
![Coverage](https://img.shields.io/badge/coverage-99.9%25-brightgreen)
![License](https://img.shields.io/badge/license-Apache%202.0-blue)

`music-ops` converts every track to AAC, cleans titles and featured artists, classifies genres with Last.fm and a local language model, numbers tracks, renames files, skips duplicates already in the library and writes a report of everything it did. Source files are never modified.

## Highlights

- **One command per album folder.** Discover, convert, resolve metadata and publish run as one pipeline, with conversion and metadata resolution in parallel.
- **Deterministic output.** Every rule (codec, bitrate, title cleanup, artist order, genre format, file name) is a numbered rule with a test.
- **The model is advisory.** Each answer from the local model is validated against the source tags; a rejected answer falls back to rule-based results.
- **Resumable.** Each run is journaled in bbolt. A run killed halfway resumes without duplicates, and a second run of the same folder writes nothing.
- **Measured.** 20 FLAC tracks (about 1 GB) take about 70 seconds on an 8 GB NVIDIA GPU, model startup included. A repeated run finishes in under a second with zero network or GPU calls.

## Contents

- [How it works](#how-it-works)
- [Output rules](#output-rules)
- [Install](#install)
- [Usage](#usage)
- [Configuration](#configuration)
- [Architecture](#architecture)
- [Testing](#testing)
- [Design decisions](#design-decisions)
- [Building and releases](#building-and-releases)
- [License](#license)

## How it works

```
folder ─> discover ─┬─> convert ──┬─> publish ─> library + report
                    └─> resolve ──┘
```

1. **Discover** every audio file in the folder (`.flac`, `.wav`, `.aiff`, `.ape`, `.wv`, `.m4a`, `.mp3`, `.ogg`, `.opus`, `.wma`); other files are listed as skipped.
2. **Convert** each track to AAC in a staging folder under `%LOCALAPPDATA%\music-ops\staging`. The library only receives finished tracks.
3. **Resolve** titles, featured artists and genres: Last.fm tags first, then the local model. Every answer is validated and falls back to rule-based results when it is rejected.
4. **Publish** each track into the library with its final name and tags, skipping duplicates, and record it in the journal.

The terminal shows one progress line per stage with a live counter and elapsed time:

```
[1/4] Discovering  ██████████  42/42  00:03
[2/4] Converting   ██████░░░░  25/42  00:41
```

## Output rules

| Area | Rule |
|---|---|
| Audio | AAC-LC in `.m4a`, 44.1 kHz, CBR 256 kb/s with FDK-AAC (512 kb/s with the ffmpeg native encoder); AAC sources at 44.1 kHz are copied without re-encoding; the source cover is kept |
| Title | Version descriptors such as `(Remix)` or `[Radio Edit]` stay; `feat.` clauses move to the artist list; an empty title tag falls back to the file name |
| Artists | `Main; Guest; Guest`, main artist first, no repeats; band names such as `AC/DC` or `Simon & Garfunkel` are never split |
| Album artist | The main artist |
| Genre | A main genre and an optional sub-genre from the genre list ([built-in or your own](#custom-genres)), for example `Rock; Metal`, `Electronic; Dance`, `Rap Nacional`; `desconhecido` when nothing reliable is found |
| Track number | Numbered per source folder as `n/total` |
| File name | `{AlbumArtist} - {Title}.m4a`, or `{AlbumArtist} - {Title} (feat. {Guests}).m4a`; invalid characters removed, long names shortened, Windows reserved names escaped |
| Location | `<library>\<input folder name>\<file>`; paths longer than 260 characters are supported |
| Duplicates | Within a run the best-quality copy is kept; a file name already anywhere in the library is skipped and the library file is left untouched |

Examples:

| Source title | Source artist | Title | Artist |
|---|---|---|---|
| `HEATSEEKER (feat. Ethan Ross)` | `$werve` | `HEATSEEKER` | `$werve; Ethan Ross` |
| `Destination Calabria (feat. Crystal Waters) [Radio Edit]` | `Alex Gaudino` | `Destination Calabria [Radio Edit]` | `Alex Gaudino; Crystal Waters` |
| `Song (with A & B)` | `X` | `Song` | `X; A; B` |
| `Back In Black` | `AC/DC` | `Back In Black` | `AC/DC` |

## Install

Requirements: Windows 10 or 11 (x64), ffmpeg, a free Last.fm API key, and for the local model an NVIDIA GPU with 8 GB of VRAM (it also runs on CPU, more slowly).

1. Download `music-ops-windows-amd64.exe` and `checksums.txt` from the [latest release](https://github.com/UrielJaloto/music-ops/releases/latest), rename the executable to `music-ops.exe` and put it in a folder on your `PATH`.
2. Install ffmpeg:

   ```powershell
   winget install -e --id Gyan.FFmpeg
   ```

3. Optional, for the best encoder: install [fre:ac](https://www.freac.org/). `music-ops` finds its `FDK-AAC.dll` under Program Files. Any other FDK-AAC library can be configured:

   ```powershell
   music-ops config set encoder.fdk_library "C:\path\to\FDK-AAC.dll"
   ```

4. Install the local model runtime (llama.cpp and a Qwen GGUF model, about 6 GB):

   ```powershell
   music-ops advisor install
   ```

5. Set the [Last.fm API key](https://www.last.fm/api/account/create) and the library folder (the default is your Music folder):

   ```powershell
   music-ops config set lastfm.api_key <key>
   music-ops config set output.dir "D:\Music\Library"
   ```

6. Check the setup:

   ```powershell
   music-ops status
   ```

## Usage

| Command | What it does |
|---|---|
| `music-ops` | Asks for a folder and imports it into the configured library |
| `music-ops run <folder> [--out <dir>]` | Imports `<folder>` |
| `music-ops preview <folder>` | Prints the plan (names, tags, duplicates) without writing anything |
| `music-ops watch <inbox>` | Imports every folder that lands in `<inbox>`, then moves it to `_processed` or `_failed` |
| `music-ops status` | Checks ffmpeg, the encoder, Last.fm, the local model and the output folder |
| `music-ops config show` | Prints every setting with its effective value |
| `music-ops version` | Prints the version, the ffmpeg build and the active encoder |

Flags for `run`, `preview` and `watch`:

| Flag | Effect |
|---|---|
| `--tag key=value` | Writes the same tag on every track; keys `album`, `genre`, `year`. A fixed genre must come from the genre list and skips genre classification |
| `--set key=value` | Overrides one setting for this run (shortcuts `--out`, `--layout`) |
| `--open` | Opens the report and the output folder when the run ends |
| `--verbose` | Prints detailed logs |

Example:

```powershell
music-ops run "D:\Downloads\Hybrid Theory" --tag album="Hybrid Theory" --tag genre="Rock; Metal" --tag year=2000
```

Exit codes: `0` every track published or skipped, `2` some tracks failed, `130` cancelled with Ctrl+C, `1` the run could not start.

Every run writes `report.md` and `report.json` under `%LOCALAPPDATA%\music-ops\runs\<run id>\`, with one row per track, durations per stage and warnings.

## Configuration

Settings live in one file per user, `%APPDATA%\music-ops\config.yaml`, created by `music-ops config set <key> <value>`. Defaults are named constants in [config.go](internal/infrastructure/config/config.go).

```yaml
output:
  dir: ""                            # default: the user's Music folder
  layout: flat                       # flat | album_folders
  album_template: "{albumartist} - {album}"
encoder:
  fdk_library: ""                    # default: discovered
  workers: 0                         # 0 = CPU count - 1
lastfm:
  api_key: ""
advisor:
  port: 8089
  batch_size: 40
numbering:
  mode: per_directory                # per_directory | keep
watch:
  settle: 20s
genres:
  file: ""                           # default: the built-in genre list
```

### Custom genres

The built-in list is the one in [Output rules](#output-rules). To use your own, write a YAML file and point `genres.file` to it:

```yaml
- name: Rock
  subgenres: [Metal, Punk, Grunge]
- name: Lo-Fi
  hint: chill beats, study music
- name: Sertanejo
  subgenres: [Universitário]
  national_only: true
```

```powershell
music-ops config set genres.file "D:\Music\genres.yaml"
```

The file replaces the whole list. `hint` is extra context for the local model, and `national_only` marks a genre that never gets the `Nacional` suffix. Genre names are case-insensitive in `--tag`, must be unique and cannot contain `;`. Last.fm tags that match a genre name are used directly; the model only answers with genres from the file.

## Architecture

The code is split in four layers under `internal`: `domain` holds the rules, `services` the use cases, `presentation/cli` the command line and `infrastructure` the adapters. Dependencies point inward only, and `internal/test/architecture_test.go` checks it with `go/parser`.

| Path | Contents |
|---|---|
| `cmd/music-ops` | `main.go`, which only builds and connects the objects |
| `internal/domain` | Business rules with no dependencies: credits, audio, file names, genres, tracks, albums, library, runs |
| `internal/services` | Use cases; each declares the interfaces it consumes |
| `internal/presentation/cli` | Command router, controllers, presenters, Markdown and JSON reports |
| `internal/infrastructure` | Adapters for ffmpeg, FDK-AAC, bbolt, Last.fm, llama-server, the file system and the desktop |
| `internal/test` | Architecture test and end-to-end suite |

**Platforms.** Releases target Windows x64. The code builds for Linux and macOS (`GOOS=linux go build ./...`). OS-specific code lives only in infrastructure, in `<job>_windows.go` and `<job>_other.go` pairs next to the code that owns the job: hidden-file detection, free disk space, cross-device moves, child process windows, long paths, the FDK-AAC library, tool discovery, the folder picker and opening files. On other systems the FDK library, the native folder dialog and the pinned model download report an explicit "not supported" error and the program falls back where it can.

## Testing

Tests use only `go test` and the standard library.

| Suite | What it proves | Command |
|---|---|---|
| Unit | Every rule of the domain and every service path, with stubs for the interfaces each service declares | `go test ./...` |
| Integration | The ffmpeg, FDK-AAC, bbolt and file system adapters against real tools and generated audio | `go test -tags integration ./...` |
| End-to-end | The built binary importing generated audio: output files and tags, duplicates, tag overrides, a custom genre list, a killed run that resumes, Ctrl+Break cancellation | `go test -tags e2e ./internal/test/e2e/` |
| Live | A short import against the real Last.fm API and the local model | `go test -tags live ./internal/test/e2e/` |
| Architecture | Layer and dependency rules, one owner per external driver, repository layout | part of `go test ./...` |

Merged statement coverage of the unit, integration and end-to-end runs is 99.9 % (`-coverpkg=./...` plus the e2e binary built with `-cover`, merged with `go tool covdata`).

## Design decisions

| Decision | Reason |
|---|---|
| FDK-AAC called through `syscall`, no cgo | Best AAC quality at 256 kb/s while keeping a single static binary |
| ffmpeg as an external process | Decoding, resampling, tagging and cover art for every input format without linking codecs |
| Local model through llama-server with JSON-schema output | Private, free to run, and its answers can be parsed and validated |
| Model answers validated against the source | Every artist must appear in the source tags or path, so the model cannot invent names |
| bbolt for cache and journal | Embedded, transactional, one file; makes runs resumable and repeat runs free |
| Staging next to the library, then rename | Finished files appear atomically and the library never holds partial output |
| Interfaces declared by the consumer | Services stay testable with small stubs and never depend on infrastructure |

Tech stack: Go 1.26, ffmpeg and ffprobe, FDK-AAC, llama.cpp with a Qwen GGUF model, Last.fm API, [bbolt](https://github.com/etcd-io/bbolt), [go-mp4](https://github.com/abema/go-mp4), [fsnotify](https://github.com/fsnotify/fsnotify), [sqweek/dialog](https://github.com/sqweek/dialog), [x/sync](https://pkg.go.dev/golang.org/x/sync).

## Building and releases

```powershell
go build -ldflags "-X main.version=1.0.0" ./cmd/music-ops
```

Pushing a `v*` tag runs [release.yml](.github/workflows/release.yml), which builds one binary per target in its matrix and publishes them with `checksums.txt` as a GitHub release.

## License

[Apache License 2.0](LICENSE)
