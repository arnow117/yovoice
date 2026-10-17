#!/bin/bash
set -euo pipefail

repo=$(cd "$(dirname "$0")/../.." && pwd)
cd "$repo"

app="${YOVOICE_SPEAKER_VALIDATION_APP:-$repo/artifacts/speaker-validation-macos/yovoice.app}"
ffmpeg="${YOVOICE_SPEAKER_FFMPEG:-/opt/homebrew/bin/ffmpeg}"
python_bin="${YOVOICE_SPEAKER_PYTHON:-$repo/artifacts/recognition/venv/bin/python}"
model="${YOVOICE_SPEAKER_MODEL:-$repo/artifacts/recognition/models/wespeaker_zh_cnceleb_resnet34.onnx}"
runner="${YOVOICE_SPEAKER_RUNNER:-$repo/scripts/recognition/runner.py}"
threshold="${YOVOICE_SPEAKER_THRESHOLD:-0.6}"
margin="${YOVOICE_SPEAKER_MARGIN:-0.05}"
runtime_timeout="${YOVOICE_SPEAKER_TIMEOUT:-120s}"
manifest="$repo/scripts/recognition/eval-manifest.json"
host="$app/Contents/MacOS/VoiceWorkbenchMac"
service="$app/Contents/Resources/service/yovoice-service"
result_root="${YOVOICE_SPEAKER_VALIDATION_SMOKE_OUTPUT:-$repo/artifacts/speaker-validation-macos/smoke-$(date +%Y%m%d-%H%M%S)}"
data_root="$result_root/data"
timeout_seconds="${YOVOICE_SPEAKER_SMOKE_TIMEOUT:-600}"
post_result_timeout_seconds="${YOVOICE_SPEAKER_POST_RESULT_TIMEOUT:-15}"
terminate_grace_seconds="${YOVOICE_SPEAKER_TERMINATE_GRACE:-3}"

# 以仓库为基准解析显式输入，避免从 Finder、临时目录或带空格目录启动时
# 把验证数据与日志写到不可预期的位置。
absolute_path() {
  python3 "$repo/scripts/desktop/speaker-validation-paths.py" "$repo" "$1"
}

app=$(absolute_path "$app")
ffmpeg=$(absolute_path "$ffmpeg")
python_bin=$(absolute_path "$python_bin")
model=$(absolute_path "$model")
runner=$(absolute_path "$runner")
manifest=$(absolute_path "$manifest")
result_root=$(absolute_path "$result_root")
host="$app/Contents/MacOS/VoiceWorkbenchMac"
service="$app/Contents/Resources/service/yovoice-service"
data_root="$result_root/data"

for path in "$host" "$service" "$ffmpeg" "$python_bin" "$model" "$runner" "$manifest"; do
  if [[ ! -f "$path" ]]; then
    echo "缺少声纹验证输入：$path" >&2
    exit 1
  fi
done
if [[ ! -x "$host" || ! -x "$service" || ! -x "$ffmpeg" || ! -x "$python_bin" ]]; then
  echo '声纹验证包或其运行依赖不可执行。' >&2
  exit 1
fi
if [[ "$timeout_seconds" =~ [^0-9] ]] || (( timeout_seconds < 1 || timeout_seconds > 900 )); then
  echo 'YOVOICE_SPEAKER_SMOKE_TIMEOUT 必须为 1 到 900 秒。' >&2
  exit 1
fi
if [[ "$post_result_timeout_seconds" =~ [^0-9] ]] || (( post_result_timeout_seconds < 1 || post_result_timeout_seconds > 60 )); then
  echo 'YOVOICE_SPEAKER_POST_RESULT_TIMEOUT 必须为 1 到 60 秒。' >&2
  exit 1
fi
if [[ "$terminate_grace_seconds" =~ [^0-9] ]] || (( terminate_grace_seconds < 1 || terminate_grace_seconds > 30 )); then
  echo 'YOVOICE_SPEAKER_TERMINATE_GRACE 必须为 1 到 30 秒。' >&2
  exit 1
fi

# 凭据始终位于签名 App 的同级位置，避免把它写入已封装的资源树后改变签名。
receipt=$(python3 "$repo/scripts/desktop/capture-speaker-validation-evidence.py" \
  --print-build-receipt-path \
  --app "$app")
if ! python3 "$repo/scripts/desktop/capture-speaker-validation-evidence.py" \
  --verify-build-receipt \
  --receipt "$receipt" \
  --app "$app"; then
  echo '声纹验证包构建凭据无效；宿主未启动。' >&2
  exit 1
fi

