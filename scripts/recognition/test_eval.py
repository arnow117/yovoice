"""评估器运行输入指纹的隔离测试。"""

from __future__ import annotations

import hashlib
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock


EVAL_PATH = Path(__file__).with_name("eval.py")
SPEC = importlib.util.spec_from_file_location("speaker_eval", EVAL_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError("cannot load evaluation module")
EVAL = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(EVAL)


def file_hash(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


class ArtifactFingerprintTest(unittest.TestCase):
    def test_reloads_model_and_each_manifest_sample(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            model = root / "model.onnx"
            runner = root / "runner.py"
            binary = root / "yovoice"
            model.write_bytes(b"model-one")
            runner.write_text("runner-one", encoding="utf-8")
            binary.write_bytes(b"binary")
            samples = []
            for index in range(5):
                path = root / f"sample-{index}.wav"
                path.write_bytes(f"sample-{index}".encode())
                samples.append(
                    {
                        "id": f"sample-{index}",
                        "path": path.name,
                        "source": "https://example.test/sample.wav",
                        "sha256": file_hash(path),
                    }
                )
            manifest = root / "manifest.json"
            manifest.write_text(
                json.dumps(
                    {
                        "version": 1,
                        "model": {
                            "path": model.name,
                            "source": "https://example.test/model.onnx",
                            "sha256": file_hash(model),
                        },
                        "samples": samples,
                    }
                ),
                encoding="utf-8",
            )
            output = root / "output"
            with mock.patch.dict(os.environ, {"YOVOICE_SPEAKER_RUNNER": str(runner)}, clear=False):
                evaluation = EVAL.Evaluation(root, binary, output, manifest)
                before = evaluation.artifact_fingerprint()
                model.write_bytes(b"model-two")
                after_model_change = evaluation.artifact_fingerprint()
                self.assertNotEqual(before["modelSha256"], after_model_change["modelSha256"])

                sample = root / "sample-3.wav"
                sample.write_bytes(b"sample-three-changed")
                after_sample_change = evaluation.artifact_fingerprint()
                self.assertNotEqual(before["sampleSha256"], after_sample_change["sampleSha256"])
                self.assertEqual(len(after_sample_change["sampleSha256"]), 5)


if __name__ == "__main__":
    unittest.main()
