#!/bin/bash
set -euo pipefail

repo=$(cd "$(dirname "$0")/../.." && pwd)
cd "$repo"

if [[ "$(uname -m)" != "arm64" ]]; then
  echo '声纹验证包仅支持 Apple Silicon arm64。' >&2
  exit 1
fi

output="${YOVOICE_SPEAKER_VALIDATION_OUTPUT:-$repo/artifacts/speaker-validation-macos/yovoice.app}"
data_root="${YOVOICE_SPEAKER_VALIDATION_DATA:-$repo/artifacts/speaker-validation-macos/data}"
ffmpeg="${YOVOICE_SPEAKER_FFMPEG:-/opt/homebrew/bin/ffmpeg}"
required_runtime=(YOVOICE_SPEAKER_PYTHON YOVOICE_SPEAKER_MODEL YOVOICE_SPEAKER_RUNNER YOVOICE_SPEAKER_THRESHOLD YOVOICE_SPEAKER_MARGIN YOVOICE_SPEAKER_TIMEOUT)

# 验证包会被 Finder 从任意工作目录启动。把操作者给出的相对路径固定到
# 当前仓库，而不是把它们原样写进 Info.plist 或 FFmpeg 包装器。
absolute_path() {
  python3 "$repo/scripts/desktop/speaker-validation-paths.py" "$repo" "$1"
}

output=$(absolute_path "$output")
data_root=$(absolute_path "$data_root")
ffmpeg=$(absolute_path "$ffmpeg")
receipt_output=$(python3 "$repo/scripts/desktop/capture-speaker-validation-evidence.py" \
  --print-build-receipt-path \
  --app "$output")

for name in "${required_runtime[@]}"; do
  if [[ -z "${!name-}" ]]; then
    echo "缺少验证包运行配置：$name" >&2
    exit 1
  fi
done
YOVOICE_SPEAKER_PYTHON=$(absolute_path "$YOVOICE_SPEAKER_PYTHON")
YOVOICE_SPEAKER_MODEL=$(absolute_path "$YOVOICE_SPEAKER_MODEL")
YOVOICE_SPEAKER_RUNNER=$(absolute_path "$YOVOICE_SPEAKER_RUNNER")

if [[ ! -f "$YOVOICE_SPEAKER_PYTHON" || ! -x "$YOVOICE_SPEAKER_PYTHON" ]]; then
  echo '声纹验证 Python 必须是已有可执行文件。' >&2
  exit 1
fi
if [[ ! -f "$YOVOICE_SPEAKER_MODEL" ]]; then
  echo '声纹验证模型必须是已有常规文件。' >&2
  exit 1
fi
if [[ ! -f "$YOVOICE_SPEAKER_RUNNER" ]]; then
  echo '声纹验证 runner 必须是已有常规文件。' >&2
  exit 1
fi
if [[ ! -x "$ffmpeg" ]]; then
  echo "找不到可执行的外部 FFmpeg：$ffmpeg" >&2
  exit 1
fi
if [[ -e "$data_root" && ! -d "$data_root" ]]; then
  echo '验证数据目录不能是普通文件。' >&2
  exit 1
fi
if [[ -e "$output" && ! -d "$output" ]]; then
  echo '验证 App 输出路径必须是目录或不存在。' >&2
  exit 1
fi
if [[ -e "$receipt_output" && ! -f "$receipt_output" ]]; then
  echo '验证包构建凭据输出路径必须是普通文件或不存在。' >&2
  exit 1
fi
if ps -axo comm= | grep -Fqx "$output/Contents/MacOS/VoiceWorkbenchMac"; then
  echo '请先退出声纹验证包；脚本不会替换运行中的验证 App。' >&2
  exit 1
fi
if [[ -e "$output" || -e "$receipt_output" ]]; then
  echo '验证 App 输出或同级构建凭据已存在；请使用新的独立输出目录，脚本不会覆盖已有证据。' >&2
  exit 1
fi

staging=$(mktemp -d "$repo/artifacts/.speaker-validation.XXXXXX")
trap 'rm -rf "$staging"' EXIT
source_before="$staging/source-before.json"
python3 "$repo/scripts/desktop/capture-speaker-validation-evidence.py" \
  --write-source-manifest "$source_before"

(
  cd web
  ./node_modules/.bin/tsc -b
  ./node_modules/.bin/vite build
)
swift build --package-path desktop/macos -c debug --arch arm64

