# Project Context: yovoice 声纹识别二次开发

## 当前开发范围（2026-10-19）

REQ-002 / FEAT-002 在 `codex/speaker-validation-ui` 开发原 Mac App 的声纹页面；依赖已交付的 FEAT-001。
有效产品与工程上下文为 `.sdlc-v1/context/REQ-002.product.md` 和 `.sdlc-v1/context/FEAT-002.engineering.md`。
范围包含桌面私有 RPC、临时录音与文件、档案管理、显式绑定生成及独立本地验证包；不改原 CLI/v1、State 或私有档案格式。
当前业务提交 `a001a0f` 对应的新包已由主 Agent 使用正常 LaunchServices 启动和 Computer Use 验收：原生文件面板登记 A/B、两个独立已知查询及一个未知查询均正确，删除 B 后正常重启仍只保留 A。合计2次启动、5次本地单线程提取，653秒内完成清理；普通 State/Voices、源码、App/Web和固定资产三阶段一致，自有进程均退出。当前正式独立 Validate/Review 待执行；历史 smoke 显示失败仍保留，原始证据及各层边界见工程上下文 Validation。
新的 Web runner 与本地 Mac 验证命令已实际执行，入口和证据边界见下表。
用户已授权本轮功能分支 commit/push、必要修正与独立正式验证/评审；本次完整原生旅程已通过并消费完一次预算，不再自动启动 App 或模型。后续按已有证据适用性执行独立阶段；主线合并或二进制发行仍不在本次范围内。

## 项目定位与采集边界

- 当前上游是本地 TTS 创作工具：文字、字幕和参考音频输入，WAV、角色与作品输出；支持 macOS / Windows App、独立 CLI、HTTP API / MCP。
- 二次开发目标是增加声纹识别；采集时上游未提供说话人识别或声纹 embedding，现有 `speaker` 是内置 TTS 音色或字幕角色字段。FEAT-001 新增独立私有 profile CLI/API，不提供 diarization，也不重用旧 speaker ID。
- 采集日期：2026-10-10；分支 `codex/speaker-recognition-mvp`；源码 HEAD `2401e134d3b0ecd0f25f52a8e1d1e4527159dfde`。
- 本文件记录源码、配置及验证覆盖边界。初始 onboard 仅采集命令；后续主 Agent 的实际基线见下文，FEAT-001 的实现与验收证据见其工程上下文。存在命令不代表该版本已通过。

## 已接受的开发基线

主 Agent 于 2026-10-10 在上述 HEAD 实际完成 `make check-core`（格式、vet、全部 Go race 测试）、
`make check-web`（lint、构建、84 项浏览器测试）、`swift test --package-path desktop/macos`（5 项测试），全部退出 0。
`scripts/test-release-notes.py` 的 2 项测试以及 `check-macos-run.py`、`check-macos-update.py`、`check-signing.py`
也退出 0；后三项使用隔离替身，不代表真实安装、签名或公证。原始日志与环境/资产摘要位于
`artifacts/verification/baseline/summary.json` 及同目录日志。

识别运行时实验采用 Python 3.12.13、sherpa-onnx / sherpa-onnx-core 1.13.8、NumPy 2.5.3，CPU 单线程；
官方 WeSpeaker 中文 ResNet34 ONNX 和五段独立示例录音的本地 SHA256 已记录。256 维特征提取实际通过，
实验结果在 `artifacts/verification/baseline/recognition-runtime-smoke.json`。这尚不是产品接口或识别人群准确率验收。
真实 TTS、GPU 推理、完整原生打包及 Windows 执行尚未验证。

## 技术栈与来源（当前配置）

