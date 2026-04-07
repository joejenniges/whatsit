# RadioTranscriber

Live radio stream transcription for trivia competitions. Captures audio from MP3 streams, classifies it as speech, music, or singing, transcribes speech segments using whisper.cpp or Parakeet CTC, and displays a searchable running log.

Built with Go, Wails v2, and Svelte.

## Features

- Live MP3 stream transcription via whisper.cpp (GPU accelerated via Vulkan)
- 3-phase audio classification: CED-tiny AI model + rhythm beat detection + fusion logic
- Speech/music/singing detection with genre identification
- Two ASR engines: whisper.cpp (default) and Parakeet CTC (ONNX)
- Two transcription modes: segment (cleaner text) and rolling (lower latency)
- Searchable transcript with glob wildcard matching and filter mode
- Editable song markers with manual insert
- Listen to the radio stream through your speakers
- Multi-select entries for copy/delete
- Auto-scroll with floating scroll-to-bottom button
- Window position and size remembered between sessions
- Dark theme UI (Wails + Svelte)

## Screenshots

Screenshots coming soon.

## Requirements

### End User

- Windows 10/11 x64
- Internet connection (for radio streaming)
- Vulkan GPU drivers (optional -- falls back to CPU)

### Developer (building from source)

- Go 1.22+
- Node.js 18+ and npm
- MSYS2 with ucrt64 environment
- Wails CLI v2

MSYS2 packages:

| Package | Purpose |
|---------|---------|
| `mingw-w64-ucrt-x86_64-whisper.cpp` | Speech-to-text engine |
| `mingw-w64-ucrt-x86_64-gcc` | C/C++ compiler for CGO |
| `mingw-w64-ucrt-x86_64-cmake` | Build system (whisper dependency) |
| `mingw-w64-ucrt-x86_64-chromaprint` | Optional, for future AcoustID support |

## Building from Source

```bash
# 1. Install MSYS2 (if not already)
# Download from https://www.msys2.org/

# 2. Install MSYS2 packages (from MSYS2 UCRT64 terminal)
pacman -S mingw-w64-ucrt-x86_64-whisper.cpp mingw-w64-ucrt-x86_64-gcc

# 3. Install Go 1.22+
# Download from https://go.dev/dl/

# 4. Install Node.js 18+
# Download from https://nodejs.org/

# 5. Install Wails CLI
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# 6. Set environment
export PATH="/c/Users/$USER/go/bin:/c/msys64/ucrt64/bin:$PATH"
export PKG_CONFIG_PATH="/c/msys64/ucrt64/lib/pkgconfig"

# 7. Install frontend dependencies
cd frontend && npm install && cd ..

# 8. Build
wails build
```

The binary is output to `build/bin/RadioTranscriber.exe`.

### Makefile Targets

| Target | Description |
|--------|-------------|
| `make build` | Build the Go binary (without Wails frontend) |
| `make test` | Run all tests |
| `make test-whisper` | Run whisper-specific tests (requires whisper.cpp) |
| `make dist` | Build and package into a zip with all DLLs and models |
| `make clean` | Remove build artifacts |

## Distribution

The `build/bin/` directory contains everything needed to run:

- `RadioTranscriber.exe`
- DLLs: `libwhisper-1.dll`, `ggml.dll`, `ggml-base.dll`, `ggml-cpu.dll`, `ggml-vulkan.dll`, `libgcc_s_seh-1.dll`, `libstdc++-6.dll`, `libwinpthread-1.dll`, `onnxruntime.dll`, `libchromaprint.dll`
- Model files: `ced-tiny.onnx`, `ced-tiny.onnx.data` (CED audio classifier)

The whisper model (`ggml-base.en.bin`, ~142 MB) downloads automatically on first run to `%APPDATA%/RadioTranscriber/models/`. For the Parakeet engine, its model (~2.5 GB) also downloads automatically on first run.

## Configuration

All settings are accessible from the Settings tab in the UI. Configuration is persisted to `%APPDATA%/RadioTranscriber/config.yaml`.

### Stream

| Setting | Description | Default |
|---------|-------------|---------|
| Stream URL | MP3 stream URL | (empty) |
| Language | Transcription language: `en`, `es`, `fr`, `de`, `auto` | `en` |

### Speech Recognition

