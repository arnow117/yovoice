#!/usr/bin/env python3
"""静态检查声纹验证包脚本的安全边界，不运行构建或修改任何外部工具。"""

import importlib.util
import json
import os
from pathlib import Path
import signal
import sqlite3
import subprocess
from tempfile import TemporaryDirectory
import time
import unittest


ROOT = Path(__file__).resolve().parents[2]
BUILD = (ROOT / "scripts/desktop/build-speaker-validation-macos.sh").read_text()
PLIST = (ROOT / "scripts/desktop/speaker-validation-info.plist").read_text()
SMOKE = (ROOT / "scripts/desktop/smoke-speaker-validation-macos.sh").read_text()
CAPTURE_PATH = ROOT / "scripts/desktop/capture-speaker-validation-evidence.py"
PATHS_PATH = ROOT / "scripts/desktop/speaker-validation-paths.py"
WRAPPER_PATH = ROOT / "scripts/desktop/speaker-validation-wrapper.py"
HOST_MAIN = (ROOT / "desktop/macos/Sources/VoiceWorkbenchMac/main.swift").read_text()


def capture_module():
    spec = importlib.util.spec_from_file_location("speaker_validation_capture", CAPTURE_PATH)
    if spec is None or spec.loader is None:
        raise RuntimeError("cannot load speaker validation evidence capture")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def paths_module():
    spec = importlib.util.spec_from_file_location("speaker_validation_paths", PATHS_PATH)
    if spec is None or spec.loader is None:
        raise RuntimeError("cannot load speaker validation paths")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def wrapper_module():
    spec = importlib.util.spec_from_file_location("speaker_validation_wrapper", WRAPPER_PATH)
    if spec is None or spec.loader is None:
        raise RuntimeError("cannot load speaker validation wrapper")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def write_fixture_receipt(app: Path) -> Path:
    capture = capture_module()
    web_root = app / "Contents/Resources/web"
    web_root.mkdir(parents=True, exist_ok=True)
    (web_root / "index.html").write_text("fixture web resource")
    receipt_path = capture.build_receipt_path(app)
    receipt = capture.build_receipt(capture.source_candidates(), app)
    receipt_path.write_text(json.dumps(receipt))
    return receipt_path


def write_state_database(path: Path, preference_locale: str, draft_id: str) -> None:
    connection = sqlite3.connect(path)
    connection.executescript("""
        CREATE TABLE settings (key TEXT PRIMARY KEY, pos INTEGER NOT NULL, data TEXT NOT NULL);
        CREATE TABLE characters (key TEXT PRIMARY KEY, pos INTEGER NOT NULL, data TEXT NOT NULL);
        CREATE TABLE drafts (key TEXT PRIMARY KEY, pos INTEGER NOT NULL, data TEXT NOT NULL);
        CREATE TABLE voices (key TEXT PRIMARY KEY, pos INTEGER NOT NULL, data TEXT NOT NULL);
        CREATE TABLE models (key TEXT PRIMARY KEY, pos INTEGER NOT NULL, data TEXT NOT NULL);
        CREATE TABLE history (key TEXT PRIMARY KEY, pos INTEGER NOT NULL, data TEXT NOT NULL);
        CREATE TABLE ai_providers (key TEXT PRIMARY KEY, pos INTEGER NOT NULL, data TEXT NOT NULL);
        CREATE TABLE private (key TEXT PRIMARY KEY, data TEXT NOT NULL);
        PRAGMA user_version = 1;
    """)
    settings = {
        "preferences": {"uiLocale": preference_locale},
        "runtimePath": None,
        "runtimeBackend": "cpu",
        "runtimeVersion": None,
        "runtimes": None,
        "activity": None,
        "modelActivity": None,
        "aiProviderID": "",
        "assistantSessions": None,
        "assistantMemory": [],
    }
    for key, value in settings.items():
        connection.execute("INSERT INTO settings (key, pos, data) VALUES (?, 0, ?)", (key, json.dumps(value)))
    connection.execute("INSERT INTO drafts (key, pos, data) VALUES (?, 0, ?)", (draft_id, json.dumps({"id": draft_id})))
    connection.commit()
    connection.close()


