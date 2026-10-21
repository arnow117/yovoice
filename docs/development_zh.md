# 开发指南

[English](development.md) · 简体中文

本指南用于从源码构建 yovoice。直接安装请查看 [CLI 安装](cli_zh.md#安装)或 [Releases](https://github.com/leemysw/yovoice/releases)。

## 环境要求

- Node.js 22+、pnpm 9.15.2、Go 1.26+、Python 3.12+。
- 音频转换器构建需要 Make、Bash、curl 和 C 编译器。
- macOS：Apple Silicon、macOS 14+、Xcode Command Line Tools。
- Windows：Windows 10/11 x64、.NET SDK 8、WebView2 Runtime、MSYS2 MinGW64。制作安装包还需 Inno Setup 6，使用 Make 命令需 GNU Make。

Windows 在 MSYS2 MinGW64 终端中安装音频构建工具：

```sh
pacman -Syu
pacman -S --needed make curl diffutils mingw-w64-x86_64-gcc mingw-w64-x86_64-python
```

更新要求重新打开终端时按提示操作。构建默认查找 `artifacts/tooling/msys64` 或 `C:/msys64`；其他位置可通过 `YOVOICE_MSYS2` 指定。

## 运行 App

```sh
git clone https://github.com/leemysw/yovoice.git
cd yovoice
make install
make app-run
```

后续命令均在仓库根目录执行。`app-run` 只构建并启动，不执行测试或打包；macOS 下会在终端显示日志。关闭窗口不会退出 App，重新构建前请通过应用菜单退出。日志位于 `~/.yovoice/logs`。

仅开发浏览器界面时运行 `make dev`，访问 `http://127.0.0.1:5173`。本地语音生成需要桌面 App 或 CLI。

## 构建与打包

| 命令 | 产物 |
| --- | --- |
| `make app-build` | `artifacts/macos-arm64/`、`artifacts/windows-x64/` 或 `artifacts/linux-x64/` 中的 App |
| `make app-package` | `artifacts/` 中的 macOS DMG/ZIP、Windows Setup EXE 或 Linux `.tar.gz` 与 `.deb` |
| `make cli-build` | CLI 可执行文件和 `artifacts/tools/` |
| `python3 scripts/package-cli.py` | `artifacts/` 中的独立 CLI ZIP |

CLI 需保留同级的 `tools/` 目录。完整安装包应在目标平台构建。macOS DMG 打包需要已登录的桌面会话和 Finder 自动化权限。

Linux 版在 Linux x64 上构建，运行时需要 WebKitGTK 4.1（`sudo apt install libwebkit2gtk-4.1-0`）；`desktop/linux/` 的宿主经 purego 加载 GTK 与 WebKitGTK，构建不需要 GTK 头文件。`scripts/desktop/smoke-linux.sh` 在 Xvfb（`xvfb-run`）中启动打包好的应用，检查桥接、音频、保存与退出。

Windows 未安装 GNU Make 时，可在 PowerShell 中运行：

```powershell
./scripts/desktop/build-windows.ps1
./scripts/desktop/build-windows.ps1 -Package
./scripts/desktop/build-windows.ps1 -Package -Cuda  # 另生成 CUDA 12.4 / 13.3 完整安装包
python scripts/package-cli.py
```

模型转换与 App 打包独立。准备兼容的 Kokoro 模型时，可在仓库中运行 `python scripts/prepare-kokoro-models.py --help` 查看用法。

## 测试

```sh
pnpm --dir web exec playwright install chromium
make check
```

`make check-core` 检查 Go 格式与静态分析，并运行带竞态检测的测试；`make check-web` 执行前端构建与浏览器测试。原生安装和 GPU 推理还需在目标平台验证。

## 声纹识别开发与验证

非实时识别原型使用外部配置的 Python 3.12 环境、`sherpa-onnx==1.13.8`、`numpy==2.5.3` 和 WeSpeaker ONNX 模型。yovoice 不会安装、下载或打包这些内容。

使用 `uv` 准备隔离环境；必要时将缓存放到仓库外：

```sh
UV_CACHE_DIR=/private/tmp/yovoice-uv-cache uv venv artifacts/recognition/venv --python 3.12
UV_CACHE_DIR=/private/tmp/yovoice-uv-cache uv pip install --python artifacts/recognition/venv/bin/python -r scripts/recognition/requirements.txt
make check-recognition
```

运行 CLI 或 HTTP 识别前，将 `YOVOICE_SPEAKER_PYTHON`、`YOVOICE_SPEAKER_MODEL` 和 `YOVOICE_SPEAKER_RUNNER` 设置为绝对路径。模型和测试录音位于被忽略的 `artifacts/recognition/`，通过 `scripts/recognition/eval-manifest.json` 核对 SHA-256。不要提交或再分发这些本地文件，也不要从 Apache-2.0 源码许可推定它们的许可。

要运行带断言的本地 CLI 与 HTTP 回归，先构建被忽略的 CLI 产物，将开发用 FFmpeg 放到相邻目录，为全部运行时参数设置绝对路径，再选择一个新的证据目录：

```sh
repo=$(pwd -P)
(cd "$repo" && go build -o artifacts/yovoice ./cmd/yovoice)
mkdir -p "$repo/artifacts/tools"
ln -sfn /opt/homebrew/bin/ffmpeg "$repo/artifacts/tools/ffmpeg"

export YOVOICE_SPEAKER_PYTHON="$repo/artifacts/recognition/venv/bin/python"
export YOVOICE_SPEAKER_MODEL="$repo/artifacts/recognition/models/wespeaker_zh_cnceleb_resnet34.onnx"
export YOVOICE_SPEAKER_RUNNER="$repo/scripts/recognition/runner.py"
export YOVOICE_SPEAKER_THRESHOLD=0.6
export YOVOICE_SPEAKER_MARGIN=0.05
export YOVOICE_SPEAKER_TIMEOUT=120s

YOVOICE_EVAL_OUTPUT="$repo/artifacts/verification/YYYYMMDD-speaker-regression-rN" \
  "$repo/scripts/recognition/run-eval.sh"
```

评估器从 `scripts/recognition/eval-manifest.json` 读取每个本地资产的哈希、查询角色、预期结果和固定决策配置。输出目录已存在，或阈值、间隔、超时被改动时，脚本会拒绝执行；阈值 `.6` 与 `0.6` 视为同一个固定值。它为 CLI 和自身启动的回环 HTTP 服务创建独立临时数据目录，登记两个档案，将两段独立的已知查询分别断言为对应登记 ID，要求未知查询没有选中的档案，并检查两次列表均为两个档案。HTTP 登记必须返回 201，识别和列表必须返回 200。业务动作前后都会记录源码、binary、模型、runner、manifest、全部五条样本哈希和计算得到的 feature version 指纹，任一输入变化都会失败。`summary.json` 只保存公开 JSON 响应、每个动作的退出码/状态/失败分类和耗时、工具/包版本、预期计数以及实际完成/正确/拒绝计数；随后删除包含私有特征的临时数据目录。`PASS` summary 表示这些断言全部完成。

每次运行均使用新的 `YYYYMMDD-speaker-regression-rN` 目录；保留下来的目录是证据，绝不覆盖。该命令使用开发机 FFmpeg（已准备环境为 9.0.2），不是项目打包的 FFmpeg 8.1.2，因此验证了本地识别流程，未验证发行包的音频转换。小样本分母为 2 个已知匹配查询和 1 个未知非匹配查询。这是带断言的集成回归，不代表人群准确率、真实 TTS 音质、GPU、Windows 或防伪验证。

## 项目结构

进程、请求链路与数据目录见[架构](architecture_zh.md)。

| 目录 | 用途 |
| --- | --- |
| `cmd/` | CLI 和本地服务入口 |
| `internal/` | Go 服务实现，按职责分包（见下） |
| `desktop/macos/`、`desktop/windows/`、`desktop/linux/` | 原生 App 宿主 |
| `web/src/` | 前端应用 |
| `web/browser-tests/` | 浏览器测试 |
| `scripts/` | 构建与打包工具 |

### 架构约定

- 前端依赖方向为 `app → features → shared`，`shared` 不引用业务模块。
- Go 服务按职责分包，依赖只能自上而下：

  | 层 | 包 | 职责 |
  | --- | --- | --- |
  | 传输 | `desktop`、`remote` | 桌面本机服务与 RPC 分发；远程 HTTP API、异步任务与 MCP |
  | 应用 | `workbench` | 作品、素材、角色、模型、运行时、生成与工程归档 |
  | 基础 | `engine`、`store`、`download`、`audio` | 推理进程、状态持久化、下载解包、音频解析与转码 |
  | 模型 | `schema`、`catalog` | 持久化结构与校验；模型目录与生成参数 |
  | 叶子 | `msg`、`diag`、`platform` | 消息码、诊断日志、平台差异 |

  传输层只解码请求并调用 `Workbench` 的公开方法，不访问其内部状态。`testkit` 仅供测试引用。
- 桌面调用并发执行。`Workbench` 用编辑锁串行化“读取 → 校验引用 → 写入”的短事务，转码、下载、推理与工程导入导出不持锁；后台操作同一时间只运行一个。
- 模型目录、生成参数和 OmniVoice 属性同时嵌入 Go 服务并供界面引用，`internal/catalog/*.json` 与 `web/src/shared/lib/*.json` 需保持一致（`engine.json` 仅供服务端与构建脚本使用，没有界面副本）。
- 消息码在 `internal/msg` 中声明，须加入 `msg.All`，并在 `web/src/shared/i18n/catalogs/` 的每种界面语言中提供文案（英文是源语言，也是缺词时的回退）。新增界面语言时在 `web/src/shared/i18n/locale.ts` 和 `schema.UiLocaleNames` 中登记，并在 `catalogs/index.ts` 中添加目录和 Astryx 语言包的加载。
- 升级 audio.cpp 时只修改 `internal/catalog/engine.json`：`version` 是推荐版本，`minimum` 是请求格式与模型包所需的最低版本，另含运行包校验值；在线安装与 `scripts/desktop/` 构建脚本都从它读取。已安装内核的版本记录在状态中，低于 `minimum` 时必须升级，低于 `version` 时界面提供可选升级。只有确实依赖新内核时才提高 `minimum`。
- Windows `-Package` 生成完整 CPU 安装包与只含应用本体的 `-update.exe`；自动更新只下载后者，`engine/` 目录及其中的 `yovoice-engine.json`（内置内核的版本与后端）在更新时保持不变。

后三项由 `go test` 校验。

公开指南使用英文 `*.md` 和中文 `*_zh.md`。修改用户可见行为时，同步更新两种语言及其链接。

从 v0.1.7 起，CHANGELOG.md 的发布说明需分别包含 `### English` 和 `### 简体中文` 两部分。发布流程在构建前检查两种语言是否齐全。
