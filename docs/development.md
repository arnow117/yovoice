# Development

English · [简体中文](development_zh.md)

This guide is for building yovoice from source. For prebuilt packages, see [CLI installation](cli.md#installation) or [Releases](https://github.com/leemysw/yovoice/releases).

## Requirements

- Node.js 22+, pnpm 9.15.2, Go 1.26+, Python 3.12+.
- Make, Bash, curl, and a C compiler for the bundled audio converter.
- macOS: Apple Silicon, macOS 14+, and Xcode Command Line Tools.
- Windows: Windows 10/11 x64, .NET SDK 8, WebView2 Runtime, and MSYS2 MinGW64. Install Inno Setup 6 to create installers. Make targets require GNU Make.

On Windows, install the audio build tools in the MSYS2 MinGW64 terminal:

```sh
pacman -Syu
pacman -S --needed make curl diffutils mingw-w64-x86_64-gcc mingw-w64-x86_64-python
```

Reopen the terminal if the update requests it. The build finds MSYS2 at `artifacts/tooling/msys64` or `C:/msys64`; set `YOVOICE_MSYS2` for another location.

## Run the app

```sh
git clone https://github.com/leemysw/yovoice.git
cd yovoice
make install
make app-run
```

Run all following commands from the repository root. `app-run` builds and starts the app without tests or packaging. On macOS, it streams logs to the terminal. Closing the window keeps the app running; quit from the app menu before rebuilding. Logs are in `~/.yovoice/logs`.

For browser-only UI development, run `make dev` and open `http://127.0.0.1:5173`. Local speech generation requires the desktop app or CLI.

## Build and package

| Command | Output |
| --- | --- |
| `make app-build` | Runnable app in `artifacts/macos-arm64/` or `artifacts/windows-x64/` |
| `make app-package` | macOS DMG/ZIP or Windows Setup EXE in `artifacts/` |
| `make cli-build` | CLI executable and `artifacts/tools/` |
| `python3 scripts/package-cli.py` | Standalone CLI ZIP in `artifacts/` |

Keep `tools/` beside the CLI executable. Complete packages must be built on their target platform. macOS DMG packaging requires a logged-in desktop session and Finder automation permission.

On Windows without GNU Make, use PowerShell:

```powershell
./scripts/desktop/build-windows.ps1
./scripts/desktop/build-windows.ps1 -Package
./scripts/desktop/build-windows.ps1 -Package -Cuda  # also build the CUDA 12.4 / 13.3 full installers
python scripts/package-cli.py
```

Model conversion is separate from app packaging. For compatible Kokoro model preparation, run `python scripts/prepare-kokoro-models.py --help` from the repository.

## Tests

```sh
pnpm --dir web exec playwright install chromium
make check
```

Use `make check-core` for Go formatting, static analysis and tests with the race detector, or `make check-web` for the frontend build and browser tests. Native installation and GPU inference also need testing on the target platform.

## Speaker-recognition development

The non-realtime recognition prototype uses an externally configured Python 3.12 environment, `sherpa-onnx==1.13.8`, `numpy==2.5.3`, and the WeSpeaker ONNX model. It is deliberately not installed, downloaded, or packaged by yovoice.

Use `uv` to prepare an isolated environment; keep caches outside the repository when necessary:

```sh
UV_CACHE_DIR=/private/tmp/yovoice-uv-cache uv venv artifacts/recognition/venv --python 3.12
UV_CACHE_DIR=/private/tmp/yovoice-uv-cache uv pip install --python artifacts/recognition/venv/bin/python -r scripts/recognition/requirements.txt
make check-recognition
```

Set `YOVOICE_SPEAKER_PYTHON`, `YOVOICE_SPEAKER_MODEL`, and `YOVOICE_SPEAKER_RUNNER` to absolute paths before running CLI or HTTP recognition. The model and test recordings stay under ignored `artifacts/recognition/`; validate their SHA-256 against `scripts/recognition/eval-manifest.json`. Do not commit, redistribute, or infer a license for those local files from the Apache-2.0 source license.

For the asserted local CLI and HTTP regression, build an ignored CLI artifact, place a development FFmpeg beside it, set every runtime value to an absolute path, and choose a new evidence directory:

```sh
repo=$(pwd -P)
(cd "$repo" && go build -o artifacts/yovoice ./cmd/yovoice)
mkdir -p "$repo/artifacts/tools"
ln -sfn /opt/homebrew/bin/ffmpeg "$repo/artifacts/tools/ffmpeg"

export YOVOICE_SPEAKER_PYTHON="$repo/artifacts/recognition/venv/bin/python"
export YOVOICE_SPEAKER_MODEL="$repo/artifacts/recognition/models/wespeaker_zh_cnceleb_resnet34.onnx"
export YOVOICE_SPEAKER_RUNNER="$repo/scripts/recognition/runner.py"
export YOVOICE_SPEAKER_THRESHOLD=0.6
export YOVOICE_SPEAKER_MARGIN=0.05
export YOVOICE_SPEAKER_TIMEOUT=120s

YOVOICE_EVAL_OUTPUT="$repo/artifacts/verification/YYYYMMDD-speaker-regression-rN" \
  "$repo/scripts/recognition/run-eval.sh"
```

The evaluator reads `scripts/recognition/eval-manifest.json` for every local asset hash, query role, expected result, and fixed decision configuration. It refuses an existing output directory or a threshold, margin, or timeout change; `.6` and `0.6` are equivalent fixed threshold spellings. It creates separate temporary data directories for the CLI and a self-owned loopback HTTP service, enrolls two profiles, checks both independent known queries against their enrollment IDs, requires the unknown query to have no selected profile, and checks each list has two profiles. HTTP enrollment must return 201; identify and list must return 200. Before and after the business actions, it records source, binary, model, runner, manifest, all five sample hashes, and the calculated feature-version fingerprint and fails if any input changes. `summary.json` contains only public JSON responses, per-action exit/status/failure and duration, tool/package versions, expected counts, and actual completed/correct/rejected counts; it then removes the temporary data directories that contain private embeddings. A `PASS` summary means those assertions completed.

Use a new `YYYYMMDD-speaker-regression-rN` directory for every run; retained directories are evidence and are never overwritten. The command uses the developer's FFmpeg (9.0.2 in the prepared environment), not the project's packaged FFmpeg 8.1.2, so it verifies the local recognition flow but not release-package conversion. The sample denominator is 2 known matched queries and 1 unknown non-match. This is an asserted integration regression, not a population-accuracy, real-TTS quality, GPU, Windows, or anti-spoofing result.

## Project layout

| Directory | Purpose |
| --- | --- |
| `cmd/` | CLI and local service entry points |
| `internal/` | Go service, split into packages by responsibility (see below) |
| `desktop/macos/`, `desktop/windows/` | Native app hosts |
| `web/src/` | Frontend application |
| `web/browser-tests/` | Browser tests |
| `scripts/` | Build and packaging tools |

### Architecture conventions

- Frontend dependencies flow `app → features → shared`; `shared` never imports feature modules.
- The Go service is split into packages by responsibility. Dependencies only point downward:

  | Layer | Packages | Responsibility |
  | --- | --- | --- |
  | Transport | `desktop`, `remote` | Local desktop server and RPC dispatch; remote HTTP API, async jobs and MCP |
  | Application | `workbench` | Projects, media, characters, models, runtime, generation and project archives |
  | Infrastructure | `engine`, `store`, `download`, `audio` | Inference process, state persistence, downloads and extraction, audio parsing and conversion |
  | Model | `schema`, `catalog` | Persisted structures and validation; model catalog and generation options |
  | Leaf | `msg`, `diag`, `platform` | Message codes, diagnostic logs, platform differences |

  Transport packages only decode requests and call public `Workbench` methods; they never touch its internal state. `testkit` is for tests only.
- Desktop calls run concurrently. `Workbench` serializes short read-validate-write transactions with an edit lock; conversion, downloads, inference and project import/export run without it. Only one background operation runs at a time.
- The model catalog, generation options and OmniVoice attributes are embedded in the Go service and imported by the UI. Keep `internal/catalog/*.json` and `web/src/shared/lib/*.json` identical (`engine.json` is used only by the service and build scripts and has no UI copy).
- Declare message codes in `internal/msg`, add them to `msg.All`, and provide copy in both UI languages.
- To upgrade audio.cpp, edit only `internal/catalog/engine.json`: `version` is the recommended release, `minimum` is the oldest runtime the request mapping and model packages support, plus the archive checksums. Runtime installs and the build scripts in `scripts/desktop/` read it. The installed runtime version is stored in the state; below `minimum` an upgrade is required, below `version` the UI offers an optional upgrade. Raise `minimum` only when the app actually depends on the newer runtime.
- On Windows, `-Package` builds the full CPU installer and the app-only `-update.exe`. Auto-update downloads only the latter, leaving `engine/` and its `yovoice-engine.json` (bundled runtime version and backend) untouched.

`go test` enforces the last three.

Public guides use English `*.md` and Chinese `*_zh.md` files. Keep both versions and their links in sync when changing user-facing behavior.

Release notes from v0.1.7 onward must include separate `### English` and `### 简体中文` sections in CHANGELOG.md. The release workflow checks both sections before building.