class SpeakerValidationBuildChecks(unittest.TestCase):
    def test_rejects_running_target_before_replacement(self):
        self.assertIn("请先退出声纹验证包", BUILD)
        self.assertLess(
            BUILD.index("请先退出声纹验证包"),
            BUILD.index('if [[ -e "$output" || -e "$receipt_output" ]]'),
        )

    def test_external_ffmpeg_is_only_wrapped(self):
        self.assertIn("speaker-validation-wrapper.py", BUILD)
        self.assertNotIn('exec "$ffmpeg" "\\$@"', BUILD)
        self.assertNotIn("codesign --force --sign - \"$ffmpeg\"", BUILD)
        self.assertNotIn("rm -rf /opt/homebrew", BUILD)

    def test_external_ffmpeg_wrapper_executes_special_paths_as_literals(self):
        with TemporaryDirectory(prefix="speaker-ffmpeg-wrapper-") as directory:
            root = Path(directory)
            marker = root / "unexpected-command"
            executable = root / "ffmpeg with space $dollar 'single' \"double\" `backtick` $(touch unexpected-command)"
            legacy_wrapper = root / "legacy-ffmpeg-wrapper"
            wrapper = root / "ffmpeg-wrapper"
            arguments = root / "arguments.json"
            executable.write_text(
                "#!/usr/bin/env python3\n"
                "import json\n"
                "from pathlib import Path\n"
                "import sys\n"
                "Path(sys.argv[1]).write_text(json.dumps(sys.argv[2:]))\n",
            )
            executable.chmod(0o755)

            legacy_wrapper.write_text(f'#!/bin/sh\nexec "{executable}" "$@"\n')
            legacy_wrapper.chmod(0o755)
            legacy = subprocess.run(
                [str(legacy_wrapper), str(arguments), "argument with spaces"],
                cwd=root,
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertTrue(marker.exists(), legacy.stderr)
            self.assertFalse(arguments.exists(), legacy.stdout)
            marker.unlink()

            wrapper_module().write_wrapper(wrapper, executable)
            subprocess.run(
                [str(wrapper), str(arguments), "argument with spaces", "$literal", "`literal`"],
                cwd=root,
                check=True,
            )

            self.assertFalse(marker.exists())
            self.assertEqual(
                json.loads(arguments.read_text()),
                ["argument with spaces", "$literal", "`literal`"],
            )

    def test_build_normalizes_runtime_and_validation_paths_before_plist_write(self):
        self.assertIn('output=$(absolute_path "$output")', BUILD)
        self.assertIn('data_root=$(absolute_path "$data_root")', BUILD)
        self.assertIn('YOVOICE_SPEAKER_PYTHON=$(absolute_path "$YOVOICE_SPEAKER_PYTHON")', BUILD)
        self.assertIn('YOVOICE_SPEAKER_MODEL=$(absolute_path "$YOVOICE_SPEAKER_MODEL")', BUILD)
        self.assertIn('YOVOICE_SPEAKER_RUNNER=$(absolute_path "$YOVOICE_SPEAKER_RUNNER")', BUILD)
        self.assertIn('ffmpeg=$(absolute_path "$ffmpeg")', BUILD)
        self.assertLess(BUILD.index('output=$(absolute_path "$output")'), BUILD.index('YOVOICE_VALIDATION_DATA_DIRECTORY="$data_root"'))

    def test_path_normalization_keeps_relative_spaces_and_venv_python_entrypoint(self):
        paths = paths_module()
        with TemporaryDirectory(prefix="speaker validation ") as directory:
            root = Path(directory)
            venv_python = root / "recognition venv" / "bin" / "python"
            venv_python.parent.mkdir(parents=True)
            target = root / "runtime" / "python3"
            target.parent.mkdir()
            target.write_text("python")
            venv_python.symlink_to(target)

            normalized = paths.absolute_path(str(root), "recognition venv/bin/python")

        self.assertEqual(normalized, str(venv_python))
        self.assertNotEqual(normalized, str(target))

    def test_build_uses_the_existing_local_web_toolchain(self):
        self.assertIn("./node_modules/.bin/tsc -b", BUILD)
        self.assertIn("./node_modules/.bin/vite build", BUILD)
        self.assertNotIn("pnpm --dir web run build", BUILD)

    def test_final_signing_precedes_sidecar_receipt_and_publication(self):
        service = BUILD.index('codesign --force --sign - "$resources/service/yovoice-service"')
        host = BUILD.index('codesign --force --sign - --entitlements')
        bundle = BUILD.index('codesign --force --sign - "$app"')
        strict_verify = BUILD.index('codesign --verify --deep --strict "$app"')
        receipt = BUILD.index('receipt="$staging/speaker-validation-build-receipt.json"')
        receipt_publication = BUILD.index('mv "$receipt" "$receipt_output"')
        app_publication = BUILD.index('mv "$app" "$output"')
        self.assertLess(service, host)
        self.assertLess(host, bundle)
        self.assertLess(bundle, strict_verify)
        self.assertLess(strict_verify, receipt)
        self.assertLess(receipt, receipt_publication)
        self.assertLess(receipt_publication, app_publication)
        self.assertIn("--print-build-receipt-path", BUILD)
        self.assertNotIn('speaker-validation-build-receipt.json"', SMOKE)
        self.assertIn("--print-build-receipt-path", SMOKE)

        with TemporaryDirectory(prefix="speaker-receipt-path-") as directory:
            app = Path(directory) / "nested" / "fixture.app"
            self.assertEqual(
                capture_module().build_receipt_path(app),
                app.parent / "fixture.app.receipt.json",
            )

    def test_final_bundle_signature_changes_are_bound_after_signing(self):
        capture = capture_module()
        with TemporaryDirectory(prefix="speaker-final-signature-") as directory:
            root = Path(directory)
            app = root / "fixture.app"
            host = app / "Contents/MacOS/VoiceWorkbenchMac"
            service = app / "Contents/Resources/service/yovoice-service"
            web = app / "Contents/Resources/web/index.html"
            host.parent.mkdir(parents=True)
            service.parent.mkdir(parents=True)
            web.parent.mkdir(parents=True)
            host.write_text("host before bundle signature")
            service.write_text("signed service")
            web.write_text("signed web")

            # The final bundle signature changes host bytes after the host itself
            # has been signed. Build the sidecar only after that final mutation.
            host.write_text("host after final bundle signature")
            receipt_path = capture.build_receipt_path(app)
            receipt = capture.build_receipt(capture.source_candidates(), app)
            receipt_path.write_text(json.dumps(receipt))

            capture.verify_build_receipt(receipt_path, app)
            host.write_text("host after an unexpected additional signature")
            with self.assertRaisesRegex(ValueError, "app fingerprint does not match"):
                capture.verify_build_receipt(receipt_path, app)

    def test_validation_plist_has_only_declared_runtime_configuration(self):
        self.assertIn("YovoiceSpeakerValidation", PLIST)
        self.assertIn("YovoiceValidationDataDirectory", PLIST)
        self.assertNotIn("YovoiceSpeakerRuntime", PLIST)

    def test_smoke_uses_its_own_data_and_preserves_failed_evidence(self):
        self.assertIn('WORKBENCH_DATA="$data_root"', SMOKE)
        self.assertIn('WORKBENCH_SMOKE_SCRIPT="$script"', SMOKE)
        self.assertIn('拒绝覆盖既有证据', SMOKE)
        self.assertIn('python3 "$repo/scripts/desktop/capture-speaker-validation-evidence.py"', SMOKE)
        self.assertNotIn("--prepare-data", SMOKE)
        self.assertNotIn("/api/call", SMOKE)

    def test_smoke_normalizes_paths_and_stops_after_a_phase_failure(self):
        self.assertIn('result_root=$(absolute_path "$result_root")', SMOKE)
        self.assertIn('require_phase_success "$label"', SMOKE)
        self.assertIn('preserve_phase_result "$label"', SMOKE)
        self.assertIn('wait_for_phase_completion', SMOKE)
        self.assertIn('wait_for_owned_exit', SMOKE)
        self.assertIn('kill -KILL "$app_pid"', SMOKE)
        self.assertLess(SMOKE.index('preserve_phase_result "$label"'), SMOKE.index('require_phase_success "$label"'))
        self.assertLess(SMOKE.index('require_phase_success "$label"'), SMOKE.index('wait_for_phase_completion "$label"'))
        self.assertLess(SMOKE.index('wait_for_phase_completion "$label"'), SMOKE.index('mv "$data_root/smoke-result.json"'))

    def test_smoke_passes_normalized_runtime_values_to_the_host(self):
        smoke = ROOT / "scripts/desktop/smoke-speaker-validation-macos.sh"

        with TemporaryDirectory(prefix="speaker-runtime-probe-") as directory:
            root = Path(directory)
            app = root / "fixture.app"
            host = app / "Contents/MacOS/VoiceWorkbenchMac"
            service = app / "Contents/Resources/service/yovoice-service"
            runtime = root / "runtime with spaces"
            python_bin = runtime / "venv/bin/python"
            model = runtime / "models/model.onnx"
            runner = runtime / "scripts/runner.py"
            ffmpeg = runtime / "tools/ffmpeg"
            for path in (python_bin, model, runner, ffmpeg):
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("#!/bin/sh\nexit 0\n")
            for path in (python_bin, ffmpeg):
                path.chmod(0o755)
            host.parent.mkdir(parents=True)
            service.parent.mkdir(parents=True)
            service.write_text("#!/bin/sh\nexit 0\n")
            service.chmod(0o755)
            host.write_text("""#!/bin/bash
set -euo pipefail
mkdir -p "$WORKBENCH_DATA"
python3 - "$WORKBENCH_DATA/received-runtime.json" <<'PY'
import json
import os
import sys

keys = [
    "YOVOICE_SPEAKER_PYTHON",
    "YOVOICE_SPEAKER_MODEL",
    "YOVOICE_SPEAKER_RUNNER",
    "YOVOICE_SPEAKER_THRESHOLD",
    "YOVOICE_SPEAKER_MARGIN",
    "YOVOICE_SPEAKER_TIMEOUT",
]
with open(sys.argv[1], "w") as handle:
    json.dump({key: os.environ.get(key) for key in keys}, handle)
PY
printf '%s\\n' '{"failure":"fixture stops after received environment"}' > "$WORKBENCH_DATA/smoke-result.json"
printf 'fixture screenshot' > "$WORKBENCH_DATA/smoke.png"
""")
            host.chmod(0o755)
            write_fixture_receipt(app)
            result_root = root / "result"
            relative = lambda path: os.path.relpath(path, ROOT)
            environment = {
                **os.environ,
                "YOVOICE_SPEAKER_VALIDATION_APP": str(app),
                "YOVOICE_SPEAKER_VALIDATION_SMOKE_OUTPUT": str(result_root),
                "YOVOICE_SPEAKER_FFMPEG": relative(ffmpeg),
                "YOVOICE_SPEAKER_PYTHON": relative(python_bin),
                "YOVOICE_SPEAKER_MODEL": relative(model),
                "YOVOICE_SPEAKER_RUNNER": relative(runner),
                "YOVOICE_SPEAKER_THRESHOLD": "0.61",
                "YOVOICE_SPEAKER_MARGIN": "0.04",
                "YOVOICE_SPEAKER_TIMEOUT": "119s",
                "YOVOICE_SPEAKER_SMOKE_TIMEOUT": "3",
            }

            completed = subprocess.run(
                ["bash", str(smoke)],
                cwd=ROOT,
                env=environment,
                capture_output=True,
                text=True,
                check=False,
            )

            self.assertNotEqual(completed.returncode, 0, completed.stdout + completed.stderr)
            self.assertIn("fixture stops after received environment", completed.stderr)
            received = json.loads((result_root / "data" / "received-runtime.json").read_text())
            self.assertEqual(received, {
                "YOVOICE_SPEAKER_PYTHON": str(python_bin),
                "YOVOICE_SPEAKER_MODEL": str(model),
                "YOVOICE_SPEAKER_RUNNER": str(runner),
                "YOVOICE_SPEAKER_THRESHOLD": "0.61",
                "YOVOICE_SPEAKER_MARGIN": "0.04",
                "YOVOICE_SPEAKER_TIMEOUT": "119s",
            })

    def test_smoke_bounds_hung_hosts_and_preserves_received_results(self):
        smoke = ROOT / "scripts/desktop/smoke-speaker-validation-macos.sh"

        for outcome, expected_text in (("failure", "fixture phase failure"), ("success", "未完成截图和退出")):
            with self.subTest(outcome=outcome), TemporaryDirectory(prefix="speaker-smoke-hang-") as directory:
                root = Path(directory)
                model = root / "model.onnx"
                runner = root / "runner.py"
                python_bin = root / "python"
                ffmpeg = root / "ffmpeg"
                app = root / "fixture.app"
                host = app / "Contents/MacOS/VoiceWorkbenchMac"
                service = app / "Contents/Resources/service/yovoice-service"
                model.write_text("fixture model")
                runner.write_text("fixture runner")
                for executable in (python_bin, ffmpeg):
                    executable.write_text("#!/bin/sh\nexit 0\n")
                    executable.chmod(0o755)
                host.parent.mkdir(parents=True)
                service.parent.mkdir(parents=True)
                service.write_text("#!/bin/sh\nexit 0\n")
                service.chmod(0o755)
                host.write_text("""#!/bin/bash
set -euo pipefail
mkdir -p \"$WORKBENCH_DATA\"
printf '%s\\n' \"$$\" > \"$SPEAKER_TEST_PID_FILE\"
if [[ \"$SPEAKER_TEST_OUTCOME\" == failure ]]; then
  printf '%s\\n' '{\"failure\":\"fixture phase failure\"}' > \"$WORKBENCH_DATA/smoke-result.json\"
else
  printf '%s\\n' '{\"initialized\":true}' > \"$WORKBENCH_DATA/smoke-result.json\"
fi
printf 'fixture screenshot' > \"$WORKBENCH_DATA/smoke.png\"
trap '' TERM
while :; do sleep 1; done
""")
                host.chmod(0o755)
                write_fixture_receipt(app)
                result_root = root / f"result-{outcome}"
                pid_file = root / "host.pid"
                environment = {
                    **os.environ,
                    "YOVOICE_SPEAKER_VALIDATION_APP": str(app),
                    "YOVOICE_SPEAKER_VALIDATION_SMOKE_OUTPUT": str(result_root),
                    "YOVOICE_SPEAKER_FFMPEG": str(ffmpeg),
                    "YOVOICE_SPEAKER_PYTHON": str(python_bin),
                    "YOVOICE_SPEAKER_MODEL": str(model),
                    "YOVOICE_SPEAKER_RUNNER": str(runner),
                    "YOVOICE_SPEAKER_SMOKE_TIMEOUT": "3",
                    "YOVOICE_SPEAKER_POST_RESULT_TIMEOUT": "1",
                    "YOVOICE_SPEAKER_TERMINATE_GRACE": "1",
                    "SPEAKER_TEST_OUTCOME": outcome,
                    "SPEAKER_TEST_PID_FILE": str(pid_file),
                }

                try:
                    started = time.monotonic()
                    completed = subprocess.run(
                        ["bash", str(smoke)],
                        cwd=ROOT,
                        env=environment,
                        capture_output=True,
                        text=True,
                        timeout=8,
                    )
                    elapsed = time.monotonic() - started

                    self.assertNotEqual(completed.returncode, 0, completed.stdout + completed.stderr)
                    self.assertLess(elapsed, 8, completed.stdout + completed.stderr)
                    self.assertIn(expected_text, completed.stderr)
                    self.assertEqual(
                        json.loads((result_root / "initialize-result.json").read_text()),
                        {"failure": "fixture phase failure"} if outcome == "failure" else {"initialized": True},
                    )
                    self.assertTrue((result_root / "initialize.png").is_file())

                    host_pid = int(pid_file.read_text().strip())
                    with self.assertRaises(ProcessLookupError):
                        os.kill(host_pid, 0)
                finally:
                    if pid_file.is_file():
                        host_pid = int(pid_file.read_text().strip())
                        try:
                            os.kill(host_pid, signal.SIGKILL)
                        except ProcessLookupError:
                            pass

    def test_smoke_rejects_unbound_or_tampered_bundles_before_host_start(self):
        smoke = ROOT / "scripts/desktop/smoke-speaker-validation-macos.sh"

        for scenario in ("missing", "different-source", "app", "host", "service", "web", "valid"):
            with self.subTest(scenario=scenario), TemporaryDirectory(prefix="speaker-receipt-") as directory:
                root = Path(directory)
                app = root / "fixture.app"
                host = app / "Contents/MacOS/VoiceWorkbenchMac"
                service = app / "Contents/Resources/service/yovoice-service"
                python_bin = root / "python"
                model = root / "model.onnx"
                runner = root / "runner.py"
                ffmpeg = root / "ffmpeg"
                host_called = root / "host-called"
                for executable in (python_bin, ffmpeg):
                    executable.write_text("#!/bin/sh\nexit 0\n")
                    executable.chmod(0o755)
                model.write_text("fixture model")
                runner.write_text("fixture runner")
                host.parent.mkdir(parents=True)
                service.parent.mkdir(parents=True)
                (app / "Contents/Info.plist").write_text("fixture app metadata")
                service.write_text("#!/bin/sh\nexit 0\n")
                service.chmod(0o755)
                host.write_text("""#!/bin/bash
set -euo pipefail
mkdir -p "$WORKBENCH_DATA"
printf 'started\\n' > "$SPEAKER_TEST_HOST_CALLED"
printf '%s\\n' '{"failure":"fixture host started"}' > "$WORKBENCH_DATA/smoke-result.json"
printf 'fixture screenshot' > "$WORKBENCH_DATA/smoke.png"
""")
                host.chmod(0o755)
                receipt_path = write_fixture_receipt(app)

                if scenario == "missing":
                    receipt_path.unlink()
                elif scenario == "different-source":
                    capture = capture_module()
                    receipt = json.loads(receipt_path.read_text())
                    for source in (receipt["source"]["before"], receipt["source"]["after"]):
                        source["files"][0]["sha256"] = "0" * 64
                        source["sha256"] = capture.value_sha256(source["files"])
                    receipt_path.write_text(json.dumps(receipt))
                elif scenario == "app":
                    (app / "Contents/Info.plist").write_text("changed app metadata")
                elif scenario == "host":
                    host.write_text(host.read_text() + "# changed after receipt\n")
                elif scenario == "service":
                    service.write_text(service.read_text() + "# changed after receipt\n")
                elif scenario == "web":
                    (app / "Contents/Resources/web/index.html").write_text("changed after receipt")

                environment = {
                    **os.environ,
                    "YOVOICE_SPEAKER_VALIDATION_APP": str(app),
                    "YOVOICE_SPEAKER_VALIDATION_SMOKE_OUTPUT": str(root / "result"),
                    "YOVOICE_SPEAKER_FFMPEG": str(ffmpeg),
                    "YOVOICE_SPEAKER_PYTHON": str(python_bin),
                    "YOVOICE_SPEAKER_MODEL": str(model),
                    "YOVOICE_SPEAKER_RUNNER": str(runner),
                    "YOVOICE_SPEAKER_SMOKE_TIMEOUT": "3",
                    "SPEAKER_TEST_HOST_CALLED": str(host_called),
                }
                completed = subprocess.run(
                    ["bash", str(smoke)],
                    cwd=ROOT,
                    env=environment,
                    capture_output=True,
                    text=True,
                    check=False,
                    timeout=8,
                )

                self.assertNotEqual(completed.returncode, 0, completed.stdout + completed.stderr)
                if scenario == "valid":
                    self.assertTrue(host_called.is_file(), completed.stdout + completed.stderr)
                    self.assertIn("fixture host started", completed.stderr)
                else:
                    self.assertFalse(host_called.exists(), completed.stdout + completed.stderr)
                    self.assertIn("构建凭据无效；宿主未启动", completed.stderr)

    def test_capture_compares_full_business_state_without_exposing_runtime_paths(self):
        capture = capture_module()
        with TemporaryDirectory() as directory:
            data_root = Path(directory)
            (data_root / "state.json").write_text(json.dumps({
                "drafts": [{"id": "draft-a", "text": "unchanged source text"}],
                "runtimePath": "/private/runtime",
                "voices": [{"id": "voice-a", "duration": 1.25, "referenceText": "saved reference"}],
            }))
            state = capture.normalized_state(data_root)

        self.assertEqual(state["comparisonPolicy"]["volatileFieldsExcluded"], [])
        self.assertEqual(state["businessState"]["drafts"][0]["text"], "unchanged source text")
        self.assertTrue(state["businessState"]["runtimePath"]["redacted"])
        self.assertEqual(state["voices"][0]["referenceText"], "saved reference")

    def test_capture_reads_complete_sqlite_state_before_legacy_json(self):
        capture = capture_module()
        with TemporaryDirectory() as directory:
            data_root = Path(directory)
            database = data_root / "yovoice.db"
            connection = sqlite3.connect(database)
            connection.executescript("""
                CREATE TABLE settings (key TEXT PRIMARY KEY, pos INTEGER NOT NULL, data TEXT NOT NULL);
                CREATE TABLE characters (key TEXT PRIMARY KEY, pos INTEGER NOT NULL, data TEXT NOT NULL);
                CREATE TABLE drafts (key TEXT PRIMARY KEY, pos INTEGER NOT NULL, data TEXT NOT NULL);
                CREATE TABLE voices (key TEXT PRIMARY KEY, pos INTEGER NOT NULL, data TEXT NOT NULL);
                CREATE TABLE models (key TEXT PRIMARY KEY, pos INTEGER NOT NULL, data TEXT NOT NULL);
                CREATE TABLE history (key TEXT PRIMARY KEY, pos INTEGER NOT NULL, data TEXT NOT NULL);
                CREATE TABLE ai_providers (key TEXT PRIMARY KEY, pos INTEGER NOT NULL, data TEXT NOT NULL);
                CREATE TABLE private (key TEXT PRIMARY KEY, data TEXT NOT NULL);
                PRAGMA user_version = 1;
            """)
            settings = {
                "preferences": {"proxyURL": "http://private-proxy", "uiLocale": "en"},
                "runtimePath": "/private/runtime",
                "runtimeBackend": "cpu",
                "runtimeVersion": "1.0",
                "runtimes": {"cpu": {"path": "/private/runtime", "version": "1.0"}},
                "activity": None,
                "modelActivity": None,
                "aiProviderID": "provider-a",
                "assistantSessions": None,
                "assistantMemory": [],
            }
            for key, value in settings.items():
                connection.execute("INSERT INTO settings (key, pos, data) VALUES (?, 0, ?)", (key, json.dumps(value)))
            connection.execute("INSERT INTO drafts (key, pos, data) VALUES (?, ?, ?)", ("draft-second", 1, json.dumps({"id": "draft-second"})))
            connection.execute("INSERT INTO drafts (key, pos, data) VALUES (?, ?, ?)", ("draft-first", 0, json.dumps({"id": "draft-first"})))
            connection.execute("INSERT INTO voices (key, pos, data) VALUES (?, ?, ?)", ("voice-a", 0, json.dumps({"id": "voice-a", "referenceText": "saved reference"})))
            connection.execute("INSERT INTO private (key, data) VALUES (?, ?)", ("token", json.dumps("must-not-export")))
            connection.commit()
            connection.close()
            (data_root / "state.json").write_text(json.dumps({"drafts": [{"id": "legacy-draft"}]}))

            state = capture.normalized_state(data_root)

        self.assertEqual(state["source"], "sqlite")
        self.assertEqual([draft["id"] for draft in state["businessState"]["drafts"]], ["draft-first", "draft-second"])
        self.assertTrue(state["businessState"]["preferences"]["proxyURL"]["redacted"])
        self.assertTrue(state["businessState"]["runtimePath"]["redacted"])
        self.assertNotIn("private", state["businessState"])
        self.assertEqual(state["voices"][0]["referenceText"], "saved reference")

    def test_capture_rejects_a_bad_sqlite_database_without_legacy_fallback(self):
        capture = capture_module()
        with TemporaryDirectory() as directory:
            data_root = Path(directory)
            (data_root / "yovoice.db").write_text("not a SQLite database")
            (data_root / "state.json").write_text(json.dumps({"drafts": [{"id": "legacy-draft"}]}))

            with self.assertRaisesRegex(ValueError, "SQLite State database cannot be read"):
                capture.normalized_state(data_root)

    def test_capture_uses_one_sqlite_snapshot_while_another_connection_commits(self):
        capture = capture_module()
        with TemporaryDirectory() as directory:
            data_root = Path(directory)
            database = data_root / "yovoice.db"
            write_state_database(database, "before", "draft-before")
            writer = sqlite3.connect(database)
            writer.execute("PRAGMA journal_mode = WAL")
            writer.execute("UPDATE settings SET data = data WHERE key = 'preferences'")
            writer.commit()
            primer = sqlite3.connect(database)
            primer.execute("SELECT key FROM settings").fetchall()

            original_read_rows = capture.read_json_rows

            def read_rows_with_mid_capture_commit(connection, table):
                rows = original_read_rows(connection, table)
                if table == "settings":
                    writer.execute(
                        "UPDATE settings SET data = ? WHERE key = 'preferences'",
                        (json.dumps({"uiLocale": "after"}),),
                    )
                    writer.execute(
                        "UPDATE drafts SET data = ? WHERE key = 'draft-before'",
                        (json.dumps({"id": "draft-after"}),),
                    )
                    writer.commit()
                return rows

            capture.read_json_rows = read_rows_with_mid_capture_commit
            try:
                state = capture.normalized_state(data_root)
            finally:
                capture.read_json_rows = original_read_rows
                primer.close()
                writer.close()

        self.assertEqual(state["businessState"]["preferences"]["uiLocale"], "before")
        self.assertEqual(state["businessState"]["drafts"][0]["id"], "draft-before")

    def test_smoke_requires_complete_fingerprints_and_fixed_runtime_versions(self):
        self.assertIn("sourceCandidate", SMOKE)
        self.assertIn("webResources", SMOKE)
        self.assertIn("recognitionManifest", SMOKE)
        self.assertIn("matchesExpectedSherpaOnnx", SMOKE)

    def test_native_smoke_uses_public_profile_rows_and_loaded_state(self):
        main = (ROOT / "scripts/desktop/speaker-validation-smoke.js").read_text()
        restart = (ROOT / "scripts/desktop/speaker-validation-restart.js").read_text()
        self.assertIn('[data-speaker-profile-row="true"]', main)
        self.assertIn("profilesLoaded", main)
        self.assertIn("textInput('speaker-profile-name')", main)
        self.assertIn("unknownWithProfile", main)
        self.assertIn("profileID !== null", main)
        self.assertIn("[data-speaker-candidate-id]", main)
        self.assertIn("expectedCandidateMissing", main)
        self.assertIn("candidateIDs.length !== candidateCount", main)
        self.assertNotIn("#speaker-profile-name", main)
        self.assertIn('dataset.state === \'ready\'', restart)

    def test_native_visibility_requires_a_real_hit_target_and_observed_frames(self):
        main = (ROOT / "scripts/desktop/speaker-validation-smoke.js").read_text()
        restart = (ROOT / "scripts/desktop/speaker-validation-restart.js").read_text()
        for source in (main, restart):
            self.assertIn("const hitTarget = Boolean(hit)", source)
            self.assertIn("hit === element || element.contains", source)
            self.assertNotIn("hit.contains", source)
            self.assertIn("if (typeof requestAnimationFrame !== 'function') return false", source)
            self.assertIn("const fallback = setTimeout(() => finish(false), 500)", source)
        self.assertIn("if (!painted || visibleDialogOpen()", main)
        self.assertIn("if (!painted || renderedDialogOpen()", restart)

    def test_debug_host_checks_display_before_running_smoke_script(self):
        self.assertIn("SmokePresentationReadiness", HOST_MAIN)
        self.assertIn("SmokeDocumentVisibilityRequest", HOST_MAIN)
        self.assertIn("DispatchTime.now().uptimeNanoseconds", HOST_MAIN)
        self.assertIn("10_000_000_000", HOST_MAIN)
        self.assertIn("guard !completed", HOST_MAIN)
        self.assertIn("let expired = clock() >= deadline", HOST_MAIN)
        self.assertIn("remainingSmokePresentationNanoseconds(until: deadline) == 0", HOST_MAIN)
        self.assertIn("remainingSmokePresentationNanoseconds(until: deadline) > 0", HOST_MAIN)
        self.assertIn("window.makeKeyAndOrderFront(nil)", HOST_MAIN)
        self.assertIn("NSApp.activate(ignoringOtherApps: true)", HOST_MAIN)
        self.assertIn("document.visibilityState", HOST_MAIN)
        self.assertIn("windowMiniaturized", HOST_MAIN)
        self.assertIn("webViewAttached", HOST_MAIN)
        self.assertIn("webViewWidth", HOST_MAIN)
        self.assertIn("windowOcclusionVisible", HOST_MAIN)
        self.assertIn("writeSmokePresentationFailure", HOST_MAIN)
        self.assertLess(
            HOST_MAIN.index("_ = try await waitForSmokePresentation(webView)"),
            HOST_MAIN.index("String(contentsOfFile: scriptPath"),
        )

    def test_restart_waits_for_delayed_react_navigation_and_visible_profile(self):
        script = """
import { readFileSync } from 'node:fs';

const source = readFileSync(process.argv[1], 'utf8');
const profileID = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';
let navigationLookups = 0;
let speakerPageSelected = false;

const rect = { top: 40, right: 320, bottom: 80, left: 20, width: 300, height: 40 };
const belowFold = { top: 960, right: 320, bottom: 1000, left: 20, width: 300, height: 40 };
let profileRect = belowFold;
const visibleElement = (dataset = {}, getRect = () => rect) => ({
  dataset,
  isConnected: true,
  parentElement: null,
  getBoundingClientRect: getRect,
  scrollIntoView: () => undefined,
  contains: () => false,
});
const navigation = visibleElement();
navigation.click = () => { speakerPageSelected = true; };
navigation.getAttribute = name => name === 'aria-current' && speakerPageSelected ? 'page' : null;
const runtime = visibleElement();
const profiles = visibleElement({ state: 'ready' }, () => belowFold);
const profile = visibleElement({ profileId: profileID }, () => profileRect);
navigation.contains = candidate => candidate === navigation;
runtime.contains = candidate => candidate === runtime;
profiles.contains = candidate => candidate === profiles;
profile.contains = candidate => candidate === profile;
profile.scrollIntoView = () => { profileRect = rect; };

const document = {
  documentElement: { clientWidth: 1280, clientHeight: 800 },
  querySelector(selector) {
    if (selector === '[data-testid="nav-speakers"]') {
      navigationLookups += 1;
      return navigationLookups >= 3 ? navigation : null;
    }
    if (selector === '.speaker-runtime[role="status"]') return speakerPageSelected ? runtime : null;
    if (selector === '[data-testid="speaker-profiles"]') return speakerPageSelected ? profiles : null;
    return null;
  },
  querySelectorAll(selector) {
    if (selector === '[data-speaker-profile-row="true"]') return speakerPageSelected ? [profile] : [];
    if (selector === '[role="dialog"], [aria-modal="true"]') return [];
    return [];
  },
  elementFromPoint: () => speakerPageSelected ? profile : navigation,
};
const window = { innerWidth: 1280, innerHeight: 800 };
const getComputedStyle = () => ({
  display: 'block',
  visibility: 'visible',
  opacity: '1',
  overflow: 'visible',
  overflowX: 'visible',
  overflowY: 'visible',
});
const requestAnimationFrame = callback => setTimeout(() => callback(performance.now()), 0);

const directLookupBeforeReactMount = Boolean(document.querySelector('[data-testid="nav-speakers"]'));
const execute = new Function('document', 'window', 'getComputedStyle', 'requestAnimationFrame', source);
const result = await execute(document, window, getComputedStyle, requestAnimationFrame);
process.stdout.write(JSON.stringify({ directLookupBeforeReactMount, navigationLookups, result }));
"""
        restart = ROOT / "scripts/desktop/speaker-validation-restart.js"
        completed = subprocess.run(
            ["node", "--input-type=module", "--eval", script, str(restart)],
            cwd=ROOT,
            check=True,
            capture_output=True,
            text=True,
        )
        result = json.loads(completed.stdout)

        self.assertFalse(result["directLookupBeforeReactMount"])
        self.assertGreaterEqual(result["navigationLookups"], 3)
        self.assertEqual(result["result"]["profileIDs"], ["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"])
        self.assertTrue(result["result"]["readiness"]["profile"]["reachable"])

    def test_restart_rejects_an_ancestor_hit_target(self):
        script = """
import { readFileSync } from 'node:fs';

const source = readFileSync(process.argv[1], 'utf8');
const rect = { top: 40, right: 320, bottom: 80, left: 20, width: 300, height: 40 };
const navigation = {
  isConnected: true,
  parentElement: null,
  getBoundingClientRect: () => rect,
  contains: candidate => candidate === navigation,
};
const ancestor = {
  contains: candidate => candidate === navigation,
};
const document = {
  documentElement: { clientWidth: 1280, clientHeight: 800 },
  querySelector: selector => selector === '[data-testid="nav-speakers"]' ? navigation : null,
  querySelectorAll: () => [],
  elementFromPoint: () => ancestor,
};
let now = 0;
const clock = { now: () => { now += 31_000; return now; } };
const immediateTimeout = callback => { queueMicrotask(callback); return 0; };
const execute = new Function('document', 'window', 'getComputedStyle', 'requestAnimationFrame', 'performance', 'setTimeout', 'clearTimeout', source);
const result = await execute(
  document,
  { innerWidth: 1280, innerHeight: 800 },
  () => ({ display: 'block', visibility: 'visible', opacity: '1', overflow: 'visible', overflowX: 'visible', overflowY: 'visible' }),
  () => 0,
  clock,
  immediateTimeout,
  () => undefined,
);
process.stdout.write(JSON.stringify(result));
"""
        restart = ROOT / "scripts/desktop/speaker-validation-restart.js"
        completed = subprocess.run(
            ["node", "--input-type=module", "--eval", script, str(restart)],
            cwd=ROOT,
            check=True,
            capture_output=True,
            text=True,
        )
        result = json.loads(completed.stdout)

        self.assertIn("visible speaker navigation", result["failure"])
        self.assertFalse(result["diagnostics"]["navigation"]["reachable"])

    def test_native_smoke_records_public_runtime_diagnostics(self):
        main = (ROOT / "scripts/desktop/speaker-validation-smoke.js").read_text()
        self.assertIn("publicRuntime", main)
        self.assertIn("speaker-runtime-diagnostic", main)
        self.assertIn("AbortSignal.timeout(10_000)", main)
        self.assertIn("visible configured speaker enrollment controls", main)
        self.assertIn("controlVisibility('speaker-enroll')", main)
        self.assertIn("controlVisibility('speaker-profile-name')", main)
        self.assertIn("diagnostics: await publicRuntime()", main)
        self.assertNotIn("modelPath", main)

    def test_native_smoke_failure_returns_public_diagnostics(self):
        script = """
import { readFileSync } from 'node:fs';

const source = readFileSync(process.argv[1], 'utf8');
const document = {
  querySelector: () => null,
};
const fetch = async (url) => {
  if (url === '/speaker-validation-samples.json') return { ok: false, status: 503 };
  if (url === '/api/call') {
    return {
      ok: true,
      json: async () => ({ result: { configured: false, threshold: 0.6, margin: 0.05, timeoutSeconds: 120 } }),
    };
  }
  throw new Error(`unexpected fetch: ${url}`);
};
const execute = new Function('document', 'fetch', 'AbortSignal', source);
const result = await execute(document, fetch, AbortSignal);
process.stdout.write(JSON.stringify(result));
"""
        smoke = ROOT / "scripts/desktop/speaker-validation-smoke.js"
        completed = subprocess.run(
            ["node", "--input-type=module", "--eval", script, str(smoke)],
            cwd=ROOT,
            check=True,
            capture_output=True,
            text=True,
        )
        result = json.loads(completed.stdout)

        self.assertIn("validation sample fixture unavailable: 503", result["failure"])
        self.assertEqual(result["diagnostics"]["runtime"], {
            "configured": False,
            "threshold": 0.6,
            "margin": 0.05,
            "timeoutSeconds": 120,
            "featureVersion": None,
            "dimension": None,
        })
        self.assertIsNone(result["diagnostics"]["rpcError"])
        self.assertEqual(result["diagnostics"]["controls"]["navigation"], {
            "present": False,
            "visible": False,
            "reachable": False,
        })

    def test_visibility_diagnostics_distinguish_rejection_reasons(self):
        scenarios = {
            "hidden": {"rect": [20, 20, 220, 60], "style": {"display": "none"}},
            "zero-size": {"rect": [20, 20, 20, 60]},
            "ancestor-clipped": {"rect": [20, 20, 220, 60], "ancestor": "clipper"},
            "off-viewport": {"rect": [20, 1200, 220, 1240]},
            "hit-occluded": {"rect": [20, 20, 220, 60], "occluded": True},
        }
        for source_name, diagnostic_key in (
            ("speaker-validation-smoke.js", "enrollment"),
            ("speaker-validation-restart.js", "navigation"),
        ):
            source_path = ROOT / "scripts/desktop" / source_name
            for reason, scenario in scenarios.items():
                with self.subTest(source=source_name, reason=reason):
                    source = source_path.read_text()
                    script = """
import { readFileSync } from 'node:fs';

const source = readFileSync(process.argv[1], 'utf8');
const scenario = JSON.parse(process.argv[2]);
const reason = process.argv[3];
const rectValues = scenario.rect;
const rect = {
  left: rectValues[0],
  top: rectValues[1],
  right: rectValues[2],
  bottom: rectValues[3],
  width: rectValues[2] - rectValues[0],
  height: rectValues[3] - rectValues[1],
};
const normalStyle = {
  display: 'block',
  visibility: 'visible',
  opacity: '1',
  overflow: 'visible',
  overflowX: 'visible',
  overflowY: 'visible',
};
const ancestor = scenario.ancestor ? {
  dataset: { testid: scenario.ancestor },
  className: 'fixture-clipper',
  parentElement: null,
  getBoundingClientRect: () => ({ left: 300, top: 20, right: 360, bottom: 60, width: 60, height: 40 }),
} : null;
const target = {
  dataset: { testid: 'speaker-enroll' },
  className: 'fixture-control',
  isConnected: true,
  parentElement: ancestor,
  matches: () => true,
  querySelector: () => null,
  getBoundingClientRect: () => rect,
  scrollIntoView: () => undefined,
  contains: candidate => candidate === target,
  getAttribute: () => null,
};
const occluder = { contains: () => false };
const document = {
  documentElement: { clientWidth: 800, clientHeight: 600 },
  querySelector(selector) {
    if (selector === '[data-testid="speaker-enroll"]') return target;
    if (selector === '[data-testid="nav-speakers"]') return target;
    return null;
  },
  querySelectorAll: () => [],
  elementFromPoint: () => scenario.occluded ? occluder : target,
};
const window = { innerWidth: 800, innerHeight: 600 };
const getComputedStyle = element => {
  if (element === ancestor) {
    return { ...normalStyle, overflow: 'hidden', overflowX: 'hidden', overflowY: 'hidden' };
  }
  return { ...normalStyle, ...scenario.style };
};
const fetch = async url => {
  if (url === '/speaker-validation-samples.json') return { ok: false, status: 503 };
  if (url === '/api/call') return { ok: true, json: async () => ({ result: { configured: false } }) };
  throw new Error(`unexpected fetch: ${url}`);
};
let clock = 0;
const performance = { now: () => { clock += 31_000; return clock; } };
const requestAnimationFrame = callback => setTimeout(() => callback(0), 0);
const execute = new Function(
  'document', 'window', 'getComputedStyle', 'fetch', 'AbortSignal', 'performance', 'requestAnimationFrame', source,
);
const result = await execute(document, window, getComputedStyle, fetch, AbortSignal, performance, requestAnimationFrame);
const controls = result.diagnostics.controls ?? result.diagnostics;
process.stdout.write(JSON.stringify({ reason, diagnostic: controls[process.argv[4]] }));
"""
                    completed = subprocess.run(
                        [
                            "node",
                            "--input-type=module",
                            "--eval",
                            script,
                            str(source_path),
                            json.dumps(scenario),
                            reason,
                            diagnostic_key,
                        ],
                        cwd=ROOT,
                        check=True,
                        capture_output=True,
                        text=True,
                    )
                    result = json.loads(completed.stdout)
                    diagnostic = result["diagnostic"]

                    self.assertEqual(diagnostic["reason"], reason)
                    self.assertIn("rect", diagnostic)
                    self.assertIn("clipped", diagnostic)
                    self.assertIn("display", diagnostic)
                    self.assertIn("visibility", diagnostic)
                    self.assertIn("opacity", diagnostic)
                    if reason == "ancestor-clipped":
                        self.assertEqual(diagnostic["rejectedAncestor"]["testID"], "clipper")
                    if reason == "hit-occluded":
                        self.assertTrue(diagnostic["visible"])
                        self.assertFalse(diagnostic["reachable"])
                    else:
                        self.assertFalse(diagnostic["visible"])
                        self.assertFalse(diagnostic["reachable"])


if __name__ == "__main__":
    unittest.main()
