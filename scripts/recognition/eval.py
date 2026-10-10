#!/usr/bin/env python3
"""运行由清单定义的本地声纹 CLI 与 HTTP 评估。"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import importlib.metadata
import json
import math
import os
from pathlib import Path
import secrets
import shutil
import signal
import socket
import subprocess
import sys
import time
from typing import Any
import urllib.error
import urllib.request


RUNTIME_VERSION = "1.13.8"
PREPROCESS_VERSION = "pcm16-mono16k-v1"
NORMALIZATION_VERSION = "l2-cosine-v1"
COMMAND_TIMEOUT_SECONDS = 150
HTTP_TIMEOUT_SECONDS = 15
SERVER_STOP_TIMEOUT_SECONDS = 10


class EvaluationError(Exception):
    """表示稳定的断言失败或本地前置条件不可用。"""


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def sha256_bytes(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def utc_now() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat().replace("+00:00", "Z")


def duration_ms(start: float) -> int:
    return round((time.monotonic() - start) * 1000)


def write_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as handle:
        json.dump(value, handle, ensure_ascii=False, indent=2, sort_keys=True)
        handle.write("\n")


class Evaluation:
    def __init__(self, root: Path, binary: Path, output: Path, manifest_path: Path) -> None:
        self.root = root.resolve()
        self.binary = binary.resolve()
        self.output = output.resolve()
        self.manifest_path = manifest_path.resolve()
        self.actions: list[dict[str, Any]] = []
        self.server: subprocess.Popen[bytes] | None = None
        self.base_url = ""
        self.token = ""
        self.data_directories = [self.output / "cli" / "data", self.output / "http" / "data"]
        self.manifest: dict[str, Any] = {}
        self.decision: dict[str, Any] = {}
        self.expected_feature_version = ""
        self.initial_fingerprint: dict[str, Any] | None = None
        self.final_fingerprint: dict[str, Any] | None = None
        self.model_hash: str | None = None
        self.tools: dict[str, str] = {}
        self.actual_counts = {
            surface: {
                "enrollmentsCompleted": 0,
                "knownQueriesCompleted": 0,
                "knownProfileIDsCorrect": 0,
                "unknownQueriesCompleted": 0,
                "unknownRejected": 0,
                "profilesListed": 0,
            }
            for surface in ("cli", "http")
        }

    def record(
        self,
        name: str,
        start: float,
        *,
        exit_code: int | str | None = None,
        status: int | None = None,
        failure: str | None = None,
    ) -> None:
        action: dict[str, Any] = {"name": name, "durationMs": duration_ms(start)}
        if exit_code is not None:
            action["exitCode"] = exit_code
        if status is not None:
            action["httpStatus"] = status
        if failure is not None:
            action["failure"] = failure
        self.actions.append(action)

    def require(self, condition: bool, message: str) -> None:
        if not condition:
            raise EvaluationError(message)

    def command_output(self, name: str, args: list[str]) -> bytes:
        start = time.monotonic()
        try:
            completed = subprocess.run(
                args,
                cwd=self.root,
                stdout=subprocess.PIPE,
                stderr=subprocess.DEVNULL,
                timeout=COMMAND_TIMEOUT_SECONDS,
            )
        except subprocess.TimeoutExpired as exc:
            self.record(name, start, exit_code="timeout", failure="timeout")
            raise EvaluationError(f"{name} timed out") from exc
        except OSError as exc:
            self.record(name, start, exit_code="unavailable", failure="unavailable")
            raise EvaluationError(f"{name} is unavailable") from exc
        self.record(name, start, exit_code=completed.returncode)
        if completed.returncode != 0:
            raise EvaluationError(f"{name} failed")
        return completed.stdout

    def load_manifest(self) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
        self.manifest = self.read_manifest_file()

        self.require(self.manifest.get("version") == 1, "manifest version is invalid")
        decision = self.manifest.get("decision")
        self.require(isinstance(decision, dict), "manifest decision is invalid")
        self.require(
            decision == {"threshold": 0.6, "margin": 0.05, "timeout": "120s", "dimension": 256},
            "manifest decision is invalid",
        )
        self.require(self.fixed_configuration_matches(decision), "effective recognition configuration differs from manifest")
        self.decision = decision

        model = self.manifest.get("model")
        self.require(isinstance(model, dict), "manifest model is invalid")
        samples = self.manifest.get("samples")
        self.require(isinstance(samples, list) and len(samples) == 5, "manifest samples are invalid")
        enrollment = [sample for sample in samples if sample.get("role") == "enroll"]
        queries = [sample for sample in samples if sample.get("role") == "query"]
        self.require(
            [sample.get("label") for sample in enrollment] == ["A", "B"],
            "manifest enrollment samples are invalid",
        )
        self.require(
            [(sample.get("label"), sample.get("expected")) for sample in queries]
            == [("A", "matched"), ("B", "matched"), ("unknown", "unknown")],
            "manifest query samples are invalid",
        )
        return enrollment, queries

    def read_manifest_file(self) -> dict[str, Any]:
        try:
            content = self.manifest_path.read_bytes()
            manifest = json.loads(content)
        except (OSError, json.JSONDecodeError) as exc:
            raise EvaluationError("manifest is unreadable") from exc
        self.require(isinstance(manifest, dict), "manifest is invalid")
        return manifest

    def fixed_configuration_matches(self, decision: dict[str, Any]) -> bool:
        try:
            threshold = float(os.environ.get("YOVOICE_SPEAKER_THRESHOLD", ""))
            margin = float(os.environ.get("YOVOICE_SPEAKER_MARGIN", ""))
        except ValueError:
            return False
        return (
            math.isfinite(threshold)
            and math.isfinite(margin)
            and threshold == decision["threshold"]
            and margin == decision["margin"]
            and os.environ.get("YOVOICE_SPEAKER_TIMEOUT") == decision["timeout"]
        )

    def validate_assets(self, samples: list[dict[str, Any]]) -> tuple[Path, str]:
        model = self.manifest["model"]
        model_path = self.resolve_manifest_asset(model, "model")
        configured_model = Path(os.environ["YOVOICE_SPEAKER_MODEL"]).resolve()
        self.require(configured_model == model_path, "configured model differs from manifest")
        runner = Path(os.environ["YOVOICE_SPEAKER_RUNNER"])
        self.require(runner.is_file(), "configured runner is unavailable")
        model_hash = self.validate_asset(model_path, model, "model")
        for sample in samples:
            sample_path = self.resolve_manifest_asset(sample, "sample")
            self.validate_asset(sample_path, sample, "sample")
        return model_path, model_hash

    def resolve_manifest_asset(self, entry: dict[str, Any], kind: str) -> Path:
        value = entry.get("path")
        source = entry.get("source")
        expected_hash = entry.get("sha256")
        self.require(
            isinstance(value, str)
            and isinstance(source, str)
            and source.startswith("https://")
            and isinstance(expected_hash, str)
            and len(expected_hash) == 64,
            f"manifest {kind} asset is invalid",
        )
        path = (self.root / value).resolve()
        try:
            path.relative_to(self.root)
        except ValueError as exc:
            raise EvaluationError(f"manifest {kind} path is invalid") from exc
        self.require(path.is_file(), f"manifest {kind} asset is unavailable")
        return path

    def validate_asset(self, path: Path, entry: dict[str, Any], kind: str) -> str:
        actual_hash = sha256_file(path)
        self.require(actual_hash == entry["sha256"], f"manifest {kind} hash differs")
        return actual_hash

    def expected_feature(self, model_hash: str, runner_hash: str) -> str:
        parts = [
            model_hash,
            runner_hash,
            RUNTIME_VERSION,
            PREPROCESS_VERSION,
            NORMALIZATION_VERSION,
            str(self.decision["dimension"]),
        ]
        return sha256_bytes("\n".join(parts).encode())

    def run_cli_json(self, name: str, args: list[str], result_path: Path) -> dict[str, Any]:
        raw = self.command_output(name, [str(self.binary), *args])
        try:
            result = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise EvaluationError("CLI response is invalid") from exc
        write_json(result_path, result)
        return result

    def validate_profile(self, profile: Any, expected_name: str | None, expected_model_hash: str) -> str:
        self.require(isinstance(profile, dict), "profile response is invalid")
        prohibited = {"embedding", "path", "audioPath"}
        self.require(not prohibited.intersection(profile), "profile response exposes private data")
        profile_id = profile.get("id")
        self.require(
            isinstance(profile_id, str)
            and profile_id
            and profile.get("modelSha256") == expected_model_hash
            and profile.get("featureVersion") == self.expected_feature_version
            and profile.get("dimension") == self.decision["dimension"],
            "profile fingerprint is invalid",
        )
        if expected_name is not None:
            self.require(profile.get("name") == expected_name, "profile name differs")
        return profile_id

    def validate_identification(
        self,
        result: Any,
        query: dict[str, Any],
        expected_profile_ids: dict[str, str],
        expected_model_hash: str,
    ) -> None:
        self.require(isinstance(result, dict), "identification response is invalid")
        score = result.get("score")
        self.require(
            isinstance(score, (int, float))
            and not isinstance(score, bool)
            and math.isfinite(score)
            and -1 <= score <= 1
            and result.get("modelSha256") == expected_model_hash
            and result.get("featureVersion") == self.expected_feature_version
            and result.get("threshold") == self.decision["threshold"]
            and result.get("margin") == self.decision["margin"],
            "identification fingerprint is invalid",
        )
        candidates = result.get("candidates")
        self.require(isinstance(candidates, list) and len(candidates) <= 3, "identification candidates are invalid")
        if query["expected"] == "matched":
            self.require(result.get("status") == "matched", "known query did not match")
            self.require(
                result.get("profileId") == expected_profile_ids[query["label"]],
                "known query matched a different profile",
            )
            return
        self.require(
            result.get("status") == "unknown" and not result.get("profileId"),
            "unknown query selected a profile",
        )

    def validate_list(self, profiles: Any, expected_ids: dict[str, str], expected_model_hash: str) -> None:
        self.require(isinstance(profiles, list) and len(profiles) == 2, "profile list count differs")
        observed_ids: set[str] = set()
        for profile in profiles:
            observed_ids.add(self.validate_profile(profile, None, expected_model_hash))
        self.require(observed_ids == set(expected_ids.values()), "profile list identifiers differ")

    def count_identification(self, surface: str, query: dict[str, Any]) -> None:
        counts = self.actual_counts[surface]
        if query["expected"] == "matched":
            counts["knownQueriesCompleted"] += 1
            counts["knownProfileIDsCorrect"] += 1
        else:
            counts["unknownQueriesCompleted"] += 1
            counts["unknownRejected"] += 1

    def run_cli(self, enrollment: list[dict[str, Any]], queries: list[dict[str, Any]], model_hash: str) -> None:
        data_dir = self.output / "cli" / "data"
        common = ["--data-dir", str(data_dir)]
        expected_ids: dict[str, str] = {}
        for sample in enrollment:
            result = self.run_cli_json(
                f"cli.enroll.{sample['label']}",
                [
                    "speakers",
                    "enroll",
                    "--file",
                    str(self.resolve_manifest_asset(sample, "sample")),
                    "--name",
                    f"Speaker-{sample['label']}",
                    *common,
                ],
                self.output / "cli" / f"enroll-{sample['label'].lower()}.json",
            )
            expected_ids[sample["label"]] = self.validate_profile(result, f"Speaker-{sample['label']}", model_hash)
            self.actual_counts["cli"]["enrollmentsCompleted"] += 1
        for sample in queries:
            result = self.run_cli_json(
                f"cli.identify.{sample['label']}",
                ["speakers", "identify", "--file", str(self.resolve_manifest_asset(sample, "sample")), *common],
                self.output / "cli" / f"identify-{sample['label']}.json",
            )
            self.validate_identification(result, sample, expected_ids, model_hash)
            self.count_identification("cli", sample)
        profiles = self.run_cli_json("cli.list", ["speakers", "list", *common], self.output / "cli" / "list.json")
        self.validate_list(profiles, expected_ids, model_hash)
        self.actual_counts["cli"]["profilesListed"] = len(profiles)

    def available_port(self) -> int:
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
            listener.bind(("127.0.0.1", 0))
            return int(listener.getsockname()[1])

    def start_server(self) -> None:
        port = self.available_port()
        self.base_url = f"http://127.0.0.1:{port}"
        self.token = secrets.token_urlsafe(32)
        env = os.environ.copy()
        env["YOVOICE_API_TOKEN"] = self.token
        start = time.monotonic()
        try:
            self.server = subprocess.Popen(
                [str(self.binary), "serve", "--listen", f"127.0.0.1:{port}", "--data-dir", str(self.output / "http" / "data")],
                cwd=self.root,
                env=env,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                start_new_session=True,
            )
        except OSError as exc:
            self.record("http.start", start, exit_code="unavailable", failure="unavailable")
            raise EvaluationError("HTTP service is unavailable") from exc
        deadline = time.monotonic() + HTTP_TIMEOUT_SECONDS
        while time.monotonic() < deadline:
            try:
                status, _ = self.http_request("GET", "/v1/status", None, None, record=False)
                if status == 200:
                    self.record("http.ready", start, status=status)
                    return
            except EvaluationError:
                pass
            if self.server.poll() is not None:
                break
            time.sleep(0.1)
        exit_code: int | str = "timeout"
        if self.server is not None and self.server.poll() is not None:
            exit_code = self.server.returncode
        self.record("http.ready", start, exit_code=exit_code, failure="not_ready")
        raise EvaluationError("HTTP service did not become ready")

    def stop_server(self) -> None:
        if self.server is None:
            return
        start = time.monotonic()
        failure: str | None = None
        if self.server.poll() is None:
            self.server.terminate()
            try:
                self.server.wait(timeout=SERVER_STOP_TIMEOUT_SECONDS)
            except subprocess.TimeoutExpired:
                failure = "terminate_timeout"
                try:
                    if os.name == "posix":
                        os.killpg(self.server.pid, signal.SIGKILL)
                    else:
                        self.server.kill()
                    self.server.wait(timeout=SERVER_STOP_TIMEOUT_SECONDS)
                except (OSError, subprocess.TimeoutExpired):
                    failure = "process_group_stop_failed"
        exit_code: int | str = self.server.returncode
        if self.server.poll() is None:
            exit_code = "timeout"
        self.record("http.stop", start, exit_code=exit_code, failure=failure)
        self.server = None
        self.token = ""

    def http_request(
        self,
        method: str,
        path: str,
        body: bytes | None,
        content_type: str | None,
        *,
        record: bool = True,
        action_name: str | None = None,
    ) -> tuple[int, Any]:
        name = action_name or f"http.{method.lower()}.{path.rsplit('/', 1)[-1] or 'speakers'}"
        request = urllib.request.Request(self.base_url + path, data=body, method=method)
        request.add_header("Authorization", "Bearer " + self.token)
        if content_type is not None:
            request.add_header("Content-Type", content_type)
        start = time.monotonic()
        try:
            with urllib.request.urlopen(request, timeout=HTTP_TIMEOUT_SECONDS) as response:
                status = response.status
                raw = response.read()
        except urllib.error.HTTPError as error:
            status = error.code
            raw = error.read()
        except (OSError, urllib.error.URLError) as exc:
            if record:
                self.record(name, start, failure="network_error")
            raise EvaluationError("HTTP request failed") from exc
        if record:
            self.record(name, start, status=status)
        try:
            return status, json.loads(raw)
        except json.JSONDecodeError as exc:
            raise EvaluationError("HTTP response is invalid") from exc

    def run_http(self, enrollment: list[dict[str, Any]], queries: list[dict[str, Any]], model_hash: str) -> None:
        self.start_server()
        expected_ids: dict[str, str] = {}
        try:
            for sample in enrollment:
                path = self.resolve_manifest_asset(sample, "sample")
                status, result = self.http_request(
                    "POST",
                    "/v1/speakers",
                    path.read_bytes(),
                    "application/octet-stream",
                    action_name=f"http.enroll.{sample['label']}",
                )
                write_json(self.output / "http" / f"enroll-{sample['label'].lower()}.json", result)
                self.require(status == 201, "HTTP enrollment status differs")
                profile_id = self.validate_profile(result, None, model_hash)
                self.require(result.get("name") == profile_id, "HTTP default profile name differs")
                expected_ids[sample["label"]] = profile_id
                self.actual_counts["http"]["enrollmentsCompleted"] += 1
            for sample in queries:
                path = self.resolve_manifest_asset(sample, "sample")
                status, result = self.http_request(
                    "POST",
                    "/v1/speakers/identify",
                    path.read_bytes(),
                    "application/octet-stream",
                    action_name=f"http.identify.{sample['label']}",
                )
                write_json(self.output / "http" / f"identify-{sample['label']}.json", result)
                self.require(status == 200, "HTTP identify status differs")
                self.validate_identification(result, sample, expected_ids, model_hash)
                self.count_identification("http", sample)
            status, profiles = self.http_request("GET", "/v1/speakers", None, None, action_name="http.list")
            write_json(self.output / "http" / "list.json", profiles)
            self.require(status == 200, "HTTP list status differs")
            self.validate_list(profiles, expected_ids, model_hash)
            self.actual_counts["http"]["profilesListed"] = len(profiles)
        finally:
            self.stop_server()

    def source_snapshot(self, prefix: str) -> dict[str, Any]:
        head = self.command_output(f"{prefix}.head", ["git", "rev-parse", "HEAD"]).decode().strip()
        diff = self.command_output(
            f"{prefix}.tracked-diff",
            ["git", "diff", "--binary", "HEAD", "--", ".", ":(exclude).sdlc-v1", ":(exclude)artifacts"],
        )
        tracked = self.command_output(
            f"{prefix}.tracked-names", ["git", "diff", "--name-only", "-z", "HEAD"]
        ).decode().split("\0")
        untracked = self.command_output(
            f"{prefix}.untracked-names", ["git", "ls-files", "--others", "--exclude-standard", "-z"]
        ).decode().split("\0")
        names = sorted(
            {
                name
                for name in [*tracked, *untracked]
                if name and not name.startswith(".sdlc-v1/") and not name.startswith("artifacts/")
            }
        )
        files: list[dict[str, str]] = []
        aggregate = hashlib.sha256()
        for name in names:
            path = self.root / name
            self.require(path.is_file(), "business source file is unavailable")
            content_hash = sha256_file(path)
            files.append({"path": name, "sha256": content_hash})
            aggregate.update(name.encode())
            aggregate.update(b"\0")
            aggregate.update(content_hash.encode())
            aggregate.update(b"\n")
        return {
            "head": head,
            "businessTrackedDiffSha256": sha256_bytes(diff),
            "businessFileContentSha256": aggregate.hexdigest(),
            "businessFiles": files,
            "excludedPrefixes": [".sdlc-v1/", "artifacts/"],
        }

    def versions(self) -> dict[str, str]:
        raw_version = self.command_output("metadata.binary-version", [str(self.binary), "--version"])
        try:
            binary_version = json.loads(raw_version)
        except json.JSONDecodeError as exc:
            raise EvaluationError("binary version response is invalid") from exc
        ffmpeg = self.binary.parent / "tools" / "ffmpeg"
        self.require(ffmpeg.is_file() or ffmpeg.is_symlink(), "development FFmpeg is unavailable")
        ffmpeg_line = self.command_output("metadata.ffmpeg-version", [str(ffmpeg), "-version"]).decode().splitlines()
        self.require(bool(ffmpeg_line), "development FFmpeg version is unavailable")
        try:
            sherpa_version = importlib.metadata.version("sherpa-onnx")
            core_version = importlib.metadata.version("sherpa-onnx-core")
            numpy_version = importlib.metadata.version("numpy")
        except importlib.metadata.PackageNotFoundError as exc:
            raise EvaluationError("recognition package version is unavailable") from exc
        self.require(sherpa_version == RUNTIME_VERSION, "sherpa-onnx version differs")
        self.require(core_version == RUNTIME_VERSION, "sherpa-onnx-core version differs")
        return {
            "binary": str(binary_version.get("version", "")),
            "python": sys.version.split()[0],
            "sherpaOnnx": sherpa_version,
            "sherpaOnnxCore": core_version,
            "numpy": numpy_version,
            "developmentFfmpeg": ffmpeg_line[0],
        }

    def artifact_fingerprint(self) -> dict[str, Any]:
        current_manifest = self.read_manifest_file()
        model = current_manifest.get("model")
        samples = current_manifest.get("samples")
        self.require(isinstance(model, dict), "manifest model is invalid")
        self.require(isinstance(samples, list) and len(samples) == 5, "manifest samples are invalid")
        model_path = self.resolve_manifest_asset(model, "model")
        sample_hashes: list[dict[str, str]] = []
        for sample in samples:
            self.require(isinstance(sample, dict), "manifest sample is invalid")
            sample_path = self.resolve_manifest_asset(sample, "sample")
            sample_id = sample.get("id")
            self.require(isinstance(sample_id, str) and sample_id != "", "manifest sample identifier is invalid")
            sample_hashes.append({"id": sample_id, "sha256": sha256_file(sample_path)})
        runner = Path(os.environ["YOVOICE_SPEAKER_RUNNER"])
        self.require(runner.is_file(), "configured runner is unavailable")
        return {
            "binarySha256": sha256_file(self.binary),
            "modelSha256": sha256_file(model_path),
            "runnerSha256": sha256_file(runner),
            "manifestSha256": sha256_file(self.manifest_path),
            "sampleSha256": sample_hashes,
        }

    def run_fingerprint(self, prefix: str) -> dict[str, Any]:
        return {
            "sourceSnapshot": self.source_snapshot(prefix),
            "artifacts": self.artifact_fingerprint(),
            "expectedFeatureVersion": self.expected_feature_version,
        }

    def prepare(self) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
        enrollment, queries = self.load_manifest()
        _, self.model_hash = self.validate_assets([*enrollment, *queries])
        runner_hash = sha256_file(Path(os.environ["YOVOICE_SPEAKER_RUNNER"]))
        self.expected_feature_version = self.expected_feature(self.model_hash, runner_hash)
        self.tools = self.versions()
        self.initial_fingerprint = self.run_fingerprint("before")
        return enrollment, queries

    def clean_private_data(self) -> bool:
        success = True
        for directory in self.data_directories:
            try:
                shutil.rmtree(directory)
            except FileNotFoundError:
                continue
            except OSError:
                success = False
        return success and all(not directory.exists() for directory in self.data_directories)

    def write_summary(self, status: str, started_at: str, started: float, failure: str | None, failure_reason: str | None) -> None:
        summary: dict[str, Any] = {
            "status": status,
            "startedAt": started_at,
            "durationMs": duration_ms(started),
            "runInputBefore": self.initial_fingerprint,
            "runInputAfter": self.final_fingerprint,
            "effectiveConfig": self.decision,
            "tools": self.tools,
            "actions": self.actions,
            "expectedCounts": {"enrollments": 2, "knownQueries": 2, "unknownQueries": 1, "profilesListed": 2},
            "actualCounts": self.actual_counts,
            "dataDirectoriesRemoved": all(not directory.exists() for directory in self.data_directories),
        }
        if failure is not None:
            summary["failure"] = failure
        if failure_reason is not None:
            summary["failureReason"] = failure_reason
        write_json(self.output / "summary.json", summary)

    def run(self) -> int:
        started_at = utc_now()
        started = time.monotonic()
        status = "FAIL"
        failure: str | None = None
        failure_reason: str | None = None
        result = 1
        self.output.mkdir(parents=True)
        try:
            enrollment, queries = self.prepare()
            shutil.copyfile(self.manifest_path, self.output / "eval-manifest.json")
            self.run_cli(enrollment, queries, self.model_hash or "")
            self.run_http(enrollment, queries, self.model_hash or "")
            self.final_fingerprint = self.run_fingerprint("after")
            self.require(self.final_fingerprint == self.initial_fingerprint, "run inputs changed during evaluation")
            status = "PASS"
            result = 0
        except EvaluationError as exc:
            failure = "assertion_failed"
            failure_reason = str(exc)
        except Exception:
            failure = "evaluator_failed"
            failure_reason = "unexpected evaluator failure"
        finally:
            self.stop_server()
            if not self.clean_private_data() and failure is None:
                status = "FAIL"
                failure = "private_cleanup_failed"
                failure_reason = "private data directory remained"
                result = 1
            try:
                self.write_summary(status, started_at, started, failure, failure_reason)
            except Exception:
                write_json(self.output / "summary.json", {"status": "FAIL", "failure": "summary_failed", "actions": self.actions})
                result = 1
        return result


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--manifest", type=Path, required=True)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if args.output.exists():
        print("evaluation output directory already exists", file=sys.stderr)
        return 2
    if not args.binary.is_file() or not os.access(args.binary, os.X_OK):
        print("evaluation binary is unavailable", file=sys.stderr)
        return 2
    for variable in ("YOVOICE_SPEAKER_PYTHON", "YOVOICE_SPEAKER_MODEL", "YOVOICE_SPEAKER_RUNNER"):
        value = os.environ.get(variable, "")
        if not Path(value).is_absolute():
            print(f"{variable} must be an absolute path", file=sys.stderr)
            return 2
    return Evaluation(args.root, args.binary, args.output, args.manifest).run()


if __name__ == "__main__":
    raise SystemExit(main())