if [[ -e "$result_root" ]]; then
  echo "声纹验证输出已存在，拒绝覆盖既有证据：$result_root" >&2
  exit 1
fi
mkdir -p "$result_root" "$data_root"
main_script="$result_root/speaker-validation-smoke.js"
restart_script="$result_root/speaker-validation-restart.js"
init_script="$result_root/speaker-validation-init.js"
app_pid=''

wait_for_owned_exit() {
  local timeout="$1"
  local deadline=$((SECONDS + timeout))

  while [[ -n "$app_pid" ]] && kill -0 "$app_pid" 2>/dev/null; do
    if (( SECONDS >= deadline )); then
      return 1
    fi
    sleep 1
  done

  if [[ -n "$app_pid" ]]; then
    wait "$app_pid" 2>/dev/null || true
    app_pid=''
  fi
  return 0
}

stop_owned_app() {
  if [[ -n "$app_pid" ]] && kill -0 "$app_pid" 2>/dev/null; then
    kill -TERM "$app_pid" 2>/dev/null || true
    if ! wait_for_owned_exit "$terminate_grace_seconds"; then
      echo "验证 App 在 TERM 宽限后仍未退出，发送 KILL：$app_pid" >&2
      kill -KILL "$app_pid" 2>/dev/null || true
      if ! wait_for_owned_exit "$terminate_grace_seconds"; then
        echo "验证 App 在 KILL 后仍未确认退出：$app_pid" >&2
      fi
    fi
  fi
  app_pid=''
}

preserve_phase_result() {
  local label="$1"
  if [[ -f "$data_root/smoke-result.json" ]]; then
    cp "$data_root/smoke-result.json" "$result_root/$label-result.json"
  fi
  if [[ -f "$data_root/smoke.png" ]]; then
    cp "$data_root/smoke.png" "$result_root/$label.png"
  fi
  if [[ -f "$data_root/smoke-error.txt" ]]; then
    cp "$data_root/smoke-error.txt" "$result_root/$label-error.txt"
  fi
}

require_phase_success() {
  local label="$1"
  preserve_phase_result "$label"
  DATA_ROOT="$data_root" LABEL="$label" python3 - <<'PY'
import json
import os
import sys
from pathlib import Path

path = Path(os.environ["DATA_ROOT"]) / "smoke-result.json"
try:
    value = json.loads(path.read_text())
except (OSError, json.JSONDecodeError) as error:
    raise SystemExit(f"声纹验证 {os.environ['LABEL']} 缺少可读取的 phase result: {error}")
failure = value.get("failure") if isinstance(value, dict) else "phase result is not an object"
if failure:
    raise SystemExit(f"声纹验证 {os.environ['LABEL']} failed: {failure}")
PY
}
trap stop_owned_app EXIT

render_smoke_script() {
  local template="$1"
  local output="$2"
  cp "$template" "$output"
}

capture() {
  local phase="$1"
  python3 "$repo/scripts/desktop/capture-speaker-validation-evidence.py" \
    --phase "$phase" \
    --output "$result_root/$phase.json" \
    --app "$app" \
    --data-root "$data_root" \
    --ffmpeg "$ffmpeg" \
    --python "$python_bin" \
    --model "$model" \
    --runner "$runner" \
    --manifest "$manifest"
}

wait_for_smoke_result() {
  local label="$1"
  local started="$SECONDS"
  while (( SECONDS - started < timeout_seconds )); do
    if [[ -f "$data_root/smoke-error.txt" ]]; then
      cat "$data_root/smoke-error.txt" >&2
      return 1
    fi
    if [[ -f "$data_root/smoke-result.json" ]]; then
      return 0
    fi
    if [[ -n "$app_pid" ]] && ! kill -0 "$app_pid" 2>/dev/null; then
      echo "验证 App 在 $label 结束前退出；请查看 $result_root/$label-host.log" >&2
      return 1
    fi
    sleep 1
  done
  echo "声纹验证 $label 在 ${timeout_seconds} 秒内未完成。保留证据于 $result_root" >&2
  return 1
}

wait_for_phase_completion() {
  local label="$1"
  local deadline=$((SECONDS + post_result_timeout_seconds))

  while (( SECONDS < deadline )); do
    local screenshot_ready=false
    [[ -f "$data_root/smoke.png" ]] && screenshot_ready=true

    if [[ -n "$app_pid" ]] && ! kill -0 "$app_pid" 2>/dev/null; then
      wait "$app_pid" 2>/dev/null || true
      app_pid=''
      if [[ "$screenshot_ready" == true ]]; then
        return 0
      fi
      echo "验证 App 在 $label 保存截图前退出。" >&2
      return 1
    fi

    if [[ "$screenshot_ready" == true ]] && [[ -z "$app_pid" ]]; then
      return 0
    fi
    sleep 1
  done

  echo "验证 App 在 $label 结果后 ${post_result_timeout_seconds} 秒内未完成截图和退出。" >&2
  return 1
}

