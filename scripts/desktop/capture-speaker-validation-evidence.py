#!/usr/bin/env python3
"""Capture reproducible, public-only evidence for the local speaker-validation bundle."""

from __future__ import annotations

import argparse
import hashlib
import json
import sqlite3
import subprocess
from pathlib import Path
from typing import Any, Iterable


ROOT = Path(__file__).resolve().parents[2]
BUILD_RECEIPT_VERSION = 1
BUILD_RECEIPT_SUFFIX = ".receipt.json"
SENSITIVE_FIELD_NAMES = frozenset({
    "api_key",
    "apikey",
    "key",
    "keymask",
    "modeldirectory",
    "modelpath",
    "path",
    "proxyurl",
    "runtimepath",
    "secret",
    "token",
})
SOURCE_EXCLUDED_PREFIXES = (
    "artifacts/",
    "web/dist/",
    "web/e2e-results/",
    "web/node_modules/",
    "web/playwright-report/",
    "web/test-results/",
)
STATE_DATABASE = "yovoice.db"
STATE_SCHEMA_VERSION = 1
STATE_SETTING_FIELDS = (
    "preferences",
    "runtimePath",
    "runtimeBackend",
    "runtimeVersion",
    "runtimes",
    "activity",
    "modelActivity",
    "aiProviderID",
    "assistantSessions",
    "assistantMemory",
)
STATE_ENTITY_TABLES = (
    ("characters", "characters"),
    ("drafts", "drafts"),
    ("voices", "voices"),
    ("models", "models"),
    ("history", "history"),
    ("ai_providers", "aiProviders"),
)
STATE_TABLES = frozenset({"settings", "private", *(table for table, _ in STATE_ENTITY_TABLES)})