| 区域 | 技术与版本 | 用途 / 来源 |
|---|---|---|
| Web | Node.js 22+、pnpm 9.15.2；React 19.2、TypeScript 5.9、Vite 7.3、Astryx 0.6.1 | 共用桌面界面与浏览器开发预览；`web/package.json`、`AGENTS.md` |
| 服务 / CLI | Go 1.26+；MCP Go SDK 1.8 | 本地业务核心、独立 CLI 与远程接口；`go.mod`、`cmd/` |
| macOS | SwiftPM tools 5.9、AppKit、WKWebView；macOS 14+、arm64 | 原生窗口、权限、菜单和桥接；`desktop/macos/Package.swift` |
| Windows | .NET 8、WPF、WebView2；Windows 10/11 x64 | 原生宿主和安装器；`desktop/windows/`、开发指南 |
| 推理 | audio.cpp 推荐 `v0.9.1`、最低 `v0.7.4`；CPU / Metal / Vulkan / CUDA 12.4 / 13.3 | 固定运行包、SHA256 校验；`internal/catalog/engine.json`，版本与最低兼容版本分开 |
| 模型 | IndexTTS 2/2.5、VoxCPM2、OmniVoice、Qwen3-TTS、Kokoro | 当前均用于 TTS / 声音设计；`internal/catalog/catalog.json` |
| 构建工具 | Python 3.12+、Make、Bash、curl、C 编译器；精简 FFmpeg 8.1.2 | 音频转换、原生依赖提取与打包；开发指南、`scripts/build-ffmpeg.py` |
| 持久化 | JSON 主副本 / 备份、媒体文件；预览使用 localStorage + IndexedDB | App 默认 `~/.yovoice`，无数据库服务；`internal/store/store.go`、`preview-backend.ts` |
| 外部依赖 | GitHub Release、Hugging Face / ModelScope / mirror、PyPI、FFmpeg 源码 | 运行包、模型及构建依赖下载；模型权重不随 App 打包 |

本机工具链由主 Agent 确认：arm64、Go 1.26.3、Node 25.9、pnpm 11、Swift 6.3；Python 3.12.13 位于父工作区 `.venv/bin/python3`。默认 pnpm 版本与项目固定版本不同；复现应使用 9.15.2。原生构建脚本调用 PATH 上的 `python3`、`pnpm`，需确保实际解析到符合要求的版本。

## 启动与代码入口

| 入口 | 位置 / 已存在的命令 | 说明 |
|---|---|---|
| 浏览器预览 | `make dev` → `web/src/main.tsx`、`web/src/app/app.tsx` | Vite `http://127.0.0.1:5173`；没有原生桥时走预览后端 |
| macOS App | `make app-run`；`desktop/macos/Sources/VoiceWorkbenchMac/main.swift` | Swift 宿主 → 随包 Go 服务 → WKWebView；重建前退出 App |
| Windows App | `desktop/windows/VoiceWorkbench/App.xaml.cs`、`MainWindow.xaml.cs` | WPF / WebView2 调用共用 Go 业务核心 |
| 本地服务 | `cmd/yovoice-service/main.go`、`internal/desktop/server.go`、`rpc.go` | `127.0.0.1` 随机端口；`POST /api/call`、`GET /api/state-events`、`/media/` |
| 独立 CLI | `cmd/yovoice/main.go`；`make cli-build` | status / setup / models / voices / generate / serve；独立 `--data-dir` 避免目录锁冲突 |
| 声纹 CLI / HTTP | `internal/recognition/`、`internal/workbench/speakers.go`、`internal/remote/speakers.go`；[使用导航](../docs/usage.md) | speakers enroll / identify / list / delete、generate --speaker-profile；`/v1/speakers` 及 profile generate。需显式本地识别配置；不自动下载，不新增桌面入口 |
| 远程 API / MCP | `yovoice serve`；`cmd/yovoice/serve.go`、`internal/remote/` | 默认 `127.0.0.1:8080`；`/v1/status`、models / voices / generate / jobs / audio，`/mcp` |
| 业务 / 推理 | `internal/workbench/` → `internal/engine/engine.go`、`request.go` | 单后台操作、编辑短事务；audio.cpp `/health` 与 `/v1/tasks/run` |
| 契约 / 目录 | `internal/schema/`、`internal/catalog/`、`web/src/shared/workbench.ts` | Go/TS 共享字段、模型能力、生成参数与消息码 |

## 已有检查与构建入口

以下命令来自 Makefile、package scripts、CI 或原生脚本。已实际执行的初始基线在上文；当前 Feature 的执行时间、源码指纹及退出结果在工程上下文，不将入口清单视为通过记录。

