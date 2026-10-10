import contextlib
import hashlib
import importlib.util
import io
import struct
import sys
import tempfile
import types
import unittest
from pathlib import Path
from unittest import mock


SPEC = importlib.util.spec_from_file_location("yovoice_runner", Path(__file__).with_name("runner.py"))
runner = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(runner)


def wav(path: Path, seconds: int = 1, sample: int = 1000) -> None:
    frames = struct.pack("<h", sample) * (16000 * seconds)
    path.write_bytes(
        b"RIFF"
        + struct.pack("<I", 36 + len(frames))
        + b"WAVEfmt "
        + struct.pack("<IHHIIHH", 16, 1, 1, 16000, 32000, 2, 16)
        + b"data"
        + struct.pack("<I", len(frames))
        + frames
    )


def request_stream(raw: bytes) -> io.TextIOWrapper:
    return io.TextIOWrapper(io.BytesIO(raw), encoding="utf-8")


def fake_sherpa(values: list[float], ready: bool = True) -> types.SimpleNamespace:
    class Stream:
        def accept_waveform(self, sample_rate: int, waveform: object) -> None:
            if sample_rate != 16000 or len(waveform) == 0:
                raise AssertionError("unexpected waveform")

        def input_finished(self) -> None:
            pass

    class Extractor:
        def __init__(self, _config: object) -> None:
            pass

        def create_stream(self) -> Stream:
            return Stream()

        def is_ready(self, _stream: Stream) -> bool:
            return ready

        def compute(self, _stream: Stream) -> list[float]:
            return values

    return types.SimpleNamespace(
        SpeakerEmbeddingExtractorConfig=lambda **_kwargs: object(),
        SpeakerEmbeddingExtractor=Extractor,
    )


class RunnerTest(unittest.TestCase):
    def read_request(self, raw: bytes) -> dict[str, object]:
        original = sys.stdin
        try:
            sys.stdin = request_stream(raw)
            return runner.read_request()
        finally:
            sys.stdin = original

    def test_request_requires_exact_protocol(self) -> None:
        request = {
            "protocolVersion": 1,
            "modelPath": "/tmp/model.onnx",
            "modelSha256": "a" * 64,
            "audioPath": "/tmp/input.wav",
        }
        self.assertEqual(self.read_request(b'{"protocolVersion":1,"modelPath":"/tmp/model.onnx","modelSha256":"' + b"a" * 64 + b'","audioPath":"/tmp/input.wav"}'), request)
        for raw in (
            b"",
            b"[]",
            b'{"protocolVersion":1}',
            b'{"protocolVersion":2,"modelPath":"x","modelSha256":"x","audioPath":"x"}',
            b'{"protocolVersion":1,"modelPath":"x","modelSha256":"x","audioPath":"x","extra":true}',
        ):
            with self.assertRaisesRegex(runner.RunnerError, "invalid_request"):
                self.read_request(raw)

    def test_pcm16_validation_includes_truncated_frames(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "input.wav"
            wav(path)
            samples = runner.read_pcm16(path)
            self.assertEqual(len(samples), 16000)
            path.write_bytes(path.read_bytes()[:-2])
            with self.assertRaisesRegex(runner.RunnerError, "invalid_audio"):
                runner.read_pcm16(path)
            wav(path, sample=0)
            with self.assertRaisesRegex(runner.RunnerError, "invalid_audio"):
                runner.read_pcm16(path)
            wav(path, seconds=0)
            with self.assertRaisesRegex(runner.RunnerError, "invalid_audio"):
                runner.read_pcm16(path)

    def test_extract_checks_runtime_ready_and_features(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            model = root / "model.onnx"
            audio = root / "input.wav"
            model.write_bytes(b"model")
            wav(audio)
            request = {
                "modelPath": str(model),
                "modelSha256": hashlib.sha256(model.read_bytes()).hexdigest(),
                "audioPath": str(audio),
            }
            with mock.patch.object(runner.importlib.metadata, "version", return_value=runner.RUNTIME_VERSION), mock.patch.dict(sys.modules, {"sherpa_onnx": fake_sherpa([1.0] + [0.0] * 255)}):
                result = runner.extract(request)
            self.assertEqual(len(result), runner.DIMENSION)
            self.assertEqual(result[0], 1.0)

            for values, ready, expected in (
                ([1.0] * 255, True, "invalid_feature"),
                ([float("nan")] + [0.0] * 255, True, "invalid_feature"),
                ([0.0] * 256, True, "invalid_feature"),
                ([1.0] + [0.0] * 255, False, "feature_not_ready"),
            ):
                with mock.patch.object(runner.importlib.metadata, "version", return_value=runner.RUNTIME_VERSION), mock.patch.dict(sys.modules, {"sherpa_onnx": fake_sherpa(values, ready)}):
                    with self.assertRaisesRegex(runner.RunnerError, expected):
                        runner.extract(request)
            with mock.patch.object(runner.importlib.metadata, "version", return_value="0.0.0"):
                with self.assertRaisesRegex(runner.RunnerError, "runtime_failed"):
                    runner.extract(request)

    def test_main_emits_only_stable_error_json(self) -> None:
        original = sys.stdin
        output = io.StringIO()
        try:
            sys.stdin = request_stream(b"not-json")
            with contextlib.redirect_stdout(output), self.assertRaises(SystemExit) as exit_error:
                runner.main()
        finally:
            sys.stdin = original
        self.assertEqual(exit_error.exception.code, 2)
        self.assertEqual(output.getvalue(), '{"errorCode":"invalid_request"}\n')


if __name__ == "__main__":
    unittest.main()
