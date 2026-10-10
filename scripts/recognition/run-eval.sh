#!/bin/sh
# 在不覆盖旧证据的前提下，重跑固定的本地 CLI 与 HTTP 声纹评估。
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd -P)
output=${YOVOICE_EVAL_OUTPUT:?set a new artifacts/verification output directory}
binary=${YOVOICE_EVAL_BINARY:-"$root/artifacts/yovoice"}

if [ -e "$output" ]; then
	printf '%s\n' "refusing to overwrite $output" >&2
	exit 2
fi
if [ ! -x "$binary" ]; then
	printf '%s\n' "build the CLI first or set YOVOICE_EVAL_BINARY" >&2
	exit 2
fi
for variable in YOVOICE_SPEAKER_PYTHON YOVOICE_SPEAKER_MODEL YOVOICE_SPEAKER_RUNNER; do
	case "$variable" in
	YOVOICE_SPEAKER_PYTHON) value=${YOVOICE_SPEAKER_PYTHON-} ;;
	YOVOICE_SPEAKER_MODEL) value=${YOVOICE_SPEAKER_MODEL-} ;;
	YOVOICE_SPEAKER_RUNNER) value=${YOVOICE_SPEAKER_RUNNER-} ;;
	esac
	case "$value" in
	/*) ;;
	*)
		printf '%s\n' "$variable must be an absolute local path" >&2
		exit 2
		;;
	esac
done

: "${YOVOICE_SPEAKER_THRESHOLD:=0.6}"
: "${YOVOICE_SPEAKER_MARGIN:=0.05}"
: "${YOVOICE_SPEAKER_TIMEOUT:=120s}"
export YOVOICE_SPEAKER_THRESHOLD YOVOICE_SPEAKER_MARGIN YOVOICE_SPEAKER_TIMEOUT

if ! "$YOVOICE_SPEAKER_PYTHON" -I -c '
import math
import os
import sys
try:
    threshold = float(os.environ["YOVOICE_SPEAKER_THRESHOLD"])
    margin = float(os.environ["YOVOICE_SPEAKER_MARGIN"])
except ValueError:
    sys.exit(1)
sys.exit(not (
    math.isfinite(threshold)
    and math.isfinite(margin)
    and threshold == 0.6
    and margin == 0.05
    and os.environ["YOVOICE_SPEAKER_TIMEOUT"] == "120s"
))
'; then
	printf '%s\n' "this fixed evaluation requires threshold 0.6, margin 0.05, and timeout 120s" >&2
	exit 2
fi

exec "$YOVOICE_SPEAKER_PYTHON" -I "$root/scripts/recognition/eval.py" \
	--root "$root" \
	--binary "$binary" \
	--output "$output" \
	--manifest "$root/scripts/recognition/eval-manifest.json"