| 范围 | 命令 | 实际覆盖 / 前置条件 |
|---|---|---|
| 依赖准备 | `make install`；`pnpm --dir web exec playwright install chromium` | frozen lockfile；安装 Playwright Chromium，需网络 |
| 核心完整检查 | `make check-core` | `gofmt -l cmd internal` 检查、`go vet ./...`、`go test -race ./...` |
| 推理 / 下载修改 | `go test -race ./...` | 根 AGENTS 明确要求；含请求映射、取消、并发、下载、状态恢复测试 |
| Web 完整检查 | `make check-web` 或 `pnpm --dir web run check` | 依次 lint、TypeScript/Vite build、Playwright；UI 修改三个检查均需执行 |
| Web 分项 | `pnpm --dir web run lint`；`pnpm --dir web run build`；`pnpm --dir web test` | build 内含 `tsc -b`；独立 typecheck script 为 `none` |
| 声纹 Web 旅程 | 在 `web/` 执行 `E2E_TELEMETRY_DISABLED=1 ./node_modules/.bin/e2e run --config e2e.config.ts --workers 1 --retries 0 --strict-cache` | tester-army CLI 与 Web engine；只读缓存、确定性业务断言、中文 1280×840 / 英文 390×600，两目标 58 项；合成桥接不执行模型 |
| 声纹验证脚本 | `python3 -B scripts/desktop/check-speaker-validation-macos.py` | 22 项隔离替身检查，覆盖配置、有限退出、可见性与构建凭据；不启动真实 App/模型 |
| 声纹本地 App | `scripts/desktop/build-speaker-validation-macos.sh`；`scripts/desktop/smoke-speaker-validation-macos.sh` | 当前全局操作入口为 `docs/usage.md`；完整签名后生成同级 receipt，smoke 启动前核对源码与包指纹；使用独立资料目录、固定本地 CPU 模型及五段样本，真实运行需要既定授权 |
| Swift 单元检查 | `swift test --package-path desktop/macos` | 版本 / 更新解析与窗口位置；需要 macOS / Xcode CLT |
| 原生脚本检查 | `python3 scripts/desktop/check-macos-run.py`；`python3 scripts/desktop/check-macos-update.py`；`python3 scripts/desktop/check-signing.py` | 隔离替身验证日志、更新回滚和签名调用顺序；不代表真实安装 / 公证通过 |
| App 构建 | `make app-build` | 本机平台构建；macOS 含 Web build、Swift、Go、内核下载校验、FFmpeg 与 Kokoro 库；不运行测试 |
| macOS 原生验证 | `scripts/desktop/smoke-macos.sh`；`python3 scripts/desktop/check-macos-service.py` | 需要已构建 App；smoke 需 debug 宿主与桌面会话；服务检查使用临时数据和凭证 |
| 打包转换器检查 | `YOVOICE_TEST_FFMPEG="$PWD/artifacts/macos-arm64/yovoice.app/Contents/Resources/tools/ffmpeg" go test -race ./internal/audio -run '^TestAudioConversion$' -count=1` | CI 已有入口；PATH 仍需完整 ffmpeg 生成输入，否则测试会 Skip |
| Windows 验证 | `dotnet run --project desktop/windows/ServiceChecks`、`UpdateChecks`；`scripts/desktop/check-windows-update.ps1`、`check-windows-setup.ps1` | Windows 实机 / CI；本机 macOS 不能证明 WebView2、安装或 CUDA 行为 |
| 发布准备 | `make app-package`；`make cli-package`；`python3 scripts/test-release-notes.py` | 打包与双语说明检查；签名、公证和发布依赖授权及外部凭据 |
| 声纹 runner / 覆盖检查 | `make check-recognition`；`go test -cover ./internal/recognition` | uv 隔离 Python、runner 协议及模型输入检查；recognition 目标有效覆盖 ≥80%，覆盖率不代替业务断言 |
| 声纹真实模型 smoke | `scripts/recognition/run-eval.sh`；配置见 [开发指南](../docs/development_zh.md#声纹识别开发与验证) | 固定模型与五段样本、T/M；新输出目录保存 CLI/HTTP 结果与源码/二进制/工具指纹。最终验收前以工程上下文记录的实际结果为准 |
| TTS 自动听测 | `none` | 假引擎只验证参数/生命周期，不验证音质；目标设备上的真实生成及人工听测仍需单独准备 |

## 验证资产、环境和证据边界

| 层 | 当前资产 / 输出 | 结论边界与准备缺口 |
|---|---|---|
| correctness | `cmd/**/*_test.go`、`internal/**/*_test.go`；临时目录和 testkit | Go/TS 契约、竞态与确定性流程；`RunFakeEngine` 返回一秒静音 WAV，不能证明实模合成质量 |
| Playwright 补充回归 | `web/playwright.config.ts`、`web/browser-tests/*.spec.ts`；`@playwright/test` 1.63.0 | 启动 Vite 4173、viewport 1440×920；具体 Chromium 版本运行时记录；默认浏览器项目，无跨浏览器矩阵 |
| 浏览器测试身份 / 数据 | localStorage fixture、预览后端、部分 `chrome.webview` 消息替身 | 无远程登录身份；验证编辑、导入、保存、时间线与错误显示；预览不执行模型安装或推理 |
| SDLC `e2e:Web` | `web/e2e.config.ts`、`web/e2e/*.e2e.ts`、合成桥接与固定 fixture；固定依赖见 `web/package.json`/lockfile | 两个 Chromium 目标，无 provider 调用、0 重试、只读/strict 缓存；报告默认在忽略的 `web/e2e-results/`，每轮归档后引用，不能用合成结果替代原生识别 |
| native desktop | Swift tests、macOS smoke/service、Windows Checks | 桌面宿主不是移动 App；`e2e:App` 的移动 Maestro Flow 不适用。WebView 旅程仍适用 `e2e:Web`，原生功能另跑目标平台检查 |
| macOS smoke | `/tmp/voice-workbench-smoke.*` 下 `smoke-result.json`、`smoke-error.txt`、`state.json`、`smoke.png` | 覆盖桥接、WAV 导入/解码、草稿保存、退出；不执行真实 IndexTTS 或识别模型 |
| 真实转换 | `internal/audio/convert_test.go`、打包 FFmpeg | 多格式/长音频/损坏输入/取消；检查是否收集且未 Skip，不能只看整体 exit 0 |
| 声纹 `eval-bench` | `scripts/recognition/eval-manifest.json` 与复跑脚本；本地 ONNX/五段示例录音不提交 | 固定 CPU 一线程、2 登记/2 独立已知查询/1 未知查询；保存原始分数、正确档案 ID、拒绝计数与耗时。样本规模不支持人群准确率、防伪、多说话人或噪声结论 |
| TTS 真实回归 | 模型目录及 CLI/App generate 入口存在；听测资产为 `none` | 固定文本、参考音频、seed 和参数，在目标硬件生成 WAV 并听测可懂度/音色/截断；假引擎与预览均不能替代 |

本机全局 pnpm 启动链曾在 Playwright webServer 阶段超时。已实际验证的本地回退为在 `web/` 使用 `./node_modules/.bin/eslint .`、`./node_modules/.bin/tsc -b`、`./node_modules/.bin/vite build`，并先以任务自有 Vite 提供 `127.0.0.1:4173`，再运行同一 Playwright 配置。保留原启动失败记录，不修改断言或用旧报告代替；测试结束只回收自有 Vite。具体命令/退出结果见 FEAT-002 Validation。

- 本地服务身份来自 `WORKBENCH_TOKEN`，运行目录/资源来自 `WORKBENCH_DATA`、`WORKBENCH_WEB`、`WORKBENCH_ENGINE`；远程 API 读取 `YOVOICE_API_TOKEN`（至少 32 字符），可配置 TLS。只记录来源，不记录凭据值。
- `web/test-results/`、`playwright-report/`、`artifacts/`、Swift `.build/` 已忽略；Playwright 未显式配置 trace/video/screenshot 或 JUnit，缺少稳定的成功证据归档策略。
- `.github/workflows/check.yml` 在 macOS 15、Windows 与 Linux CLI 执行检查/打包；失败时上传 `web/test-results/`，保留 7 天；安装包保留 14 天。无 tester-army 或真实模型质量任务。
- 本轮基线日志由主 Agent 放在 `artifacts/verification/baseline/`；今后按运行保留命令、退出码、工具/浏览器版本、commit、配置、产物标识、Skip/重试与模型结果，在 Feature 研发上下文引用，避免用旧 App 为新 HEAD 背书。
- 迭代选择：Go/CLI/API 或持久化修改跑 core 与受影响旅程；runner/模型/预处理/T/M 修改另跑 Python 和固定实模 smoke；Web 修改追加完整 Web 检查；原生/打包修改追加目标平台和实际打包转换器检查。日志中的缓存通过仅适用于相同源码、配置及环境；缺目标设备/模型的项列为未验证。
- 本轮实模识别使用开发机 FFmpeg 9.0.2，经 `artifacts/tools/ffmpeg` 定位；发行包 FFmpeg 8.1.2 的新识别转换路径未验证，不能由该 smoke 代替。

## Surface map

```yaml
surfaces:
  - name: web-ui
    globs: ["web/src/app/**", "web/src/features/**", "web/src/shared/ui/**", "web/src/shared/selector.tsx", "web/src/main.tsx", "web/public/**", "web/index.html"]
    roles: [client-dev, design, qa]
    modes: [correctness, "e2e:Web"]
  - name: shared-contract-and-web-tooling
    globs: ["web/src/shared/lib/**", "web/src/shared/i18n/**", "web/src/shared/workbench.ts", "web/browser-tests/**", "web/*.ts", "web/*.json", "web/*.yaml", "web/patches/**"]
    roles: [client-dev, architect, qa]
    modes: [correctness, "e2e:Web"]
  - name: transport-and-cli
    globs: ["cmd/**", "internal/desktop/**", "internal/remote/**"]
    roles: [server-dev, architect, security, qa]
    modes: [correctness, "e2e:OpenAPI"]
  - name: application-and-persistence
    globs: ["internal/workbench/**", "internal/store/**", "internal/schema/**"]
    roles: [server-dev, architect, security, qa]
    modes: [correctness]
  - name: inference-and-model-catalog
    globs: ["internal/engine/**", "internal/catalog/**"]
    roles: [server-dev, architect, qa]
    modes: [correctness, eval-bench]
  - name: speaker-recognition
    globs: ["internal/recognition/**", "scripts/recognition/**"]
    roles: [server-dev, architect, security, qa]
    modes: [correctness, "e2e:OpenAPI", eval-bench]
  - name: audio-download-and-platform
    globs: ["internal/audio/**", "internal/download/**", "internal/platform/**", "internal/msg/**", "internal/diag/**", "internal/testkit/**"]
    roles: [server-dev, security, qa]
    modes: [correctness]
  - name: native-desktop
    globs: ["desktop/macos/**", "desktop/windows/**"]
    roles: [client-dev, architect, security, qa]
    modes: [correctness, "e2e:Web"]
  - name: build-release-and-project-guidance
    globs: ["scripts/**", ".github/**", "Makefile", "go.mod", "go.sum", "AGENTS.md", ".editorconfig", ".gitignore", ".sdlc-v1/project.md"]
    roles: [ai-readiness, qa, security]
    modes: [correctness]
  - name: docs-and-agent-skill
    globs: ["docs/**", "skills/**", "README*.md", "CHANGELOG.md", "LICENSE", "THIRD_PARTY_NOTICES.md"]
    roles: [ai-readiness, skill-maintainer]
    modes: [correctness]
```

主要目录均已归类。应用层或基础层改动影响 Web 用户旅程时补 `e2e:Web`；涉及模型输入/输出质量时补 `eval-bench`；跨 surface 补 architect。映射不免除根 AGENTS 或验证模式的必要检查。

新增 `speaker-recognition` surface 对应已接受的 REQ-001 / FEAT-001；有效产品与工程上下文分别是
`.sdlc-v1/context/REQ-001.product.md` 和 `.sdlc-v1/context/FEAT-001.engineering.md`。
该 Feature 仅新增 CLI / HTTP 的非实时识别及显式档案绑定，不修改 Web 用户旅程。
真实识别评估必须经过产品的音频预处理与 CLI / HTTP 入口；直接调用 extractor 的准备实验不能替代它。
用户已确认完整源码提交与推送；首个实现 commit `7ae4ffa11a9438629d54701408cfdfaf9489c7ca` 已推送。
首轮独立正式验证的全部已执行检查通过，但 SCN-06 的精确 T 边界断言缺失，必要验证记 fail。
同一 TASK-001 已按 CLI 支持的 done → todo → in_progress 恢复，由原 worker 补测试后主接受并置 done；证据和历史见工程上下文。
补证后的 implementation commit `b2d86f773692db7d58c371b38054e4c2ac114481` 曾通过独立正式验证，随后首次正式review定位三处P2；原worker修正后的最终实现 `c1050c4346e67d277a9ffab355418aa1ecf42733` 已通过正式验证、独立review及源码交接。最终160个含子用例Go race、Python4+1、recognition84.4%、CLI/HTTP实模2+1全通过；首轮fail及changes_requested历史保留。
主 Agent 本轮补充 `make check-web` 已退出 0，
lint / build / 84 项 Playwright 全通过，日志在 `artifacts/verification/primary-20261010/check-web-network.log`；
首次受限网络的 pnpm fetch 失败保留在同目录 `check-web.log`。Web 源码及测试输入未修改，此结果仅用于原有前端补充回归。

## 工程约定与风险

- 以根 `AGENTS.md` 为准：中文回复/代码注释、文件名 kebab-case、Web `app → features → shared`；Astryx 组件和 token 优先，UI 编写前按其 CLI 流程查组件，不临时换 UI 框架。
- Go 依赖只能向下：desktop/remote → workbench → engine/store/download/audio → schema/catalog → msg/diag/platform；传输层不能直接操作 Workbench 内部状态。
- `Workbench.edit` 保护读/校验/写短事务，转码/下载/推理/工程导入导出不持该锁；后台操作同时只运行一个，数据目录有进程锁。新增识别任务必须先明确取消、排队、持久化和恢复契约。
- `internal/catalog/{catalog,generation_options,omni_attributes}.json` 与 Web 对应 JSON 保持一致；`engine.json` 无 Web 副本。消息码加入 `msg.All` 并提供中英文文本。
- 用户可见变化同步 `CHANGELOG.md` 的 Unreleased 与英文/中文指南；发布版本说明需同时含 English / 简体中文。
- Windows 应用更新仅替换应用，保留 `engine/` 和 `yovoice-engine.json`；低于 minimum 强制升级、低于推荐 version 可选升级。macOS 过期运行时可回退内置 Metal 内核；相关行为已有测试但 GPU 实机仍需验证。
- 日常 `~/.yovoice` 数据、模型和旧格式迁移属于敏感范围；回归使用独立数据目录，不能让测试修改日常作品。声纹档案只表示登记标签，私有特征保留至删除；删除/重启/取消及快照规则已由 REQ-001 产品上下文与 FEAT-001 Plan 固定。
- 模型大小可达 GB 级；真实模型、原生安装、CPU/Metal/CUDA 表现、识别准确率和 TTS 音质仍未由本 onboard 验证。OmniVoice 权重非商业许可，IndexTTS 及其他依赖按各自许可证。

## 部署与长期上下文

- 目标：macOS arm64 `.app` / DMG / ZIP、Windows x64 Setup / update / CUDA 包、Linux x64 独立 CLI；无独立 Web 生产站点或 staging 配置。
- 本次 FEAT-001 的已授权交付目标是源码分发：`https://github.com/arnow117/yovoice` 的 `codex/speaker-recognition-mvp` 分支。验证、评审后以 validation commit 交接，可追加仅含 `.sdlc-v1/**` 的记录提交；不合入 main，不创建二进制 Release 或部署 App。last-good 为上游基线 `2401e134d3b0ecd0f25f52a8e1d1e4527159dfde`；停止新入口与清除识别配置可保留旧 TTS，不删除用户档案。没有生产遥测，7 天观察由用户/开发者在本地记录误匹配、拒绝、时延与绑定失效，下次人工检查 2026-10-17；不创建自动监控。
- 构建/发布位置：`scripts/desktop/`、`scripts/package-cli.py`、`.github/workflows/{check,publish}.yml`；产物位于 `artifacts/`。签名、公证和 GitHub Release 凭据来自 CI secrets，发布需既有外部操作授权。
- 健康入口：远程认证后的 `/v1/status`、audio.cpp `/health`；本地桌面状态由 `state.get` / state-events 提供。
- 根指令：`AGENTS.md`；scoped AGENTS：`none`（本次文件扫描未发现）。
- canonical 文档索引：`README.md`、`README_zh.md`、`docs/development.md`、`docs/development_zh.md`、`docs/api.md`、`docs/api_zh.md`、`docs/cli.md`、`docs/cli_zh.md`、`docs/models.md`、`docs/models_zh.md`、`desktop/macos/README.md`；路径均存在。
- `.repo-context`：`none`，未发现 manifest/evidence，未把旧研究报告当作当前证据；源码采集 commit 为上述 HEAD。
- AI readiness：项目已有职责分层、固定前端依赖、跨端契约与多类测试；主要缺口是 SDLC Web runner、原生实机证据、真实模型质量评估与成功产物归档。
- 当前交付：用户已确认并完成源码推送，TASK-001 done，FEAT-001/REQ-001已完成约定功能分支源码交接。实现c1050c4的正式validation/review/source-only release均通过；最终160个含子用例Go race、Python4+1、recognition84.4%、双入口实模2+1及三项修正断言。远端SHA和资产版本已核对，证据见工程Release。旧fail与changes_requested历史保留；人工观察负责人用户/开发者，下次2026-10-17，真实TTS听测等未验证项继续明确保留。