def sha256(path: Path) -> str | None:
    if not path.is_file():
        return None
    digest = hashlib.sha256()
    with path.open("rb") as file:
        for block in iter(lambda: file.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def canonical_bytes(value: Any) -> bytes:
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"), sort_keys=True).encode("utf-8")


def value_sha256(value: Any) -> str:
    return hashlib.sha256(canonical_bytes(value)).hexdigest()


def command_version(command: list[str]) -> dict[str, Any]:
    try:
        output = subprocess.run(command, check=False, capture_output=True, text=True, timeout=20)
    except (OSError, subprocess.TimeoutExpired):
        return {"available": False, "value": None}
    text = (output.stdout or output.stderr).strip()
    return {
        "available": output.returncode == 0,
        "exitCode": output.returncode,
        "value": text.splitlines()[0] if text else None,
    }


def python_runtime(python: Path) -> dict[str, Any]:
    script = """
import importlib.metadata
import json
import sys

versions = {}
for package in (\"sherpa-onnx\", \"sherpa_onnx\"):
    try:
        versions[package] = importlib.metadata.version(package)
    except importlib.metadata.PackageNotFoundError:
        continue
print(json.dumps({\"python\": sys.version.split()[0], \"sherpaOnnx\": versions.get(\"sherpa-onnx\") or versions.get(\"sherpa_onnx\")}))
"""
    try:
        output = subprocess.run(
            [str(python), "-I", "-c", script],
            check=False,
            capture_output=True,
            text=True,
            timeout=20,
        )
    except (OSError, subprocess.TimeoutExpired):
        return {"available": False, "python": None, "sherpaOnnx": None, "expectedSherpaOnnx": "1.13.8", "matchesExpectedSherpaOnnx": False}
    try:
        runtime = json.loads(output.stdout)
    except json.JSONDecodeError:
        runtime = {"python": None, "sherpaOnnx": None}
    sherpa = runtime.get("sherpaOnnx")
    return {
        "available": output.returncode == 0,
        "python": runtime.get("python"),
        "sherpaOnnx": sherpa,
        "expectedSherpaOnnx": "1.13.8",
        "matchesExpectedSherpaOnnx": sherpa == "1.13.8",
    }


def redacted_business_value(value: Any, field_name: str | None = None) -> Any:
    field = (field_name or "").replace("_", "").lower()
    if field in SENSITIVE_FIELD_NAMES or "embedding" in field or "path" in field or "token" in field or "secret" in field:
        return {"redacted": True, "sha256": value_sha256(value)}
    if isinstance(value, dict):
        return {key: redacted_business_value(item, key) for key, item in sorted(value.items())}
    if isinstance(value, list):
        return [redacted_business_value(item) for item in value]
    return value


def normalized_state(data_root: Path) -> dict[str, Any]:
    """Return a public comparison snapshot from SQLite, or legacy JSON when SQLite is absent."""
    state_path = data_root / "state.json"
    database_path = data_root / STATE_DATABASE
    policy = {
        "volatileFieldsExcluded": [],
        "redactedFieldNames": sorted(SENSITIVE_FIELD_NAMES | {"embedding"}),
    }
    if database_path.exists():
        state = read_sqlite_state(database_path)
        source = "sqlite"
    elif state_path.is_file():
        try:
            state = json.loads(state_path.read_text())
        except (OSError, json.JSONDecodeError) as error:
            raise ValueError("legacy State file cannot be read") from error
        if not isinstance(state, dict):
            raise ValueError("legacy State file must be a JSON object")
        source = "legacy-json"
    else:
        return {"present": False, "comparisonPolicy": policy, "voices": []}

    voices = state.get("voices", [])
    if not isinstance(voices, list):
        raise ValueError("State voices must be a list")
    return {
        "present": True,
        "source": source,
        "comparisonPolicy": policy,
        "canonicalStateSha256": value_sha256(state),
        "businessState": redacted_business_value(state),
        "voices": redacted_business_value(voices),
    }


def read_sqlite_state(database_path: Path) -> dict[str, Any]:
    """Read the complete persisted State through SQLite's read-only URI mode.

    The app's private table intentionally is never queried. A malformed, unsupported,
    or incomplete database is evidence of an invalid capture and must not silently fall
    back to an adjacent legacy state.json file.
    """
    try:
        uri = f"{database_path.resolve().as_uri()}?mode=ro"
        connection = sqlite3.connect(uri, uri=True)
        try:
            connection.execute("PRAGMA query_only = ON")
            # sqlite3 only holds a snapshot across several SELECT statements inside an
            # explicit transaction. State is split over tables, so one capture must not
            # combine rows from before and after a concurrent app commit.
            connection.execute("BEGIN")
            validate_state_database(connection)
            state = read_state_rows(connection)
            connection.rollback()
        finally:
            connection.close()
    except (OSError, sqlite3.DatabaseError, json.JSONDecodeError, ValueError) as error:
        if isinstance(error, ValueError):
            raise
        raise ValueError("SQLite State database cannot be read") from error
    return state


def validate_state_database(connection: sqlite3.Connection) -> None:
    version = connection.execute("PRAGMA user_version").fetchone()
    if version is None or version[0] != STATE_SCHEMA_VERSION:
        found = None if version is None else version[0]
        raise ValueError(f"unsupported SQLite State schema version: {found}")

    quick_check = connection.execute("PRAGMA quick_check").fetchone()
    if quick_check is None or quick_check[0] != "ok":
        raise ValueError("SQLite State integrity check failed")

    names = {
        row[0]
        for row in connection.execute(
            "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'"
        )
    }
    if names != STATE_TABLES:
        raise ValueError("SQLite State table layout is unsupported")

    validate_table_columns(connection, "settings", ("key", "pos", "data"))
    validate_table_columns(connection, "private", ("key", "data"))
    for table, _ in STATE_ENTITY_TABLES:
        validate_table_columns(connection, table, ("key", "pos", "data"))


def validate_table_columns(connection: sqlite3.Connection, table: str, expected: tuple[str, ...]) -> None:
    columns = tuple(row[1] for row in connection.execute(f"PRAGMA table_info({table})"))
    if columns != expected:
        raise ValueError(f"SQLite State table {table} has an unsupported layout")


def read_state_rows(connection: sqlite3.Connection) -> dict[str, Any]:
    settings = read_json_rows(connection, "settings")
    setting_values = {key: value for key, value in settings}
    if set(setting_values) != set(STATE_SETTING_FIELDS):
        raise ValueError("SQLite State settings are incomplete or unsupported")

    state: dict[str, Any] = {
        "characters": [],
        "previews": [],
        "drafts": [],
        "voices": [],
        "models": [],
        "history": [],
        "preferences": setting_values["preferences"],
        "runtimePath": setting_values["runtimePath"],
        "runtimeBackend": setting_values["runtimeBackend"],
        "runtimeVersion": setting_values["runtimeVersion"],
        "runtimes": setting_values["runtimes"],
        "activity": setting_values["activity"],
        "modelActivity": setting_values["modelActivity"],
        "aiProviders": [],
        "aiProviderID": setting_values["aiProviderID"],
        "assistantSessions": setting_values["assistantSessions"],
        "assistantMemory": setting_values["assistantMemory"],
    }
    for table, field in STATE_ENTITY_TABLES:
        state[field] = [value for _, value in read_json_rows(connection, table)]
    return state


def read_json_rows(connection: sqlite3.Connection, table: str) -> list[tuple[str, Any]]:
    rows = connection.execute(f"SELECT key, data FROM {table} ORDER BY pos, key")
    decoded: list[tuple[str, Any]] = []
    for key, data in rows:
        if not isinstance(key, str) or not isinstance(data, str):
            raise ValueError(f"SQLite State table {table} contains an invalid row")
        try:
            decoded.append((key, json.loads(data)))
        except json.JSONDecodeError as error:
            raise ValueError(f"SQLite State table {table} contains invalid JSON") from error
    return decoded


def public_profiles(data_root: Path) -> list[dict[str, Any]]:
    path = data_root / "speakers.json"
    if not path.is_file():
        return []
    database = json.loads(path.read_text())
    return [
        {
            "id": profile.get("id"),
            "name": profile.get("name"),
            "createdAt": profile.get("createdAt"),
            "bindVoiceId": profile.get("bindVoiceId"),
            "modelSha256": profile.get("modelSha256"),
            "featureVersion": profile.get("featureVersion"),
            "dimension": profile.get("dimension"),
        }
        for profile in database.get("profiles", [])
    ]


def file_manifest(entries: Iterable[tuple[str, Path]]) -> dict[str, Any]:
    files = [
        {"path": relative, "sha256": sha256(path)}
        for relative, path in sorted(entries, key=lambda entry: entry[0])
        if path.is_file()
    ]
    return {"fileCount": len(files), "sha256": value_sha256(files), "files": files}


def tree_manifest(root: Path) -> dict[str, Any]:
    if not root.is_dir():
        return {"present": False, "fileCount": 0, "sha256": value_sha256([]), "files": []}
    entries = ((path.relative_to(root).as_posix(), path) for path in root.rglob("*") if path.is_file())
    return {"present": True, **file_manifest(entries)}


def build_package_fingerprints(app: Path) -> dict[str, Any]:
    """Return the package parts that a smoke run must bind to its source receipt.

    The receipt is a sidecar beside the signed app, so the complete final app tree
    can be hashed without introducing a self-referential signature cycle. Keep the
    executable and web fingerprints separately to make a mismatch actionable.
    """
    return {
        "appBundle": tree_manifest(app),
        "hostSha256": sha256(app / "Contents/MacOS/VoiceWorkbenchMac"),
        "serviceSha256": sha256(app / "Contents/Resources/service/yovoice-service"),
        "webResources": tree_manifest(app / "Contents/Resources/web"),
    }


def build_receipt_path(app: Path) -> Path:
    """Keep the receipt beside the signed app so its final code signature stays stable."""
    return app.parent / f"{app.name}{BUILD_RECEIPT_SUFFIX}"


def source_candidates() -> dict[str, Any]:
    try:
        output = subprocess.run(
            ["git", "ls-files", "--cached", "--others", "--exclude-standard"],
            cwd=ROOT,
            check=False,
            capture_output=True,
            text=True,
            timeout=20,
        )
    except (OSError, subprocess.TimeoutExpired):
        output = None

    if output and output.returncode == 0:
        paths = [Path(line) for line in output.stdout.splitlines() if line]
    else:
        paths = [path.relative_to(ROOT) for path in ROOT.rglob("*") if path.is_file()]
    entries = (
        (path.as_posix(), ROOT / path)
        for path in paths
        if not path.as_posix().startswith(SOURCE_EXCLUDED_PREFIXES)
    )
    return file_manifest(entries)


def valid_file_manifest(value: Any) -> bool:
    if not isinstance(value, dict):
        return False
    if set(value) != {"fileCount", "sha256", "files"}:
        return False
    files = value.get("files")
    if not isinstance(files, list) or value.get("fileCount") != len(files):
        return False
    if not isinstance(value.get("sha256"), str) or value_sha256(files) != value["sha256"]:
        return False

    for entry in files:
        if not isinstance(entry, dict) or set(entry) != {"path", "sha256"}:
            return False
        path = entry.get("path")
        digest = entry.get("sha256")
        if not isinstance(path, str) or not path or path.startswith("/") or ".." in Path(path).parts:
            return False
        if not isinstance(digest, str) or len(digest) != 64:
            return False
    return True


def valid_tree_manifest(value: Any) -> bool:
    if not isinstance(value, dict):
        return False
    if value.get("present") is not True:
        return False
    manifest = {key: value.get(key) for key in ("fileCount", "sha256", "files")}
    return valid_file_manifest(manifest)


def build_receipt(source_before: dict[str, Any], app: Path) -> dict[str, Any]:
    """Create a receipt only when the candidate did not change during a build."""
    source_after = source_candidates()
    if not valid_file_manifest(source_before):
        raise ValueError("source manifest has an invalid format")
    if source_before != source_after:
        raise ValueError("source candidate changed during bundle build")

    package = build_package_fingerprints(app)
    if not isinstance(package["hostSha256"], str):
        raise ValueError("bundle host is missing")
    if not isinstance(package["serviceSha256"], str):
        raise ValueError("bundle service is missing")
    if not valid_tree_manifest(package["appBundle"]):
        raise ValueError("bundle app tree is missing or invalid")
    if not valid_tree_manifest(package["webResources"]):
        raise ValueError("bundle web resources are missing or invalid")

    return {
        "version": BUILD_RECEIPT_VERSION,
        "source": {
            "before": source_before,
            "after": source_after,
            "stable": True,
        },
        "package": package,
    }


def write_source_manifest(output: Path) -> dict[str, Any]:
    manifest = source_candidates()
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
    return manifest


def write_build_receipt(output: Path, source_before_path: Path, app: Path) -> dict[str, Any]:
    source_before = json.loads(source_before_path.read_text())
    receipt = build_receipt(source_before, app)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(receipt, ensure_ascii=False, indent=2) + "\n")
    return receipt


