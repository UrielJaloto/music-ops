# music-ops

`music-ops` turns a folder of downloaded music into library-ready `.m4a` files in one unattended run: it converts every track to AAC, cleans titles and featured artists, classifies genres with Last.fm and a local AI advisor, numbers tracks, renames files, skips duplicates already in your library and writes a report of everything it did. Source files are never modified.

## Requirements

- Windows 10 or 11, x64.
- ffmpeg on `PATH`.
- An NVIDIA GPU with 8 GB of VRAM for the local advisor (it also runs on CPU, much slower).
- A free Last.fm API key.

## How it works

1. **Discover** every audio file in the folder (`.flac`, `.wav`, `.mp3`, `.m4a`, ...); other files are ignored.
2. **Convert** each track to AAC in a staging folder inside the library (`.music-ops\staging`).
3. **Resolve** titles, featured artists and genres: Last.fm tags first, then the local advisor; every answer is validated and falls back to deterministic rules when it is rejected.
4. **Publish** the files into the library with their final names and tags, skipping duplicates; the run is journaled, so an interrupted run resumes without duplicates.

A batch of 20 FLAC tracks (about 1 GB) takes about 70 seconds on an 8 GB NVIDIA GPU, advisor startup included; a second run of the same folder writes nothing and finishes in under a second.

## Install

1. Download `music-ops.exe` and `checksums.txt` from the [latest release](https://github.com/UrielJaloto/music-ops/releases/latest) and put the exe in a folder on your `PATH`.
2. Install ffmpeg:

   ```
   winget install -e --id Gyan.FFmpeg
   ```

3. For the best encoder, install [fre:ac](https://www.freac.org/) (or place any `FDK-AAC.dll`) and point music-ops to it when it is not at the default `C:\Program Files\freac\codecs\FDK-AAC.dll`:

   ```
   music-ops config set encoder.fdk_library "C:\path\to\FDK-AAC.dll"
   ```

   Without it, ffmpeg's native AAC encoder is used.
4. Install the local advisor (llama.cpp and a Qwen model, about 6 GB; an NVIDIA GPU is recommended):

   ```
   music-ops advisor install
   ```

5. Set your [Last.fm API key](https://www.last.fm/api/account/create):

   ```
   music-ops config set lastfm.api_key <key>
   ```

6. Choose your library folder (the default is the author's own path):

   ```
   music-ops config set output.dir "D:\Music\Library"
   ```

7. Check everything:

   ```
   music-ops status
   ```

## Usage

| Command | What it does |
|---|---|
| `music-ops` | Asks for a folder and imports it into the configured library. |
| `music-ops run <folder> --out <dir>` | Imports `<folder>` into `<dir>`. |
| `music-ops preview <folder>` | Prints the plan (names, tags, duplicates) without writing anything. |
| `music-ops watch <inbox>` | Imports every folder that lands in `<inbox>`, then moves it to `<inbox>\_processed` or `<inbox>\_failed`. |
| `music-ops config set output.dir <dir>` | Sets the default library folder. |
| `music-ops config show` | Prints the effective configuration. |
| `music-ops version` | Prints the version, ffmpeg and the active encoder. |
| `music-ops status` | Checks ffmpeg, the encoder, Last.fm, the advisor and the output folder. |

Any setting can be overridden for one run with `--set key=value` (shortcuts: `--out`, `--layout`) or with an environment variable named after the key (`MUSIC_OPS_LASTFM_API_KEY` for `lastfm.api_key`). `--verbose` prints detailed logs and `--no-open` skips opening the report.

Exit codes: `0` every track published or skipped, `2` some tracks failed, `130` cancelled with Ctrl+C, `1` the run could not start (see the message).

## Output rules

- Format: AAC-LC in `.m4a`, 44.1 kHz, CBR 256 kb/s with FDK-AAC (512 kb/s with the native fallback); AAC sources are copied without re-encoding; the source cover is kept.
- Folder: each run goes into a folder named after the input folder, for example `music-ops run "D:\Downloads\Rockzin Brabo"` writes `<library>\Rockzin Brabo\...`.
- Names: `{AlbumArtist} - {Title}.m4a`, or `{AlbumArtist} - {Title} (feat. {Guests}).m4a` when there are guests; invalid characters are removed and long names are shortened.
- Tags: the title keeps version descriptors such as `(Remix)` or `[Radio Edit]` and drops `feat.` parts; artists are written as `Main; Guest`, the album artist is the main artist.
- Genres: a main genre and an optional sub-genre from a fixed list, separated by `; ` (for example `Electronic; Dance`, `Rap Nacional`); `desconhecido` when nothing reliable is found.
- Track numbers: numbered per source folder as `n/total`.
- Duplicates: the best-quality copy is kept; tracks already anywhere in the library (same file name) are skipped and the library file is left untouched.
- Every run writes a report under `<library>\.music-ops\runs\<run id>\`.

## Testing

```
./test.ps1 -All
```

`./test.ps1 -Live` runs a short import against the real Last.fm and the installed advisor; it exits with code 3 when `MUSIC_OPS_LASTFM_API_KEY` or the advisor is missing.

## Building

```
./build.ps1 -Version 0.1.0
```

It writes `dist/music-ops.exe` and `dist/checksums.txt`. Pushing a tag `v*` builds and publishes a release.