| Setting | Description | Default |
|---------|-------------|---------|
| ASR Engine | `whisper` (whisper.cpp) or `parakeet` (Parakeet CTC ONNX) | `whisper` |
| Whisper Model | `tiny` (~75 MB), `base` (~142 MB), `small` (~466 MB), `medium` (~1.5 GB) | `base` |
| Transcription Mode | `segment` (transcribe on speech-to-music transition, cleaner text) or `rolling` (progressive output, lower latency) | `segment` |
| Use GPU | Vulkan GPU acceleration (requires restart) | `true` |

### Audio Classification

| Setting | Description | Default |
|---------|-------------|---------|
| Classifier Tier | Classification algorithm (see table below) | `whisper+rhythm` |
| Classifier Debug | Log raw feature values for debugging | `false` |

**Classifier tiers:**

| Tier | Description |
|------|-------------|
| `basic` | ZCR + spectral flatness |
| `scheirer` | 4-feature analysis |
| `mfcc` | Cepstral analysis |
| `whisper` | Uses ASR confidence |
| `whisper+rhythm` | Whisper + rhythm beat detection (recommended) |
| `fusion` | CED-tiny AI + rhythm (AI classifier) |
| `scheirer+rhythm` | Scheirer + rhythm |
| `mfcc+rhythm` | MFCC + rhythm |

### Advanced

| Setting | Description | Default |
|---------|-------------|---------|
| Window Size | Rolling window size in seconds (rolling mode only) | `10` |
| Window Step | Rolling window step in seconds (rolling mode only) | `3` |
| Save Audio | Save WAV segments to disk | `false` |

Settings marked "requires restart" take effect after restarting the application. The UI prompts for this when saving.

## Architecture

Audio pipeline:

```
MP3 Stream -> Decode -> Resample (16kHz mono) -> Classify -> Route
  Speech -> Accumulate -> Whisper/Parakeet -> Display
  Music  -> Song marker -> (optional AcoustID)
```

The application uses an orchestrator pattern (`internal/app`) that wires together the stream decoder, classifier, transcriber, and UI layer. The Wails backend (`app.go`) exposes bound methods to the Svelte frontend via WebView IPC.

## Project Structure

```
.
├── main.go                  # Wails entry point
├── app.go                   # Wails-bound methods (config, streaming, DB queries)
├── appstate.go              # Reactive UI state management
├── wails_ui.go              # WailsUI adapter (bridges orchestrator to frontend)
├── windowstate.go           # Window position/size persistence
├── wails.json               # Wails project configuration
├── Makefile                 # Build targets (build, test, dist)
├── frontend/
│   └── src/lib/
│       ├── LiveView.svelte       # Main transcript view
│       ├── SettingsPage.svelte   # Settings UI
│       ├── StatusBar.svelte      # Connection, classification, controls
│       ├── SearchBar.svelte      # Glob search and filtering
│       ├── TranscriptLine.svelte # Speech entry component
│       ├── SongLine.svelte       # Song marker component
│       ├── DownloadScreen.svelte # Model download progress
│       └── ScrollToBottom.svelte # Auto-scroll button
├── internal/
│   ├── app/           # Orchestrator (pipeline wiring)
│   ├── audio/         # MP3 decoding, resampling, playback
│   ├── classifier/    # Audio classification (basic, scheirer, mfcc, CED, rhythm)
│   ├── config/        # YAML config loading/saving
│   ├── logging/       # File-based logging setup
│   ├── musicid/       # Music identification (AcoustID, future)
│   ├── storage/       # SQLite database layer
│   ├── transcriber/   # Whisper CGO bindings, Parakeet ONNX, model download
│   └── ui/            # UI interface definition
├── build/bin/         # Output directory (exe, DLLs, models)
└── testdata/          # Test fixtures
```

## Data Storage

All application data is stored under `%APPDATA%/RadioTranscriber/`:

| Path | Contents |
|------|----------|
| `config.yaml` | User configuration |
| `models/` | Downloaded ASR models (whisper, parakeet) |
| `transcripts.db` | SQLite database of transcript entries |
| `logs/` | Application log files |
| `audio/` | Saved WAV segments (when save audio is enabled) |
| `window.json` | Window position and size |

## Keyboard Shortcuts

| Shortcut | Action |
|----------|--------|
| Ctrl+Click | Select/deselect entry |
| Shift+Click | Select range of entries |
| Ctrl+C | Copy selected entries |
| Delete | Delete selected entries |
| Escape | Clear selection |
| Double-click | Edit speech entry text |

## License

All rights reserved.