def verify_build_receipt(receipt_path: Path, app: Path) -> dict[str, Any]:
    """Validate a receipt without exposing local paths or invoking the app host."""
    if not receipt_path.is_file():
        raise ValueError("build receipt is missing")
    try:
        receipt = json.loads(receipt_path.read_text())
    except (OSError, json.JSONDecodeError) as error:
        raise ValueError("build receipt cannot be read") from error

    if not isinstance(receipt, dict) or receipt.get("version") != BUILD_RECEIPT_VERSION:
        raise ValueError("build receipt version is unsupported")
    source = receipt.get("source")
    if not isinstance(source, dict) or set(source) != {"before", "after", "stable"}:
        raise ValueError("build receipt source section is invalid")
    if source.get("stable") is not True:
        raise ValueError("build receipt source is not stable")
    if not valid_file_manifest(source.get("before")) or not valid_file_manifest(source.get("after")):
        raise ValueError("build receipt source manifest is invalid")
    current_source = source_candidates()
    if source["before"] != source["after"] or source["after"] != current_source:
        raise ValueError("build receipt source does not match the current candidate")

    package = receipt.get("package")
    expected_package_keys = {"appBundle", "hostSha256", "serviceSha256", "webResources"}
    if not isinstance(package, dict) or set(package) != expected_package_keys:
        raise ValueError("build receipt package section is invalid")
    if not isinstance(package.get("hostSha256"), str) or not isinstance(package.get("serviceSha256"), str):
        raise ValueError("build receipt executable fingerprint is invalid")
    if not valid_tree_manifest(package.get("appBundle")):
        raise ValueError("build receipt app fingerprint is invalid")
    if not valid_tree_manifest(package.get("webResources")):
        raise ValueError("build receipt web fingerprint is invalid")

    actual_package = build_package_fingerprints(app)
    if package["appBundle"] != actual_package["appBundle"]:
        raise ValueError("build receipt app fingerprint does not match")
    if package["hostSha256"] != actual_package["hostSha256"]:
        raise ValueError("build receipt host fingerprint does not match")
    if package["serviceSha256"] != actual_package["serviceSha256"]:
        raise ValueError("build receipt service fingerprint does not match")
    if package["webResources"] != actual_package["webResources"]:
        raise ValueError("build receipt web fingerprint does not match")
    return receipt


