# music-ops

`music-ops` turns a folder of downloaded music into library-ready `.m4a` files in one unattended run: it converts every track to AAC, cleans titles and featured artists, classifies genres with Last.fm and a local AI advisor, numbers tracks, renames files, skips duplicates already in your library and writes a report of everything it did. Source files are never modified.

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

6. Check everything:

   ```
   music-ops doctor
   ```

## Usage

| Command | What it does |
|---|---|
| `music-ops` | Asks for a folder and imports it into the configured library. |
| `music-ops run <folder> --out <dir>` | Imports `<folder>` into `<dir>`. |
| `music-ops preview <folder>` | Prints the plan (names, tags, duplicates) without writing anything. |
| `music-ops watch <inbox>` | Imports every folder that lands in `<inbox>`. |
| `music-ops config set output.dir <dir>` | Sets the default library folder. |
| `music-ops config show` | Prints the effective configuration. |
| `music-ops version` | Prints the version, ffmpeg and the active encoder. |
| `music-ops doctor` | Checks ffmpeg, the encoder, Last.fm, the advisor and the output folder. |

Any setting can be overridden for one run with `--set key=value` (shortcuts: `--out`, `--layout`) or with an environment variable named after the key (`MUSIC_OPS_LASTFM_API_KEY` for `lastfm.api_key`). `--verbose` prints detailed logs and `--no-open` skips opening the report.

## Output rules

- Format: AAC-LC in `.m4a`, 44.1 kHz, CBR 256 kb/s with FDK-AAC (512 kb/s with the native fallback); AAC sources are copied without re-encoding; the source cover is kept.
- Names: `{AlbumArtist} - {Title}.m4a`, or `{AlbumArtist} - {Title} (feat. {Guests}).m4a` when there are guests; invalid characters are removed and long names are shortened.
- Tags: the title keeps version descriptors such as `(Remix)` or `[Radio Edit]` and drops `feat.` parts; artists are written as `Main; Guest`, the album artist is the main artist.
- Genres: a main genre and an optional sub-genre from a fixed list, separated by `; ` (for example `Electronic; Dance`, `Rap Nacional`); `desconhecido` when nothing reliable is found.
- Track numbers: numbered per source folder as `n/total`.
- Duplicates: the best-quality copy is kept; tracks already in the library are skipped and the library file is left untouched.
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