mkdir -p "$(dirname "$output")"
app="$staging/yovoice.app"
resources="$app/Contents/Resources"
mkdir -p "$app/Contents/MacOS" "$resources/service" "$resources/web" "$resources/tools"

GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o "$resources/service/yovoice-service" ./cmd/yovoice-service
cp "desktop/macos/.build/debug/VoiceWorkbenchMac" "$app/Contents/MacOS/VoiceWorkbenchMac"
cp desktop/macos/Resources/AppIcon.icns "$resources/AppIcon.icns"
cp desktop/macos/Resources/install-update.sh "$resources/install-update.sh"
cp -R web/dist/. "$resources/web/"
python3 - "$resources/web/speaker-validation-samples.json" <<'PY'
import base64
import json
import sys
from pathlib import Path

samples = {}
for path in sorted(Path('artifacts/recognition/samples').glob('*.wav')):
    samples[path.stem] = {
        'name': path.name,
        'base64': base64.b64encode(path.read_bytes()).decode('ascii'),
    }
Path(sys.argv[1]).write_text(json.dumps(samples, separators=(',', ':')))
PY
cp LICENSE THIRD_PARTY_NOTICES.md "$resources/"

python3 "$repo/scripts/desktop/speaker-validation-wrapper.py" "$resources/tools/ffmpeg" "$ffmpeg"
cp scripts/desktop/speaker-validation-info.plist "$app/Contents/Info.plist"
YOVOICE_VALIDATION_DATA_DIRECTORY="$data_root" python3 - "$app/Contents/Info.plist" <<'PY'
import os
import plistlib
import sys

path = sys.argv[1]
with open(path, 'rb') as file:
    info = plistlib.load(file)
info['YovoiceValidationDataDirectory'] = os.environ['YOVOICE_VALIDATION_DATA_DIRECTORY']
info['YovoiceSpeakerRuntime'] = {
    'PYTHON': os.environ['YOVOICE_SPEAKER_PYTHON'],
    'MODEL': os.environ['YOVOICE_SPEAKER_MODEL'],
    'RUNNER': os.environ['YOVOICE_SPEAKER_RUNNER'],
    'THRESHOLD': os.environ['YOVOICE_SPEAKER_THRESHOLD'],
    'MARGIN': os.environ['YOVOICE_SPEAKER_MARGIN'],
    'TIMEOUT': os.environ['YOVOICE_SPEAKER_TIMEOUT'],
}
with open(path, 'wb') as file:
    plistlib.dump(info, file)
PY

# 只签验证包副本，顺序是 service → host → bundle；从不触碰 Homebrew 的 FFmpeg。
codesign --force --sign - "$resources/service/yovoice-service"
codesign --force --sign - --entitlements desktop/macos/entitlements.plist "$app/Contents/MacOS/VoiceWorkbenchMac"
codesign --force --sign - "$app"
codesign --verify --deep --strict "$app"

# receipt 必须绑定最终签名后的 host；它作为同名 sidecar 保留，避免写入包内
# 资源后改变已验证的 bundle signature。
receipt="$staging/speaker-validation-build-receipt.json"
python3 "$repo/scripts/desktop/capture-speaker-validation-evidence.py" \
  --write-build-receipt "$receipt" \
  --source-before "$source_before" \
  --app "$app"
python3 "$repo/scripts/desktop/capture-speaker-validation-evidence.py" \
  --verify-build-receipt \
  --receipt "$receipt" \
  --app "$app"

if ps -axo comm= | grep -Fqx "$output/Contents/MacOS/VoiceWorkbenchMac"; then
  echo '构建期间验证 App 被启动，拒绝发布新输出。' >&2
  exit 1
fi
if [[ -e "$output" || -e "$receipt_output" ]]; then
  echo '构建期间输出已出现，拒绝覆盖已有验证 App 或凭据。' >&2
  exit 1
fi
if ! mv "$receipt" "$receipt_output"; then
  echo '未能发布同级构建凭据；验证 App 保留在 staging，未发布。' >&2
  exit 1
fi
if ! mv "$app" "$output"; then
  echo '同级构建凭据已发布但 App 未发布；保留该不可运行凭据以避免丢失构建证据。' >&2
  exit 1
fi

shasum -a 256 "$ffmpeg"
"$ffmpeg" -version | head -1
echo "已构建可 Finder 双击的本地声纹验证包：$output"
echo "验证包构建凭据：$receipt_output"
