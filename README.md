# Bing Play

English | [简体中文](README.zh-CN.md)

Bing Play is a cross-platform desktop BitTorrent client that combines a DHT resource crawler, a download manager, and a built-in streaming video player. It is built with Wails v3 (Go + Vue 3) and runs as a single native application on Windows, macOS, and Linux.

The project is local-first: resources discovered by the crawler, download tasks, settings and playback history are stored in a local SQLite database. No account, cloud service or external backend is required.

## Project Introduction

### What it is

Bing Play ("兵 Play") is a personal media toolbox for peer-to-peer content:

- It connects to the BitTorrent DHT network on startup and continuously discovers torrent metadata (name, InfoHash, size).
- Discovered resources are listed in a searchable, sortable resource browser and can be queued for download individually or in batches.
- Downloads run in the background with priority scheduling, automatic retries, stall detection and restart recovery.
- Downloaded or local video files can be played in an embedded ArtPlayer, including streaming directly from a torrent while it is still downloading.

### Design goals

- **Local-first**: everything runs on the user's machine; the only network traffic is BitTorrent/DHT and optional update checks.
- **One binary**: the frontend is embedded into the Go binary via `go:embed`; runtime assets (locales, migrations, ffmpeg) ship in a sibling `resources` directory.
- **Explicit lifecycle**: every background service (HTTP server, DHT crawler, download engine) is wired through Fx lifecycle hooks and shut down in a deterministic order.
- **Typed IPC over HTTP only for streaming**: business calls go through Wails bindings; the local HTTP server exists solely to serve video bytes with `Range` support.
- **Bilingual by design**: every user-facing string comes from backend locale files (`en`, `zh-CN`, `zh-TW`), including player UI text.

### Who it is for

- Users who want a lightweight desktop torrent client with integrated playback and no browser plugins.
- Developers looking for a reference Wails v3 application that combines Fx dependency injection, GORM/SQLite, `anacrolix/torrent`, a DHT crawler and FFmpeg-based transcoding.

> Current scope note: the "Find" (`/find`) and "Library" (`/library`) pages are navigation placeholders in this version. The implemented pages are Play, Download, Resources and Settings.

## Tech Stack

### Backend (Go 1.26)