launch_smoke() {
  local label="$1"
  local script="$2"
  if [[ -e "$data_root/smoke-result.json" || -e "$data_root/smoke-error.txt" ]]; then
    echo "声纹验证 $label 的上一次结果仍在数据目录中，拒绝覆盖。" >&2
    return 1
  fi
  WORKBENCH_DATA="$data_root" \
    WORKBENCH_DEBUG=1 \
    WORKBENCH_SMOKE_SCRIPT="$script" \
    YOVOICE_SPEAKER_PYTHON="$python_bin" \
    YOVOICE_SPEAKER_MODEL="$model" \
    YOVOICE_SPEAKER_RUNNER="$runner" \
    YOVOICE_SPEAKER_THRESHOLD="$threshold" \
    YOVOICE_SPEAKER_MARGIN="$margin" \
    YOVOICE_SPEAKER_TIMEOUT="$runtime_timeout" \
    "$host" >"$result_root/$label-host.log" 2>&1 &
  app_pid=$!
  if ! wait_for_smoke_result "$label"; then
    preserve_phase_result "$label"
    stop_owned_app
    return 1
  fi

  preserve_phase_result "$label"
  if ! require_phase_success "$label"; then
    stop_owned_app
    return 1
  fi

  if ! wait_for_phase_completion "$label"; then
    preserve_phase_result "$label"
    stop_owned_app
    return 1
  fi

  mv "$data_root/smoke-result.json" "$result_root/$label-result.json"
  mv "$data_root/smoke.png" "$result_root/$label.png"
}

render_smoke_script "$repo/scripts/desktop/speaker-validation-init.js" "$init_script"
launch_smoke initialize "$init_script"
capture before-smoke
render_smoke_script "$repo/scripts/desktop/speaker-validation-smoke.js" "$main_script"
launch_smoke main "$main_script"
capture after-main
render_smoke_script "$repo/scripts/desktop/speaker-validation-restart.js" "$restart_script"
launch_smoke restart "$restart_script"
capture after-restart

RESULT_ROOT="$result_root" python3 - <<'PY'
import json
import os
from pathlib import Path

root = Path(os.environ['RESULT_ROOT'])
before = json.loads((root / 'before-smoke.json').read_text())
after_main = json.loads((root / 'after-main.json').read_text())
after_restart = json.loads((root / 'after-restart.json').read_text())
main = json.loads((root / 'main-result.json').read_text())
restart = json.loads((root / 'restart-result.json').read_text())

if main.get('failure'):
    raise SystemExit(f"WKWebView main smoke failed: {main['failure']}")

fingerprint_sections = ('files', 'sourceCandidate', 'appBundle', 'webResources', 'fixedAssets', 'recognitionManifest')
for section in fingerprint_sections:
    if before[section] != after_main[section] or after_main[section] != after_restart[section]:
        raise SystemExit(f'验证期间 {section} 指纹发生变化')
if not after_restart['recognitionManifest']['allExpectedHashesMatch']:
    raise SystemExit('固定识别模型或样本未匹配 eval-manifest 的预期 SHA256')
if not after_restart['versions']['python']['matchesExpectedSherpaOnnx']:
    raise SystemExit('实际 sherpa-onnx 版本不符合本次固定验证环境')
if before['normalizedState'] != after_main['normalizedState'] or after_main['normalizedState'] != after_restart['normalizedState']:
    raise SystemExit('声纹页不应修改常规 State 或 Voices')
if len(main['results']) != 6 or [result['actual'] for result in main['results'][2:5]] != ['matched', 'matched', 'unknown']:
    raise SystemExit('真实 UI 识别结果未满足两个已知与一个未知样本的预期')
if main['profileIDs'] != restart['profileIDs'] or len(main['profileIDs']) != 1:
    raise SystemExit('删除后重启的档案列表不一致')
if len(after_restart['publicProfiles']) != 1:
    raise SystemExit('删除的声纹档案在重启后重新出现')

report = {
    'status': 'passed',
    'main': main,
    'restart': restart,
    'publicProfiles': after_restart['publicProfiles'],
    'fingerprintsStable': True,
    'normalizedStateStable': True,
    'notes': ['真实 WKWebView UI 文件输入；未直接调用 RPC。', '未执行物理麦克风或 TTS 听测。'],
}
(root / 'report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
PY

echo "声纹验证通过：$result_root/report.json"
