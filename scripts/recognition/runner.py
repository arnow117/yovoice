#!/usr/bin/env python3
"""受 Go 进程调用的单请求 WeSpeaker 特征提取器。stdout 只能输出一个 JSON。"""

from __future__ import annotations

import hashlib
import importlib.metadata
import json
import math
import os
import sys
import wave
from pathlib import Path

import numpy as np

PROTOCOL_VERSION = 1
RUNTIME_VERSION = "1.13.8"
DIMENSION = 256
MAX_AUDIO_BYTES = 20 << 20


class RunnerError(Exception):
    """带稳定错误码的输入或运行时失败。"""

    def __init__(self, code: str) -> None:
        super().__init__(code)
        self.code = code


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(128 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def read_request() -> dict[str, object]:
    raw = sys.stdin.buffer.read(64 * 1024 + 1)
    if len(raw) == 0 or len(raw) > 64 * 1024:
        raise RunnerError("invalid_request")
    try:
        request = json.loads(raw)
    except json.JSONDecodeError as error:
        raise RunnerError("invalid_request") from error
    if not isinstance(request, dict) or set(request) != {"protocolVersion", "modelPath", "modelSha256", "audioPath"}:
        raise RunnerError("invalid_request")
    if request["protocolVersion"] != PROTOCOL_VERSION:
        raise RunnerError("invalid_request")
    if not all(isinstance(request[key], str) and request[key] for key in ("modelPath", "modelSha256", "audioPath")):
        raise RunnerError("invalid_request")
    return request


def read_pcm16(path: Path) -> np.ndarray:
    try:
        if not path.is_file() or path.stat().st_size > MAX_AUDIO_BYTES:
            raise RunnerError("invalid_audio")
        with wave.open(str(path), "rb") as source:
            if source.getnchannels() != 1 or source.getsampwidth() != 2 or source.getframerate() != 16000 or source.getcomptype() != "NONE":
                raise RunnerError("invalid_audio")
            frames = source.getnframes()
            if frames < 16000 or frames > 960000:
                raise RunnerError("invalid_audio")
            raw = source.readframes(frames)
            if len(raw) != frames * 2:
                raise RunnerError("invalid_audio")
            samples = np.frombuffer(raw, dtype="<i2").astype(np.float32) / 32768.0
    except (OSError, wave.Error) as error:
        raise RunnerError("invalid_audio") from error
    rms = float(np.sqrt(np.mean(np.square(samples))))
    if not math.isfinite(rms) or rms < 1e-4:
        raise RunnerError("invalid_audio")
    return samples


def extract(request: dict[str, object]) -> list[float]:
    model_path = Path(str(request["modelPath"]))
    if not model_path.is_absolute() or not model_path.is_file() or sha256_file(model_path) != request["modelSha256"]:
        raise RunnerError("model_mismatch")
    samples = read_pcm16(Path(str(request["audioPath"])))
    try:
        import sherpa_onnx

        if importlib.metadata.version("sherpa-onnx") != RUNTIME_VERSION or importlib.metadata.version("sherpa-onnx-core") != RUNTIME_VERSION:
            raise RunnerError("runtime_failed")

        config = sherpa_onnx.SpeakerEmbeddingExtractorConfig(
            model=str(model_path),
            num_threads=1,
            provider="cpu",
        )
        extractor = sherpa_onnx.SpeakerEmbeddingExtractor(config)
        stream = extractor.create_stream()
        stream.accept_waveform(sample_rate=16000, waveform=samples)
        stream.input_finished()
        if not extractor.is_ready(stream):
            raise RunnerError("feature_not_ready")
        values = [float(value) for value in extractor.compute(stream)]
    except RunnerError:
        raise
    except Exception as error:
        raise RunnerError("runtime_failed") from error
    if len(values) != DIMENSION or not all(math.isfinite(float(value)) for value in values):
        raise RunnerError("invalid_feature")
    magnitude = math.sqrt(sum(float(value) * float(value) for value in values))
    if not math.isfinite(magnitude) or magnitude == 0:
        raise RunnerError("invalid_feature")
    return [float(value) / magnitude for value in values]


def response(payload: dict[str, object], exit_code: int) -> None:
    sys.stdout.write(json.dumps(payload, allow_nan=False, separators=(",", ":")) + "\n")
    sys.stdout.flush()
    raise SystemExit(exit_code)


def main() -> None:
    try:
        request = read_request()
        embedding = extract(request)
        response(
            {
                "protocolVersion": PROTOCOL_VERSION,
                "modelSha256": request["modelSha256"],
                "runtimeVersion": RUNTIME_VERSION,
                "dimension": DIMENSION,
                "embedding": embedding,
            },
            0,
        )
    except RunnerError as error:
        response({"errorCode": error.code}, 2)
    except Exception:
        response({"errorCode": "runtime_failed"}, 2)


if __name__ == "__main__":
    main()