| Area | Technology | Version | Purpose |
|---|---|---|---|
| Desktop shell | [Wails v3](https://v3.wails.io/) | `v3.0.0-beta.16` | Native window, bindings, events, updater |
| Dependency injection | `go.uber.org/fx` | `v1.24.0` | Module wiring and lifecycle management |
| ORM | `gorm.io/gorm` | `v1.31.2` | Data access |
| SQLite driver | `modernc.org/sqlite` | `v1.57.0` | Pure-Go SQLite (no CGO) |
| SQLite helper | `github.com/libtnb/sqlite` | `v1.2.2` | Dialector integration |
| Migrations | `golang-migrate/migrate/v4` | `v4.19.1` | File-based migrations in `resources/migrations` |
| BitTorrent engine | `github.com/anacrolix/torrent` | `v1.61.1-0.20261024051421-5f95cd33fd29` | Download, seeding, streaming readers |
| DHT | `github.com/anacrolix/dht/v2` | `v2.24.0` | DHT client for the crawler |
| DHT crawler | `github.com/DMzmwh/bing-dhtcrawler` | `v0.2.1` | Metadata crawling |
| Deduplication | `github.com/tylertreat/BoomFilters` | `v0.0.0-20251117164519` | Bloom filter for InfoHash dedupe |
| i18n | `github.com/nicksnyder/go-i18n/v2` | `v2.6.1` | JSON locale bundles |
| Logging | `log/slog` + `gopkg.in/natefinch/lumberjack.v2` | `v2.2.1` | Structured, rotated file logs |
| Media tooling | FFmpeg / FFprobe | bundled per platform | Probing and transcoding |

### Frontend

| Technology | Version | Purpose |
|---|---|---|
| Vue | `3.5.41` | UI framework (Composition API) |
| TypeScript | `5.7.3` | Type safety (checked by `vue-tsc`) |
| Naive UI | `2.45.3` | Component library, theming |
| Vue Router | `4.5.1` | Page routing (`/play`, `/download`, `/resources`, `/find`, `/library`, `/settings`) |
| ArtPlayer | `5.4.0` | Video player |
| Vite | `6.4.1` | Dev server and production build |
| `@wailsio/runtime` | latest | Bindings, events, updater events |
| `@vicons/ionicons5`, `vfonts` | `^0.13.0`, `0.0.3` | Icon set and fonts |
| `sass-embedded` | `^1.103.1` | SCSS support |

### Build tooling

- Wails v3 CLI (`wails3`) with Taskfile definitions in `Taskfile.yml` and `build/**/Taskfile.yml`.
- npm for frontend dependencies.
- Platform packaging: NSIS (Windows), DMG (macOS), AppImage / deb / rpm / Arch (Linux).

## Quick Start

### Requirements

- Go 1.26.0 or newer (`go.mod` declares `go 1.26.0`).
- Node.js 20 LTS or newer with npm (Vite 6 requires Node 18+).
- Wails v3 CLI. Install a beta version compatible with the library:
  ```bash
  go install github.com/wailsapp/wails/v3/cmd/wails3@latest
  wails3 version
  ```
- Platform prerequisites for Wails v3:
  - Windows 10/11 with the WebView2 runtime (preinstalled on current Windows).
  - macOS with Xcode Command Line Tools.
  - Linux with GTK 3 and WebKitGTK (`webkit2gtk-4.1`); refer to the Wails documentation for your distribution.
- FFmpeg (optional but recommended): place `ffmpeg`/`ffprobe` in `resources/bin/ffmpeg/<goos>-<goarch>/` (for example `windows-amd64`). If missing, the app still runs but transcoding is unavailable. FFmpeg found on `PATH` is used as a fallback.

### Install and run

```bash
# 1. Install frontend dependencies
npm --prefix frontend ci

# 2. Generate Wails bindings (also done automatically by build tasks)
wails3 task common:generate:bindings

# 3. Run in development mode (frontend hot reload + backend rebuild)
wails3 task dev
```

Development mode starts the Vite dev server on **http://localhost:9245** and opens the Wails window. The Go HTTP server (video streaming) listens on **http://127.0.0.1:9090**.

### Production build

```bash
wails3 task build     # binary in ./bin/bing-play (bing-play.exe on Windows)
wails3 task package   # platform installer (NSIS / DMG / AppImage / deb / rpm)
```

`wails3 task build` runs `copy-resources` first, so locales, migrations and FFmpeg binaries are available at runtime. When invoking a platform task directly (for example `wails3 task darwin:build`), run `wails3 task copy-resources` beforehand.

### Tests and checks

```bash
go test ./internal/...        # backend unit tests
go build .                    # compile the desktop entry point
npm --prefix frontend run build   # vue-tsc + production frontend build
```

## Features

### DHT resource discovery

- Crawler starts automatically with the application (toggleable, default `enabled = true`).
- Listens on UDP port `51414` by default (valid range `1024–65535`), kept separate from the torrent engine's own ports.
- Fetches torrent metadata with a 10-second timeout and deduplicates InfoHashes with a Bloom filter (capacity 1,000,000, false-positive rate 1%).
- Persists discovered resources into SQLite and keeps a DHT node snapshot (`nodes.json`) under `resources/crawler/`.
- Settings for the crawler (enabled, port) take effect immediately without restarting the app.
- Shutdown closes both the DHT client and the underlying UDP socket synchronously so the port is released right away.

### Resource browser

- Paginated listing of crawled torrents (page size `50/100/200`, saved in-place).
- Keyword search over resource name and InfoHash.
- Sorting by newest, oldest, largest, smallest.
- Status badges derived from the download/local state machine.
- Copy magnet link, single download, and batch download with an added/skipped/failed summary.

### Download manager

- Background queue with priority scheduling and configurable concurrency.
- Default runtime parameters:

| Parameter | Default | Meaning |
|---|---|---|
| `MaxConcurrent` | 5 | Tasks downloading at the same time |
| `MaxRetries` | 3 | Automatic retries before marking a task failed |
| `MetadataTimeout` | 10 minutes | Maximum wait for torrent metadata |
| `RetryDelay` | 30 seconds | Delay before an automatic retry |
| `ProgressInterval` | 1 second | Progress sampling and event push interval |
| `PersistInterval` | 10 seconds | Progress persistence interval |
| `StallTimeout` | 3 minutes | No-new-bytes threshold that triggers a retry |

- Two orthogonal state axes (task lifecycle × local file availability) with a derived display state; see [docs/resource_state_machine.md](docs/resource_state_machine.md).
- Operations: pause, resume, retry, cancel, repair missing files, raise/lower priority, open download directory.
- Unfinished tasks are recovered on startup.
- UI updates are event-driven: `download:progress` (about once per second) and `download:state-changed`; a silent 5-second `ListActive` poll acts as a fallback.

### Streaming and playback

- Embedded ArtPlayer with localized controls (`art.player` group in locale files).
- Drag and drop a local video onto the player area; the backend validates the file type and registers it as a local resource.
- Left-hand playlist of playable local resources; sidebar width is resizable (200–360 px) and persisted.
- Torrent streaming: HTTP `Range`/`206` support, 4 MB read-ahead, and high piece priority for the file being watched.
- Playback preparation pipeline:
  - Probe the format with FFprobe.
  - Decide whether direct play is possible; convert when the system lacks an HEVC decoder, the container/codec is unsupported, or the bit depth is too high.
  - Prefer hardware encoding (NVENC on Windows, VideoToolbox on macOS) and fall back to `libx264` software encoding.
  - Guard against low disk space and defer conversion on metered networks.
  - Convert to H.264 + AAC MP4 with `+faststart`; conversion progress is polled via `/video/transcode/{jobId}`.
- Local files and completed torrents are served from `/video/{id}` with `Range` support, so seeking works the same way.

### Settings and personalization

- Interface language: English, 简体中文, 繁體中文; the first launch follows the system locale.
- Download directory selection with validation (applies to newly created tasks).
- Crawler switch and port (applied immediately).
- Theme mode (`dark` / `light`) and accent color (`#RRGGBB`, default `#3b82f6`).
- "Save" and "Reset to defaults" actions; validation errors are returned as localized messages from the backend.

### Internationalization

- All UI text, validation errors, download errors and HTTP error messages are resolved by the backend i18n service.
- Locale files: `resources/locales/active.en.json`, `active.zh-CN.json`, `active.zh-TW.json`.
- Player strings live in the `art.player` group and are passed to ArtPlayer as a language map.

### Update checking

- Wails Updater is initialized at startup with a 30-minute background check interval.
- The frontend subscribes to `Updater.Events.UpdateAvailable` and shows a localized prompt.
- **Before publishing**, change the repository in `internal/utils/updater/updater.go` (currently the Wails demo repository) to your own release repository.

## Project Structure

```
bing_play/
├── main.go                        # Entry point; embeds frontend assets and app icon
├── app/                           # Wails application wiring
│   ├── app.go                     # Fx composition of all modules
│   └── wails3_app.go              # Window options, services, lifecycle, updater
├── internal/
│   ├── config/                    # Runtime configuration (dev / prod build tags)
│   ├── db/                        # GORM initialization, migrations, DAOs, models
│   ├── dhtcrawler/                # DHT crawler service and resource persistence
│   ├── torrentmgr/                # Torrent engine, job state machine, download scheduling
│   ├── transcode/                 # Probe, conversion decision, FFmpeg job scheduler
│   ├── server/                    # Local HTTP server and video/stream handlers
│   ├── service/                   # Wails-bound business services (torrent/video/resource/settings/i18n)
│   ├── i18n/                      # go-i18n bundle, locale detection
│   ├── settings/                  # Setting keys and grouping
│   ├── filetype/                  # Video type detection (extension + MIME)
│   ├── logger/                    # slog + lumberjack setup (dev / prod)
│   ├── container/                 # Shared helpers
│   └── utils/                     # Paths, updater, IP helpers
├── frontend/
│   ├── src/views/                 # Play, Download, Resources, Settings, Layout, Find*, Library*
│   ├── src/components/            # ArtPlayer wrapper, BrandIcon
│   ├── src/i18n/                  # Locale loading from backend
│   ├── src/settings/              # Settings store, theme/accent application
│   ├── src/assets/icons/          # Theme-specific brand PNG icons
│   └── bindings/                  # Generated Wails bindings (not committed)
├── resources/
│   ├── locales/                   # en / zh-CN / zh-TW locale JSON
│   ├── migrations/                # SQL migration pairs
│   └── bin/ffmpeg/<goos>-<goarch>/ # Per-platform FFmpeg binaries (git-ignored)
├── build/                         # Platform build assets, Taskfiles, icons, installers
├── docs/resource_state_machine.md # Resource state machine design
└── Taskfile.yml                   # Build/dev/package task entry points
```

`Find.vue` and `Library.vue` are placeholders; everything else above is implemented.

## Usage

### Common scenarios

1. **Watch a local video** — open the Play page and drop a video file onto the player area. The file is registered as a local resource and appears in the playlist; playback starts automatically.
2. **Discover and download torrents** — the crawler runs in the background. Open Resources, search or sort the list, select one or more rows, then click Download. Progress appears in Download.
3. **Stream while downloading** — start a torrent download, open the Play page and select the corresponding file. The app serves byte ranges from the torrent and prioritizes the pieces needed for playback.
4. **Change the download folder** — Settings → Download directory. The change applies to tasks created afterwards; existing tasks keep their original target.

### Settings reference

| Key | Category | Default | Notes |
|---|---|---|---|
| `language` | general | system locale, fallback `en` | `en`, `zh-CN`, `zh-TW` |
| `download.dir` | general | `<data dir>/downloads` | Validated on save; affects new tasks |
| `crawler.enabled` | general | `true` | Applied immediately |
| `crawler.port` | general | `51414` | `1024–65535`, applied immediately |
| `ui.theme_mode` | interface | `dark` | `dark` / `light` |
| `ui.accent_color` | interface | `#3b82f6` | `#RRGGBB` |
| `ui.resource_page_size` | hidden | `100` | Saved in-place on the Resources page (`50/100/200`) |
| `ui.sidebar_width` | hidden | `260` | Saved in-place when resizing the playlist (`200–360`) |

### Local HTTP endpoints

The HTTP server binds to `127.0.0.1:9090` and is used for media delivery only:

| Method & path | Purpose |
|---|---|
| `POST /video/open` | Register a local file path dropped onto the player |
| `GET /video/prepare/{id}` | Probe a resource and return the playback plan (direct / convert) |
| `GET /video/transcode/{jobId}` | Poll conversion progress |
| `GET /video/cache/{name}` | Serve converted files with `Range` support |
| `GET /video/{id}` | Stream a resource (local file or torrent, with range support and piece priority) |
| `GET /video/local/{id}` | Reserved for local media; currently a development stub with a hard-coded test path and not used by the UI |
| `GET /stream/{id}` | Legacy stream entry point |

### Data locations

| Mode | Data directory | Database | Default downloads |
|---|---|---|---|
| Development (`!prod`) | `./data` | `./data/db/bt.db` | `./data/downloads` |
| Production (`prod`) | `os.UserConfigDir()/bing-play` (e.g. `%AppData%\bing-play`, `~/.config/bing-play`, `~/Library/Application Support/bing-play`) | `<data dir>/db/bt.db` | `<data dir>/downloads` |

Logs rotate via lumberjack under the data directory. Crawler node snapshots and runtime crawl data live in `resources/crawler/`.

## Deployment

### 1. Prepare release metadata

- Update `build/config.yml` (`info.productName`, `productIdentifier`, `version`, `companyName`, `copyright`). These are still template values (`My Product`, `com.mycompany.myproduct`).
- Add per-platform FFmpeg binaries under `resources/bin/ffmpeg/<goos>-<goarch>/`.
- Point the updater to your own release repository (`internal/utils/updater/updater.go`).

### 2. Build and package

```bash
# Windows: binary + NSIS installer
wails3 task build
wails3 task package                 # creates the NSIS installer (default machine scope)

# macOS: universal app bundle + DMG
wails3 task darwin:build
wails3 task darwin:package

# Linux: binary + AppImage / deb / rpm / Arch package
wails3 task linux:build
wails3 task linux:package
```

Notes:

- Icons are generated from `build/appicon.png` (`wails3 generate icons` → `build/windows/icon.ico`, `build/darwin/icons.icns`). Android/iOS icon sets live under `build/android` and `build/ios`.
- macOS release builds require signing and notarization tasks provided in `build/darwin/Taskfile.yml` when distributing outside your own machine.
- The Windows build generates a `.syso` resource file so the executable carries the brand icon.
- `wails3 task build` copies `resources/` into `bin/` before compiling; distribute that directory together with the binary (or use the generated installer).

### 3. Optional server mode

The Taskfile also exposes template-provided server/Docker tasks (`build:server`, `run:server`, `build:docker`, `run:docker`). The current application is desktop-first (Wails window, `gui` subsystem on Windows); evaluate these tasks before relying on them for headless deployment.

## Contributing

1. **Fork and branch** — create a feature or fix branch from `master` (hosted on Gitee: `git@gitee.com:bing_7/bing_play.git`).
2. **Read before editing** — inspect the related services, DAOs and wiring; prefer reusing existing Fx modules and services over new infrastructure.
3. **Follow the project conventions** — keep naming and structure consistent, avoid unrelated refactors and third-party dependencies without prior agreement.
4. **Keep i18n on the backend** — user-facing text must come from locale files (`resources/locales/*.json`), grouped by feature, not hard-coded in Vue components.
5. **Database changes** — add a paired `up`/`down` migration in `resources/migrations/` and describe compatibility in the pull request.
6. **Tests** — add unit tests for new backend logic; run `go test ./internal/...` and `npm --prefix frontend run build` before opening a pull request.
7. **Pull request** — describe the change, the motivation, and the verification you performed with realistic commands and results.

## License

This project is released under the [MIT License](LICENSE).

---

Related documentation: [Resource state machine](docs/resource_state_machine.md) · [Project development guidelines](AGENTS.md)