def recognition_manifest(manifest_path: Path, model_path: Path, runner_path: Path) -> dict[str, Any]:
    document = json.loads(manifest_path.read_text())
    expected_model_sha = document.get("model", {}).get("sha256")
    actual_model_sha = sha256(model_path)
    samples = []
    for sample in document.get("samples", []):
        path = ROOT / sample["path"]
        expected_sha = sample.get("sha256")
        actual_sha = sha256(path)
        samples.append({
            "id": sample.get("id"),
            "expectedSha256": expected_sha,
            "actualSha256": actual_sha,
            "matchesExpected": expected_sha == actual_sha,
        })
    checks = [expected_model_sha == actual_model_sha, *(sample["matchesExpected"] for sample in samples)]
    return {
        "manifestSha256": sha256(manifest_path),
        "configuredModel": {
            "expectedSha256": expected_model_sha,
            "actualSha256": actual_model_sha,
            "matchesExpected": expected_model_sha == actual_model_sha,
        },
        "runnerSha256": sha256(runner_path),
        "samples": samples,
        "decision": document.get("decision", {}),
        "allExpectedHashesMatch": all(checks),
    }


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--phase")
    parser.add_argument("--output", type=Path)
    parser.add_argument("--app", type=Path)
    parser.add_argument("--data-root", type=Path)
    parser.add_argument("--ffmpeg", type=Path)
    parser.add_argument("--python", type=Path)
    parser.add_argument("--model", type=Path)
    parser.add_argument("--runner", type=Path)
    parser.add_argument("--manifest", type=Path)
    parser.add_argument("--write-source-manifest", type=Path)
    parser.add_argument("--write-build-receipt", type=Path)
    parser.add_argument("--source-before", type=Path)
    parser.add_argument("--verify-build-receipt", action="store_true")
    parser.add_argument("--print-build-receipt-path", action="store_true")
    parser.add_argument("--receipt", type=Path)
    args = parser.parse_args()

    modes = sum((
        args.write_source_manifest is not None,
        args.write_build_receipt is not None,
        args.verify_build_receipt,
        args.print_build_receipt_path,
    ))
    if modes > 1:
        parser.error("receipt actions cannot be combined")
    if args.write_source_manifest:
        write_source_manifest(args.write_source_manifest)
        return
    if args.write_build_receipt:
        if not args.source_before or not args.app:
            parser.error("--write-build-receipt requires --source-before and --app")
        write_build_receipt(args.write_build_receipt, args.source_before, args.app)
        return
    if args.verify_build_receipt:
        if not args.app:
            parser.error("--verify-build-receipt requires --app")
        receipt_path = args.receipt or build_receipt_path(args.app)
        try:
            verify_build_receipt(receipt_path, args.app)
        except ValueError as error:
            raise SystemExit(str(error)) from error
        return
    if args.print_build_receipt_path:
        if not args.app:
            parser.error("--print-build-receipt-path requires --app")
        print(build_receipt_path(args.app))
        return

    required_capture_args = {
        "--phase": args.phase,
        "--output": args.output,
        "--app": args.app,
        "--data-root": args.data_root,
        "--ffmpeg": args.ffmpeg,
        "--python": args.python,
        "--model": args.model,
        "--runner": args.runner,
        "--manifest": args.manifest,
    }
    missing = [flag for flag, value in required_capture_args.items() if value is None]
    if missing:
        parser.error(f"capture requires {' '.join(missing)}")

    app = args.app
    files = {
        "host": app / "Contents/MacOS/VoiceWorkbenchMac",
        "service": app / "Contents/Resources/service/yovoice-service",
        "infoPlist": app / "Contents/Info.plist",
        "ffmpegWrapper": app / "Contents/Resources/tools/ffmpeg",
        "externalFfmpeg": args.ffmpeg,
        "recognitionPython": args.python,
        "recognitionModel": args.model,
        "recognitionRunner": args.runner,
        "recognitionManifest": args.manifest,
    }
    for sample in sorted((ROOT / "artifacts/recognition/samples").glob("*.wav")):
        files[f"sample:{sample.stem}"] = sample

    report = {
        "phase": args.phase,
        "files": {name: {"present": path.exists(), "sha256": sha256(path)} for name, path in files.items()},
        "sourceCandidate": source_candidates(),
        "appBundle": tree_manifest(app),
        "webResources": tree_manifest(app / "Contents/Resources/web"),
        "fixedAssets": file_manifest((name, path) for name, path in files.items()),
        "recognitionManifest": recognition_manifest(args.manifest, args.model, args.runner),
        "versions": {
            "ffmpeg": command_version([str(args.ffmpeg), "-version"]),
            "go": command_version(["go", "version"]),
            "node": command_version(["node", "--version"]),
            "python": python_runtime(args.python),
            "swift": command_version(["swift", "--version"]),
        },
        "normalizedState": normalized_state(args.data_root),
        "publicProfiles": public_profiles(args.data_root),
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")


if __name__ == "__main__":
    main()
