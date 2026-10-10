# CLI and Agent Skill

English · [简体中文](cli_zh.md)

Generate speech without opening the desktop app. The CLI supports macOS on Apple Silicon, Windows x64, and Linux x64.

## Installation

Download the CLI archive for your platform from [Releases](https://github.com/leemysw/yovoice/releases).

| Platform | Archive | Executable |
| --- | --- | --- |
| macOS · Apple Silicon | `yovoice-<version>-cli-macos-arm64.zip` | `yovoice` |
| Windows · x64 | `yovoice-<version>-cli-windows-x64.zip` | `yovoice.exe` |
| Linux · x64 | `yovoice-<version>-cli-linux-x64.zip` | `yovoice` |

Extract the archive and keep `tools/` beside the executable. Add that directory to your PATH, or use the executable's full path. Reopen your terminal after changing PATH.

```sh
yovoice --version
yovoice --help
```

The package includes the audio converter; no separate FFmpeg, Python, or Go installation is required. Engines and models are downloaded separately.

## Set up an engine and model

```sh
yovoice setup
yovoice models list --json
yovoice models download index-2.5-q8
```

`setup` uses Metal on macOS and CPU on Windows/Linux by default. Choose another backend with:

| Platform | Supported backends |
| --- | --- |
| macOS · Apple Silicon | `--backend metal`, `--backend cpu` |
| Windows · x64 | `--backend cpu`, `--backend cuda`, `--backend vulkan` |
| Linux · x64 | `--backend cpu`, `--backend vulkan` |

GPU backends require compatible hardware and drivers. Run `setup` again to switch backends.

Model downloads use ModelScope by default. Add `--source huggingface` or `--source mirror` to change the source. Downloads resume after interruption and are verified automatically. To register an existing supported GGUF file:

```sh
yovoice models import /absolute/path/model.gguf
```

Only model packages recognized by yovoice are accepted. See [model selection](models.md) for input requirements and examples.

## Generate speech

```sh
yovoice generate --model index-2.5-q8 --text-file narration.txt --reference voice.wav --output narration.wav
```

- Use either `--text TEXT` or `--text-file FILE`.
- For voice cloning, use either `--reference FILE` or `--voice ID` from `yovoice voices list`.
- Reference audio must be 1–60 seconds and no larger than 20 MB. Supported formats include WAV, MP3, M4A/AAC, FLAC, OGG/Opus, AIFF, WMA, and WebM. Conversion is automatic.
- Output must use a `.wav` extension. Existing files are not overwritten.
- Commands wait for completion. Ctrl-C cancels generation. Repeated `generate` commands reload the model; use the [remote service](api.md) for a persistent engine.

To save a reusable voice:

```sh
yovoice voices import voice.wav --name Narrator
yovoice voices list --json
```

Progress is written to stderr and results to stdout as JSON:

```json
{"id":"...","path":"/absolute/narration.wav","duration":4.2,"model":"index-2.5-q8","voice":"..."}
```

Failures return an error on stderr and exit code 1; Ctrl-C returns 130.

## Speaker profiles

See the global [usage index](usage.md#usage-index--使用导航) for USG-SPEAKER-ENROLL, USG-SPEAKER-IDENTIFY, USG-SPEAKER-GENERATE, USG-SPEAKER-DELETE, and USG-SPEAKER-VERIFY.

Speaker recognition is a local, optional development/runtime integration. Before enrolling or identifying, set absolute local paths for the isolated Python interpreter, WeSpeaker ONNX model, and repository runner:

```sh
export YOVOICE_SPEAKER_PYTHON=/absolute/path/to/venv/bin/python
export YOVOICE_SPEAKER_MODEL=/absolute/path/to/wespeaker.onnx
export YOVOICE_SPEAKER_RUNNER=/absolute/path/to/scripts/recognition/runner.py
export YOVOICE_SPEAKER_THRESHOLD=0.6
export YOVOICE_SPEAKER_MARGIN=0.05
export YOVOICE_SPEAKER_TIMEOUT=120s
```

`YOVOICE_SPEAKER_THRESHOLD` is finite in `[-1,1]`; `YOVOICE_SPEAKER_MARGIN` is in `(0,2]`; timeout is in `(0,10m]`. Values are read once when the service first uses recognition. The CLI package includes FFmpeg but does not package Python, the ONNX model, or recognition samples.

```sh
yovoice speakers enroll --file speaker-a.wav --name "Speaker A" --bind-voice EXISTING_VOICE_ID --data-dir ./voice-data
yovoice speakers identify --file query.wav --data-dir ./voice-data
yovoice speakers list --data-dir ./voice-data
yovoice speakers delete PROFILE_ID --data-dir ./voice-data
```

`enroll` and `identify` accept exactly one of `--file` or `--voice`. Input is converted to mono 16 kHz PCM16, must be 1–60 seconds and at most 20 MiB. `list` returns only public metadata, never embeddings or local paths. An empty library returns `unknown`; a close best/second score returns `ambiguous`. The feature does not determine identity, detect multiple speakers, reject arbitrary noise, or provide anti-spoofing.

## Generate through a profile

Use a profile only after explicitly binding it to an existing voice. The profile supplies that voice and saved reference text; your text and supported emotion settings remain caller-controlled.

```sh
yovoice generate --speaker-profile PROFILE_ID --model voxcpm2-q8 --text "Hello" --output profile.wav --data-dir ./voice-data
```

`--speaker-profile` cannot be combined with `--voice`, `--reference`, or `--reference-text`. It supports IndexTTS, VoxCPM clone/continuation, OmniVoice clone, and Qwen3-TTS Base. VoiceDesign, CustomVoice and Kokoro are rejected. A missing binding or a deleted bound voice does not fall back to another voice.

## Data directory

The default directory is `~/.yovoice`. Only one app, CLI command, or server can use a directory at a time. Quit the app, or use a separate directory with `--data-dir DIR` on every command:

```sh
yovoice setup --data-dir ./voice-data
yovoice models download index-2.5-q8 --data-dir ./voice-data
yovoice generate --text-file narration.txt --reference voice.wav --output narration.wav --data-dir ./voice-data
```

Engine logs are stored in the data directory's `logs/` folder.

## Agent Skill

Copy the package's `skills/yovoice` folder into your agent's Skill directory. Install the CLI on the agent's PATH for local generation. If you use an existing remote service, the client does not need the CLI or models.

Ask your agent to read a text file, choose a voice, and save the result. See [API / MCP deployment](api.md) for remote access and [development](development.md) for building from source.
