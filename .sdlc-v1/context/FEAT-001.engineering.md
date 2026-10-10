# FEAT-001 — 非实时声纹识别与音色绑定

## Plan（唯一有效工程方案）

主 Agent 汇总有界分析并确认以下方案；关联 ready 的 `REQ-001.product.md` 和 `project.md`。开发分支 `codex/speaker-recognition-mvp`，基线 HEAD `2401e134d3b0ecd0f25f52a8e1d1e4527159dfde`。单个 TASK-001 串行实现，由主 Agent 验收。用户在看到完整 46 文件交付后，于 2026-10-10 确认提交/推送；授权范围为 fork `arnow117/yovoice` 的该功能分支源码交接及其必要补证、生命周期记录，不包括合入主分支、GitHub 二进制发行、生产部署或 GB 级 TTS 下载。新增接口/数据方案在实现前完成独立对抗性复审。

## 已观察事实与复用判断

- `cmd/yovoice/main.go` 已独占 `service.lock`；`remote.API.busy` 对昂贵请求不排队；`Workbench.mu/cancel/done` 管理全局后台任务及退出，`edit → mu` 是既有锁顺序。
- `startGeneration` 在 edit 内校验并启动，之后删除声音会因生成运行状态被拒绝；`Synthesize` 支持请求取消、等待收尾并返回历史结果。
- `decodeGeneration` 在绑定解析前调用 `schema.Validate`；OmniVoice clone、VoxCPM continuation 缺原文会提前失败。CLI 也在打开 Workbench 前根据 reference/voice 推导模式并校验，profile 不能简单作为新字段透传。
- `audio.Convert` 固定 mono/24 kHz/PCM16；新识别需独立 mono/16 kHz/PCM16 转换，不能改变旧 TTS 预处理。`ImportVoice` 会写声音库，查询识别不能调用它制造持久素材。
- `Store.writeState` 已用同目录临时文件/Sync/Close/Rename；`Update` 还保留旧 state 备份，不能直接用于要求删除不复活的私有声纹数据。
- 主 Agent 的 `artifacts/verification/baseline/summary.json` 记录同 HEAD 核心 race、Web 84、Swift 5 和原生替身检查通过；这不覆盖新功能或实模 TTS。
- 识别资产已准备：Python 3.12.13、sherpa-onnx/core 1.13.8、NumPy 2.5.3，WeSpeaker zhResNet34 ONNX 26,534,363 bytes、256 维、CPU 一线程。模型本地 SHA256：`f86cd6c509f331f0e20b07bd48d1b2eb7de54202643401c4e84695ac861a0e5a`。
- `recognition-runtime-smoke.json`：同人独立查询 cosine 0.818149 / 0.838868；未知最佳 0.464850。范围仅官方示例、原生 WAV，最终预处理与 CLI/API 链路仍需重跑；下载后本地 hash 不是发布者签名证明。

| 候选 | 复用覆盖 / 成本 | 决定 |
|---|---|---|
| sherpa-onnx Python CPU 子进程 | 现有资产已实际计算特征；Python 包提供专用 embedding API，包元数据声明 Apache；Go 不引入 CGO。另需明确 ONNX/示例录音许可及独立运行配置 | 推荐引入成熟依赖，首版外置运行环境，不改 App 打包 |
| 当前 audio.cpp / Go 适配器 | 当前 yovoice 只集成 TTS / vdes 请求及 WAV 返回；没有可复用的通用声纹输出契约，需要扩大上游接口分析或实现 | 本次不采用，不推断上游完全没有该能力 |

官方 v1.13.8 示例使用 `SpeakerEmbeddingExtractorConfig`、`create_stream/accept_waveform/input_finished/is_ready/compute`，CPU/一线程及阈值 0.6；复用该 API，不引入示例的麦克风、sounddevice 或 soundfile 流程。[官方示例](https://raw.githubusercontent.com/k2-fsa/sherpa-onnx/v1.13.8/python-api-examples/speaker-identification.py)

## 最小职责与运行配置

- 新 `internal/recognition/`：私有文件格式/验证、版本标识、余弦排序/决策、外置 runner 协议与有界进程；新 `internal/workbench/speakers.go`：音频来源、绑定、edit 事务与同步操作；传输层只解码并调用公开 Workbench 方法。
- 新 `scripts/recognition/runner.py`、固定依赖清单、Python 单测及可重跑评估资产；音频转换新增识别专用函数，复用打包 FFmpeg 的定位、协议/容器白名单和进程管理。
- 昂贵登记/识别同步占用既有 `mu/cancel/done` 操作槽，不能只用一个独立识别锁绕过 TTS 的 busy。新操作不广播带新消息码的 Activity，不把向量或运行配置加入 `schema.State`，无需 Web/TS 修改。
- 操作槽先在 mu 内检查 closed/cancel、登记派生 ctx/cancel/done，再无锁运行；结束释放槽并通知 done。`Cancel/Close` 能取消并等待识别进程，不持 mu 等待；任何同时用到 edit/mu 的路径仍 edit 在先。
- 配置的识别 timeout 从取得本次操作槽起覆盖转码、特征提取和登记提交前检查，不只包住 Python。最后接受点通过并成功落盘时，以本次操作保存的成功终态为准；请求取消分支等待收尾后也不能把这次成功改报 cancelled。
- 识别操作的 timeout context 直接从调用方 context 派生，不能从 Background 派生后只依赖等待函数转发取消；登记接受点检查须观察同一父请求的取消。已取消的新登记/识别/profile 合成请求不得启动昂贵动作，完整旅程测试覆盖提交前取消无迟到登记，并保留接受点后成功的既有断言。
- 必需本地环境变量：`YOVOICE_SPEAKER_PYTHON`、`YOVOICE_SPEAKER_MODEL`、`YOVOICE_SPEAKER_RUNNER`，均解析成操作者指定的本地绝对路径；不接收 HTTP 路径、程序或 provider 配置，不运行 shell、安装、下载或云端调用。
- 可选本地变量：`YOVOICE_SPEAKER_THRESHOLD=0.6`（有限值，范围 [-1,1]）、`YOVOICE_SPEAKER_MARGIN=0.05`（(0,2]）、`YOVOICE_SPEAKER_TIMEOUT=120s`（(0,10m]）；CPU 一线程固定。服务读取不可变配置快照；更换模型/runner 前停止服务，特征版本变化需重新登记。
- list/delete/profile-generate 不需要 Python 或 ONNX 就绪；enroll/identify 必须验证运行配置与特征兼容性，缺配置返回明确错误，空库也不能掩盖坏配置。
- 原型有效音频按主决定使用归一化 RMS ≥ 1e-4 加 extractor.is_ready，并拒绝非有限/零范数特征；不能可靠排除噪声/音乐、证明存在单人语音，不新增 VAD 依赖。

## 私有持久化与提交契约

| 对象 | 字段 / 限制 |
|---|---|
| `root/speakers.json` | `{version:1, profiles:[...]}`；最多 1000 档案、文件读取上限 8 MiB；未知版本、损坏 JSON、重复 ID、非法维度/数字均失败，不当作空库覆盖 |
| 私有 Profile | `id`（既有 32 位 hex ID）、`name`（可省略默认 id，trim 后 1–100 Unicode 字符）、`createdAt`（UTC）、`bindVoiceId`（可省略）、`modelSha256`、`featureVersion`、`dimension=256`、`embedding`（256 个有限 L2 归一化数值） |
| `featureVersion` | SHA256 指纹覆盖模型 hash、runner hash、sherpa 版本、预处理 `pcm16-mono16k-v1`、归一化/余弦 `l2-cosine-v1`、维度；T/M 不改变向量兼容性，但变化需重跑评估 |
| 对外 Profile | 只输出 `id,name,createdAt,bindVoiceId,modelSha256,featureVersion,dimension`，不输出 embedding、路径或执行配置 |

- 文件由 Workbench 独占数据目录和 edit 保护；按需要懒读取，私有文件损坏不阻止旧 TTS/App 启动，但所有相关档案操作失败。识别复制完整档案快照，不在计算期间持 edit。
- 登记：短事务验证来源/绑定并复制输入到私有临时目录 → 无 edit 转码/提取 → edit 内重读档案/重查 bindVoiceId 和文件存在 → 准备完整新文件 → 最后 ctx 检查 → Sync/Close/Rename → 返回成功。容量/版本/随机 ID 冲突在提交前验证。
- **主收敛的取消接受点**：edit 内最后一次 ctx 检查通过即进入短原子提交区；此前取消不得提交，此后保存成功返回成功，不能在 Rename 后再把成功改报 ctx 错误。发布前检查最终序列化字节数不超过 8 MiB，超过返回 capacity；Rename 之前失败保留旧文件，不把半写文件发布。成功删除不通过进程重启或内部备份复活；本原型不承诺断电/系统崩溃的文件系统耐久性。
- 删除用同一接受点重写剩余档案，无可恢复旧向量备份；不存在 ID 返回已删除。成功后任何新操作不再使用该档案；开始较早的识别允许旧快照，profile-generate 必须重新检查存在。
- 临时文件权限 0600、目录 0700；所有返回路径清理。崩溃遗留的专用 `downloads/recognition-*` 与 `.speakers-*` 临时物由持有 service.lock 的启动清理处理，绝不恢复为档案或删除其他素材。
- 模型/runner hash 在提取前验证并纳入快照，提取后确认未变；兼容性检查包括每份旧档案，不能静默跳过不兼容档案返回 unknown。新文件不进入 state 主副本/备份，也不经 state.get、/v1/status 或普通日志泄露。

## Python runner 信任协议

- Go 使用指定 Python 的 `-I` 执行指定 runner（二者为本地受信配置），过滤 Token/PYTHONPATH 等无关凭据；`exec.CommandContext` 配合 `platform.ConfigureProcess/KillProcess`、有界等待和 deadline 清理进程组及管道。
- stdin 只含一个内部 JSON：`protocolVersion:1, modelPath, modelSha256, audioPath`；Go 创建的 PCM16/mono/16k WAV 路径和本地配置由 Go 组合，不透传远程原始 JSON。runner 重新核对模型 hash 和音频格式/长度，读取标准 wave + NumPy，实际核对安装的 sherpa-onnx / core 均为 1.13.8，固定 CPU/一线程；不能只回填版本常量。
- 成功 stdout 恰好一个 JSON：`protocolVersion:1, modelSha256, runtimeVersion:"1.13.8", dimension:256, embedding:[...]`；失败输出约定 `errorCode` 并非零退出。禁止 stdout 日志、多对象/尾随内容，Go 严格解码/校验；stdout 上限 64 KiB，stderr 上限 8 KiB，超过即失败并终止，不把 stderr/traceback原样返回或写日志。
- runner 失败的公开分类由主明确固定：`invalid_audio` / `feature_not_ready` → `audio`（HTTP 400），`invalid_feature` → `feature`（400）；`invalid_request` / `model_mismatch` / `runtime_failed` / 非允许码或非法失败 JSON → `runtime_failed`（502）。只返回允许的通用码，不返回 runner 原文。
- 进程是本地受信程序执行，不是沙箱安全边界；本原型不抵御操作者故意替换 Python/runner/model。协议校验防误配置、失控输出和不兼容结果，不把 runner 自述当作文件来源证明。

## CLI 与 HTTP 实现契约（实现并验证前不视为可用用法）

| 入口 | 输入 | 成功输出 |
|---|---|---|
| CLI `speakers enroll` | `--file AUDIO` 或 `--voice ID` 恰一；可选 `--name`、`--bind-voice ID`；沿用 `--data-dir/--json` | 对外 Profile JSON；每次登记新 ID，无重试去重 |
| CLI `speakers identify` | `--file AUDIO` 或 `--voice ID` 恰一；决策参数来自本地环境 | Identify JSON，matched/unknown/ambiguous 均 exit 0 |
| CLI `speakers list/delete ID` | list 无参数；delete 接 32 位 ID | list 为 Profile 数组；delete `{id,deleted:true}`，重复删除同义 |
| CLI `generate --speaker-profile ID` | 现有正文/模型/情绪/输出参数；禁止同时自行指定主要 `--voice/--reference/--reference-text`；`--speaker` 含义保持 | 既有 `{id,path,duration,model,voice}` 加 `speakerProfile`，voice 为实际绑定 ID |
| HTTP `POST /v1/speakers` | JSON `{voiceId,name?,bindVoiceId?}`，或 octet-stream 音频 + query `name?/bindVoiceId?`；两者互斥，不接受路径；音频 ≤20 MiB、JSON ≤64 KiB | 201 + Profile JSON |
| HTTP `POST /v1/speakers/identify` | JSON `{voiceId}`，或 octet-stream 音频；输入来源恰一，禁多余查询/JSON字段 | 200 + Identify JSON |
| HTTP `GET /v1/speakers`、`DELETE /v1/speakers/{id}` | 原有 Bearer / Origin 规则；合法 ID | 200 + Profile 数组；200 + `{id,deleted:true}` |
| HTTP `POST /v1/speakers/{id}/generate` | ≤1 MiB JSON：text + 原 SynthesisSettings，profileId 在 path；主要 voiceId/referenceText 由绑定提供，显式自行提供拒绝 | 200 WAV，保留 X-Yovoice-Generation-ID，可增 X-Yovoice-Speaker-Profile；取消/超时沿用同步生成 |

- Identify 字段：`status`、`reason`（matched/empty_library/below_threshold/close_candidates）、`profileId`（仅 matched）、`score`（非空库最佳分数）、`candidates`（最多 3 个 `{profileId,name,score}`）、`modelSha256,featureVersion,threshold,margin`；按真实分数严格降序、完全同分按 ID 稳定排序，epsilon 不能用于排序比较器。先 <T 判断 unknown，再判断并列/差值<M，决策边界用明确 1e-12 比较精度；并列必须 ambiguous，包括 M 小于比较精度时。
- 新 HTTP 错误统一 `{error:{code,message}}`：400 invalid_input/audio/feature/unsupported_clone；404 profile/voice_missing；409 busy/capacity/feature_mismatch；413 input_too_large；503 runtime_not_configured；502 runtime_failed；500 persistence_failed；504 deadline；408 cancelled（连接仍可写时）。401/403 保留既有认证响应。CLI 新错误可增加 code 字段、非零退出，旧错误/旧正常结果保持原格式。
- 所有新 HTTP 入口继承先鉴权、拒绝带 Origin、no-store 和 nosniff；enroll/identify/profile-generate 先取得共享 API.busy 再读有界请求并调用核心，不修改旧 /v1/generate、jobs 或 MCP 的字段/幂等语义。

## 绑定合成与旧入口兼容

- 抽出严格 JSON 解码/单对象/正文检查 helper；旧 decodeGeneration 仍在其后立即 Validate，新 profile 路由解析绑定/模式后再 Validate。不把 profileId 加入 SynthesisSettings，旧 /v1/generate 继续拒绝该未知字段。
- 在 edit 内查 profile、bindVoiceId 与参考文件，注入现有 VoiceID 和保存的 ReferenceText，再校验克隆能力：IndexTTS；VoxCPM clone/continuation；OmniVoice clone；Qwen Base。拒绝 Kokoro、CustomVoice、VoiceDesign 及显式 design 模式。
- 新 profile 分支默认 VoxCPM 有原文时 continuation、无原文时 clone；OmniVoice 默认 clone 且缺原文按已有规则失败；显式 Vox clone 与非空绑定原文冲突时说明需 continuation。IndexTTS 保持原引擎请求不透传不支持的原文；其余家族按现有 BuildRequest 处理。
- 避免“解析绑定 → 解锁 → 启动生成”的删除竞态：抽出 startGeneration 的锁内部分，profile 解析与开始生成处于同一 edit 短事务，不在 edit 内等待推理。新 SynthesizeProfile 等待本次操作自己的 done、cancel 与结果；不能直接调用现有 Synthesize 的全局 Cancel/Activity 等待逻辑。旧启动/生成调用保留原签名，内部可用窄 helper 复用。
- 新 profile 生成必须持有本次操作 handle，错误/Generation 结果在释放任务槽前捕获，避免下一任务覆盖 Activity、删除历史或迟到取消误作用于另一任务。开始后取消只调用本次 cancel。增加确定性的 A 完成→B 接手→A 处理取消测试，证明 B 不受影响、A 结果可独立读取。
- profile 生成的后台错误在进入既有 start 收尾、诊断、Activity 和 last-error.txt 之前转换为稳定通用错误；保留 context 取消/超时分类，不能把 PathError、runner/native stderr 或文本写入新增流程错误。验证新响应、旧 /v1/status 的 Activity 及该流程诊断/last-error 三处无私有路径标记。既有无 profile 入口不扩展为全仓日志改写。
- 脱敏还必须覆盖 profile 流程启动、历史保存及最终 Activity 收尾的持久化错误：允许窄改 internal/store/store.go，为该流程提供返回/诊断均使用通用错误的写入路径，旧 Update 语义保持。不能只在 action 返回后处理，因为 Store.Update 可能先记录路径。操作 handle 的 terminal error 包含收尾保存失败；即使 action 已产出音频也不能将收尾失败报告为成功。增加确定性收尾保存失败测试，检查响应、Activity、diag/last-error 及本次 handle 的错误。
- 代码检查确认 Engine 会在外层错误转换之前保存 native 错误响应，并把进程 stdout/stderr 直接写日志。允许窄改 `internal/engine/engine.go`：profile 使用专用受限诊断调用，禁止保存该请求的原始 native 响应/stdout/stderr；旧 Generate 调用及其诊断保留。私有与普通推理进程的复用 key 必须区分此模式，切换时不能复用另一模式的日志目标。用失败引擎输出私有路径标记，断言该 profile 请求的所有诊断文件和 Activity 都没有标记。
- CLI profile 分支在打开 Workbench 后完成绑定/模式/Validate；不能在前置参数检查阶段误判为缺参考或声音设计。新 profile CLI 可走同步 SynthesizeProfile 后复制 WAV，保持输出不覆盖；引用输入用私有临时副本，文件删除竞态不破坏正在提取的查询。
- 普通 `Synthesize` 的错误文案、终态 code/params 与原 Activity 契约保持一致：普通底层错误保留 `ErrUnknown` 的 detail，最终 Activity 保存失败优先返回 `ErrStateSaveFailed` 及其 detail。可以捕获本次终态，但不得以简化的 operation 原始错误丢弃旧调用方信息；profile 专用脱敏不影响普通入口。
- profile CLI 同样处理既有 `--emotion-reference`：在进入 `SynthesizeSpeaker` 前完成情绪素材导入与 `EmotionVoiceID` 设置；主要参考仍仅由绑定提供。用 IndexTTS CLI 假引擎断言实际 native 请求的主要参考与情绪参考分别对应正确媒体文件，不以文字情绪用例代替参考情绪。
- 新 profile 错误分类显式映射 `msg.ErrGenerateFailed` 为 `runtime_failed`（HTTP 502），最终 `ErrStateSaveFailed` 仍为 `persistence_failed`（500）；返回正文、Activity 和诊断继续脱敏，普通入口不改。CLI/HTTP 必须分别断言推理错误分类，原测试中 500 推理失败断言须纠正，不能用宽泛任意错误断言替代。
- profile 调用方 deadline 导致本次操作取消时，将终态 `context.Canceled` 的返回归因恢复为调用方 `DeadlineExceeded`（HTTP 504）；主动取消仍为 cancelled（408）。仅恢复取消归因，不覆盖已经接受的成功或最终非取消持久化失败，不调用共享全局 Cancel。用有界阻塞假引擎验证 HTTP timeout 与主动取消，保留交接、提交成功及最终保存失败用例。
- 新 profile 路由/CLI 按“是否显式提供”拒绝主参考输入，包括 JSON `voiceId:null`、`referenceText:""` 与空值 reference/voice flags；旧入口的解码行为不因此改变。新路由用 `url.ParseQuery(RawQuery)` 并拒绝解析错误，不能让非法转义或分号中的未知参数被 `URL.Query()` 静默丢弃。严格 JSON 的尾部解析须保留 `MaxBytesError`，包括仅尾随空白超限的输入。

## TASK-001 完整实现交付

标题：`实现非实时声纹 CLI/API、私有档案与迭代验证`；无 Task 依赖，同一 Terra worker 负责实现、必要测试、纠正、usage 和双语 CHANGELOG，交由主 Agent 接受。

- 写边界：新 `internal/recognition/**`、`internal/workbench/speakers*.go`，必要窄改 workbench/generation/cleanup、store 的 profile 专用持久化诊断路径及 engine 的 profile 专用受限诊断路径；识别音频转换函数；`cmd/yovoice/**`、`internal/remote/api.go/speakers*.go` 及相关测试；`scripts/recognition/**`；README 双语入口、docs usage/cli/api/development 双语说明、CHANGELOG、必要依赖许可证说明。
- 禁止边界：Web/桌面宿主、MCP 新工具、TTS 模型训练、现有 State/TS 增加声纹字段、旧异步 job 请求语义、自动模型安装/迁移、生产部署。Makefile 若新增检查入口限与本交付有关，不改既有 check 行为。
- 完成条件：产品 SCN-01…16 均有确定性证据，profile 特征不泄露、旧入口兼容；新 runner 可取消/受限/严格校验；最终预处理后真实 2 登记/2 独立查询/1 未知的 CLI 和 HTTP smoke 达到预期；错误、重启、删除、busy、提交前取消/提交先完成及绑定失效均覆盖；真实 TTS 音质单列未验证。
- usage：新建唯一 `docs/usage.md` 双语全局导航，用稳定 USG-SPEAKER-ENROLL/IDENTIFY/GENERATE/DELETE/VERIFY ID 映射规格场景、链接详细双语 CLI/API/开发指南及参数 owner；README 指向该入口。新命令、环境和 eval 操作必须在实际运行后写成可用用法，不能以脚手架/帮助页通过代替业务操作。

## 验证、风险与主 Agent 汇总点

- modes：correctness、e2e:OpenAPI、eval-bench；roles：server-dev、architect、security、qa。本范围无 Web 旅程变化，不引入 tester-army；上游 Web 仅作补充回归，若范围后续影响 Web 再按对应约束处理。
- 实现后运行 `make check-core`（gofmt/vet/race），受影响 CLI/API/WorkBench 隔离集成；API httptest 覆盖鉴权/Origin/路径注入/边界/错误/取消/并发/重启。纯 Go 子进程测试助手可模拟协议，不能算真实模型结果。
- Python 必要静态与单测覆盖 WAV、RMS、ready、JSON/维度/非有限/零向量、格式与错误输出；worker 先实际验证命令和测试收集，再写 usage。固定依赖由 uv 管理，不把仓库外个人 Python 环境当交付前提。
- 准备提交的 eval manifest（资产来源/hash/标签/预期/配置）及 `scripts/recognition/` 的可重跑 runner；大文件仍在 ignored artifacts，manifest 指向 hash 验证后的文件。输出按本次运行保存到 artifacts/verification，绑定 commit/配置/样本分母/得分/耗时/真实与模拟范围；不能覆盖旧失败证据或动态调阈值隐去失败。
- 在已准备 CPU 环境实际按 usage 执行新 CLI 和真实 HTTP 录音流程，T=0.6/M=0.05 为官方示例起点且未校准，不能根据当前 5 段 smoke 宣称人群准确率；最终流水线结果才构成首版 baseline。
- 新功能与旧 TTS 同步链路用假引擎证明参数选择和取消；官方识别示例不拿来 TTS 克隆。缺 GB 级 TTS 模型/许可素材/目标设备，实模生成、听测、GPU、Windows/完整原生包均诚实标未验证，不能用静音 WAV 替代。
- 上游 `make check-web` 作为最终补充回归；原生/发布脚本未修改时已有同版本证据可引用，实际 App/Windows 包未测试仍不可声称通过。
- 主已固定上述字段、错误、容量、超时、比较精度与写范围；无需修改旧 State/TS 或已有 jobs。无 UI/移动旅程变化，Web/App E2E 对本 Feature 不适用，既有浏览器套件仅作补充回归。
- 风险/回滚：新识别仅需去掉运行配置和停止使用新入口，保留 speakers.json 供显式处理；旧 State/TTS 无迁移。私有持久化和公共接口需独立 Sol review，关注删除复活、取消结果、数据泄露和旧输入行为。
- 交付后按产品 7 天窗口由用户/开发者观察误匹配、拒绝率、时延和绑定失效；记录小样本 baseline 与分母，新增范围/反馈回流 Requirement，不新增自动监控任务。

## 主 Agent 采用的验收补充

- REQ-001 → FEAT-001 → TASK-001；无平行业务写入者，同一 Terra worker 负责实现及纠正。架构复审是一次只读分析，汇总点是本 Plan；后续独立验证/评审仍以实际 commit 为准。
- 资产来源：sherpa/WeSpeaker 源代码许可引用官方 Apache-2.0 文件；不能将源代码许可推定为所有权重/录音的再分发许可。ONNX 与公开示例录音仅用于当前本地识别实验，模型、音频、个人环境、Token 不提交，manifest 记录来源与本地 hash；不作商业再分发或用户素材采集声明。
- 测试质量：recognition 新包的业务、持久化、协议校验及取消/错误路径须有有效断言，目标行覆盖至少 80%；Python runner 做隔离测试及必要静态检查。不得放宽旧断言或无限重试得到通过。
- 行覆盖不代替业务断言：通过完整 Enroll/Identify/Delete 旅程验证查询只读、同音频/标签重复登记产生不同 ID、删除及真实关闭重启后不复活且 voice/history 保留、识别与删除重叠返回完整快照。假引擎须核对实际 native 请求中的参考文件/参考原文（按模型家族）、正文及情绪，而不是只接受正文后返回静音；成功与失败的 CLI/HTTP profile 入口均有实际断言。受限诊断测试须真正输出 stdout/stderr/body 的私有标记并检查所有相关诊断。结果独立性须在 A 已完成、B 接手后再让 A 调用方取结果/处理取消时验证。识别转码 WAV 本身也须 0600；完全同分且极小 M 应有专门断言。
- SCN-06 必须用单候选明确构造 score == T，断言 matched、正确 profileId 及实际分数；同一组覆盖明确低于 T 为 unknown 且无选定 ID。T 附近排序、多候选分差与实模 unknown 不代替该边界断言。此次补证只修改识别测试，不改产品决策实现。
- 配置快照由 Workbench 持有且不可变；list/delete/bound TTS 不强制加载模型，不影响旧功能启动。新 API 错误使用稳定通用文本，不泄露 runner stderr、特征或服务器路径。
- 近静音+ready 只拒绝已知无效输入；不推断任意噪声/音乐、多人、防伪能力。档案显示名不证明现实身份。
- 真实 TTS 模型、可用克隆素材与听测环境尚缺，交付证据须单列音质未验证；先完成真实识别及参数绑定链路。
- 回滚不删除用户资料：停止新入口/清除配置可保留旧 TTS；保留 speakers.json 供用户明确处理，不自动重置或恢复删除过的特征。

## Validation

正式验证通过（2026-10-10，implementation commit `b2d86f773692db7d58c371b38054e4c2ac114481`）：同一独立 Sol validate Agent 在业务树干净时重验，主核对日志、SHA 与实际树后记录 pass。`gofmt -l cmd internal`、`go vet ./...`、`go test -json -race ./... -count=1 -timeout=90s`（22.752s，93 顶层／157 含子用例，13 包，0 失败／测试 Skip）、`make check-recognition`（4+1）、recognition 覆盖率（84.4%）、CLI build 与 VCS metadata 核对均 exit 0。新精确 T 与低于 T 两个子用例实际执行通过，原等于 M 保留，SCN-01…16 均 COVERED；首轮 fail 历史保留。

证据：`artifacts/verification/20261010-speaker-commit-validate-r2/summary.json`、`commands.json` 和原始日志；实模 `20261010-speaker-commit-eval-r2/summary.json` PASS，CLI/HTTP 各 2 登记、2/2 已知查询正确 ID、1/1 未知拒绝、列表 2，HTTP 201/200。固定 T=.6/M=.05/120s/256维/CPU一线程，A/B score .81814938/.83886787，未知 .46485008；运行前后实际重读模型、runner、manifest、五样本、binary、源码且一致，临时私有库清理、HTTP 子进程退出 0。41 份业务文件聚合 SHA256 `9ed78c3a36df5d347d9d12195072bb1e3d5012a6577db119295e37446b961c9a`，featureVersion `c240757a39516f94237b42893337156ba47d69b40e722d02897944c199975219`。

未变 Web 84、Swift 5、原生／发布替身和评估器拒绝保护仅复用相同输入/环境的已运行证据。真实 TTS／听测、GPU、Windows、完整原生包、发行 FFmpeg 8.1.2 继续 NOT_RUN，不外推人群准确率或音质。正式代码评审仍 pending。

正式验证首轮（2026-10-10，commit `7ae4ffa11a9438629d54701408cfdfaf9489c7ca`）：独立 Sol validate Agent 在业务树干净时完成格式、vet、非缓存全量 race（92 个顶层／154 个含子用例测试，13 包，0 失败／Skip）、Python 4+1、83.6% 覆盖率、当前 commit CLI build 及真实 CLI/HTTP 2 登记／2 已知／1 未知识别，所有命令 exit 0，源码和资产前后一致。完整记录为 `artifacts/verification/20261010-speaker-commit-validate/summary.json` 与 `20261010-speaker-commit-eval/summary.json`。但 SCN-06 的 score == T 单候选断言缺失，归因为 INCONCLUSIVE；主已记录 validation fail 并恢复同一 TASK-001，仅交回原 worker 补测试。此 fail 表示必要验证未完整，没有观察到实现失败。下列提交前映射须以此更正为准。

阈值补证提交前接受：仅新增 `TestIdentifySingleCandidateThresholdBoundary`，明确断言实际 score == .6 的 matched/正确 ID，以及 score == .59 的 unknown/below_threshold/无选定 ID，均要求单候选。worker 的目标 race、`make check-core`、覆盖率和 diff 检查 exit 0；覆盖率 84.4%。主只读复核 diff 并执行阈值／既有分差／精确排序三个测试的非缓存 race（30s 上限），exit 0。日志在 `artifacts/verification/20261010-speaker-threshold-fix/attempt3-*`。首次可选 .61 用例逐位相等假设因归一化浮点变化失败，日志保留；已移除该可选用例，精确 .6 的真实计算严格相等断言保留。没有修改生产实现或放宽必需边界；TASK-001 可恢复 done，正式验证仍须绑定新 commit。

主 Agent 于 2026-10-10 接受最终未提交业务快照的实现及必要补证；HEAD 为 `2401e134d3b0ecd0f25f52a8e1d1e4527159dfde`，41 份业务文件内容 SHA256 为 `d50c854f333e8c7b151a140c8dc58f09dcd29bd530d0f19208969808536bb504`。这是提交前验收，不是正式 lifecycle validation/review pass；业务工作树尚未提交，正式验证须绑定业务代码干净的实际 commit。

| 检查 | 实际结果与证据 |
| --- | --- |
| 最终主验收 | `gofmt -l cmd internal` 无输出；`go vet ./...`；`go test -race ./... -count=1 -timeout=90s`（22.557 秒）；`git diff --check` 均退出 0，源码前后一致。原始日志、命令、退出码及指纹在 `artifacts/verification/primary-20261010-speaker-acceptance-r2/summary.json`。 |
| worker 完整检查与新增边界 | `make check-core`、有界 SCN-07 audio/Workbench race、`make check-recognition`、CLI build 均退出 0；原始日志在 `artifacts/verification/20261010-speaker-final-r7/`，测试期间业务文件未变。 |
| Python 与覆盖率 | runner 4 项及 evaluator 1 项实际通过；recognition 覆盖率 83.6%。主独立证据在 `primary-20261010-speaker-acceptance/summary.json`；r7 再次通过。最终只变动两个 Go 测试文件，Python/runner/运行配置未变，允许复用主证据。 |
| 指南实际操作 | 主逐条执行 `docs/development_zh.md` 的构建、配置和 `run-eval.sh`，退出 0；完整 CLI 和自身启动的回环 HTTP 识别评估 PASS，证据在 `artifacts/verification/primary-20261010-speaker-guide-eval/summary.json`。其后只添加 Go 测试，模型/样本/runner/manifest/binary/指南完全相同。 |
| 最终实模快照 | `artifacts/verification/20261010-speaker-eval-r13/summary.json` PASS；CLI/HTTP 各 2 登记、2/2 独立已知查询匹配正确 ID、1/1 未知查询为 unknown、列表 2。模型、五样本、runner、manifest、binary、源码在运行前后重新读取且一致；含 embedding 的临时数据目录已移除。主核对当前实际文件与该记录全部一致。 |
| 评估器保护 | 主实际验证旧输出目录与 T=0.5 均退出预期 2，旧 summary 字节未变，拒绝的运行未建立输出目录；证据为 `primary-20261010-speaker-acceptance-r2/asset-and-guard-checks.json`。 |
| 原有前端补充回归 | 主本轮 `make check-web` lint/build/84 项 Playwright 全通过，退出 0；见 `primary-20261010/check-web-network.log`。Web 输入未变，未重复执行。Swift 5 项及发布脚本检查只复用未变原生/发布输入的初始基线，见 `baseline/summary.json`。 |

T=0.6、M=0.05、120s、256 维、CPU 一线程。实模 A/B 查询相似度分别约 0.81814938 / 0.83886787，未知查询最佳约 0.46485008；分数不是概率，小样本不代表人群准确率。Python 3.12.13、sherpa-onnx/core 1.13.8、NumPy 2.5.3、开发 FFmpeg 9.0.2。发行包 FFmpeg 8.1.2、真实 TTS 合成/听测、GPU、Windows 和完整原生打包为 NOT_RUN；假引擎 WAV 只证明输入选择与生命周期，不证明音质。

场景与实际测试映射（最终全量 Go race 已执行下列 Go 用例；Python 用例另由 check-recognition 执行）：

| 产品场景 | 实际断言来源 |
| --- | --- |
| SCN-01 | `TestRepositoryRoundTripDoesNotExposeEmbedding`、`TestSpeakerListSurvivesWorkbenchRestart`、完整登记/删除/重启旅程，以及 CLI/HTTP 实模登记与列表。 |
| SCN-02 | `TestIdentifyDecisionBoundariesAndCompatibility`、`TestSpeakerEnrollIdentifyDeleteAndRestartJourney` 的查询前后状态/档案比较，实模两个不同查询正确 ID。 |
| SCN-03 | 决策测试的空库 unknown，无 score/候选；`TestInvalidRecognitionAudioDoesNotMutateWorkbench` 最后合法输入在空库 unknown。 |
| SCN-04 | COVERED：实模 unknown，无选定 ID；`TestIdentifySingleCandidateThresholdBoundary` 另明确断言 .59 < .6 时 unknown/below_threshold 且不选中 ID，在 b2d86f7 正式重验通过。 |
| SCN-05 | 决策分差、精确排序与同分 ambiguous；`TestIdentifySortsByExactScoreBeforeDecisionTolerance` 含 M=1e-14 同分，无选定 ID。 |
| SCN-06 | COVERED：`TestIdentifySingleCandidateThresholdBoundary` 严格断言实际 score == .6 和 matched；原等于 M 边界保留，b2d86f7 正式重验通过。首轮 PARTIAL 历史保留。 |
| SCN-07 | `TestRecognitionWAVValidatesRecognitionPCM16Bounds` 的空/损坏/截断/近静音/1与60秒边界；完整 `TestInvalidRecognitionAudioDoesNotMutateWorkbench` 登记与识别均拒绝且不写状态/档案/临时目录；copy 字节增长/20MiB、HTTP 输入限额及显式空名、Profile 超长名称测试。 |
| SCN-08 | Config 绝对路径/限额、Extract 严格协议/文件变化/失败码、旧 feature version 拒绝；Python ready/维度/零向量/非有限特征/实际包版本校验。 |
| SCN-09 | 完整登记/识别/删除/重启旅程：重复删除、正常关闭重开不复活、绑定 Voice 与 History 保留、已删档案合成拒绝。 |
| SCN-10 | `TestSynthesizeSpeakerUsesBoundVoiceAndReturnsOwnedGeneration`、CLI `TestGenerateThroughSpeakerProfileUsesBoundVoice`、HTTP `TestSpeakerGenerationRouteUsesBindingAndRedactsPrivateDiagnostics`；native 精确参考路径、Vox 原文、Index 调用方正文/情绪。 |
| SCN-11 | 完整 HTTP→Workbench 的未绑定、已删 Voice、缺失 profile、Kokoro、显式空/null/冲突参考输入拒绝；每次拒绝比较 History 和 outputs 文件列表不变。 |
| SCN-12 | `TestSpeakerRoutesApplyAuthenticationOriginAndInputBoundaries`、严格 JSON/query 测试：Token、Origin、任意服务器输入、多余字段、非法/重复 query。 |
| SCN-13 | 父取消不能启动操作、登记提交前取消、Repository 接受点取消、提交成功后迟取消仍成功、converter/runner 超时及有限管道输出。 |
| SCN-14 | API busy、共享槽超时释放、受控操作交接/独立 cancel 与 Generation、完整旅程识别与删除重叠的旧完整快照及后续不可再匹配。 |
| SCN-15 | 完整旅程同音频/同标签显式重复登记获得不同 ID，不覆写旧档案；决策同分 ambiguous。 |
| SCN-16 | 全量 core 的原有 CLI/API/MCP/字幕/Voice/状态回归、普通 TTS 原错误 detail/最终保存错误优先级、普通 Engine 日志正控；未变 Web 的 84 项补充回归。 |

旧 r4/r10/r5 报告、挂起测试与修正历史保留在 Review 和 artifacts；不以这些旧快照为最终业务代码背书。

## Review

正式评审修正提交前接受：原 Terra 仅改 CLI profile 情绪参考导入、SpeakerErrorCode 的 Generate/StateSave 映射、profile 等待的调用方 deadline 归因，以及必要 CLI/Workbench/HTTP 测试与双语 CHANGELOG，共 6 个业务文件。新 CLI 测试捕获实际 native 请求并严格对照主参考和独立情绪参考的媒体路径/内容；HTTP 502/500、有限阻塞下504/408及原真实 marker 脱敏都通过。已成功/最终持久化失败优先及普通合成契约保留。worker 目标 race、vet、非缓存 full race（90s）、core、Python4+1、recognition race覆盖84.7%及diff检查均exit0，日志及复现失败保存在 `artifacts/verification/20261010-speaker-review-fix/`。主只读核对全部6文件diff，另非缓存race执行CLI参考、HTTP分类/取消、Workbench隐私/最终保存失败五个目标测试，3包exit0。TASK-001 done；待新commit正式验证/重审，不以前一commit的pass代替修正版。

首次正式代码评审（2026-10-10，b2d86f7）：新独立 Sol `speaker_formal_review` 审阅基线至 validation commit 的全部 41 业务文件，确认业务 clean/验证版本一致、guide→USG→参数→实际范围可对应，未发现有证据支持的重复实现或可避免工程债。提出三个 P2：profile CLI 在情绪参考导入前 break，合法 `--emotion-reference` 未设置 EmotionVoiceID；ErrGenerateFailed 未映射落入 persistence_failed（500）而非约定 runtime_failed（502），现有 HTTP 测试固化错误；profile 等待在调用方 deadline 后取消 Background 派生操作，返回 Canceled 而非 DeadlineExceeded（预期504）。主只读核对接受源码定位，记录 changes_requested 并恢复同一 TASK-001；上面 Plan 已补明确验证/恢复策略，原 Terra 窄修，保留 b2d86f7 validation pass 历史，修后重验/重审。

评审证据：business diff/状态检查与41文件指纹一致；HTTP现有脱敏旅程的有界非缓存 race 在允许回环网络后 exit 0（受限首轮失败保留，非业务通过证据），明确实际执行了当前错误的500断言。情绪参考及deadline由具体调用链定位，没有用已通过测试掩盖未覆盖分支。本轮不改变产品范围、不新增第三次架构对抗复审。

正式验证补证定位（2026-10-10）：`recognition_test.go` 的 T=.6、score 1/.95/.97 只覆盖等于 M 的接受；另一个用例覆盖 T 附近排序及同分，不包含单候选 score == T。主只读核对接受该缺口，以同一 TASK 和原 Terra worker 添加明确等于／低于阈值的回归测试，保留首轮通过命令及 fail 记录，不扩大实现范围。

最终提交前接受（2026-10-10）：主只读复核 r7 新增的 WAV 表驱动及完整 Enroll/Identify 无效输入旅程，确认调用实际产品路径而非仅 helper，精确边界/错误、全状态不变、临时清理和槽复用均有断言。主再次非缓存全量 Go race 退出 0，41 文件内容指纹未变；当前生产实现、Python/资产与主实际指南评估相同，最新 r13 双入口实模也通过。所有已定位的实现/验证问题已关闭，TASK-001 可置 done；此为实现最终接受，正式 SDLC validation/review 仍待提交后的干净 commit，不提前记录 pass。

主在 r6 冻结业务快照上已独立完成格式/vet、强制非缓存全量 race、Python 5 项、83.6% 覆盖及指南实模 CLI/HTTP，全部退出 0，40 份业务文件前后未变；最新 marker proof 与完整 SCN-11 History/outputs 不变断言已关闭上轮两项 P2。证据在 `artifacts/verification/primary-20261010-speaker-acceptance/` 和 `primary-20261010-speaker-guide-eval/`。整理 SCN 表时发现此前未直接测试新 Go RecognitionWAV 的 1/60 秒边界与完整登记/识别的无效音频副作用；同一 worker 仅补这组本地测试，不扩产品或实现，修改后须更新相应快照证据。所有先前日志保留，尚未作 TASK 完成或正式验证决定。

最终 r5 窄源码复核确认普通合成同步、精确参考绑定、操作交接结果、tiny-margin 同分和资产重读已关闭；其定向日志实际退出 0。仍定位到两个测试缺口：HTTP 引擎复用启动环境使 marker 请求被正文断言提前拒绝，尚未证明私有 stdout/stderr/body 脱敏；SCN-11 完整 HTTP→Workbench 拒绝旅程尚未比较 History/输出副作用。已将这两处具体定位交回同一 worker，保留之前证据，源码冻结后再执行主验收。此记录不是正式生命周期 review 决定。

r10 的真实 CLI/HTTP 识别评估 PASS，主已核对 39 份业务源码和 binary/model/runner/manifest 与该快照一致；该结果仅覆盖识别旅程。最终窄检查实际执行 `go test -race ./internal/workbench -run '^TestSynthesizePrioritizesFinalStateSaveFailure$' -count=1 -timeout=8s` 超时，定位到测试等待的 hook 只在非 nil result 时触发，而普通 Synthesize 传 nil。因此 r10 的全量 core 通过报告未被接受，必须修正后实际结束并保存新 raw 日志。CLI/HTTP 成功用例仍未触发新的 native 参数断言、绑定参考只查非空、返回后的 History 修改不证明操作交接；这些不视为完成。评估器的 after 模型 hash 使用缓存且未重读样本，亦须改为真正运行前后核对。保留旧证据及失败历史，下一轮仍由同一 worker 做具体定位修正，不能用重复运行代替修正。

实现前独立协议复审由 Sol reviewer 完成（2026-10-10，源码基线 HEAD）。主接受 3 个真实问题：操作自己的取消/结果、profile 后台错误在共享收尾前转换、序列化提交大小上限；相应约束已纳入 Plan。第四项是未明确的系统崩溃耐久性，采用有界原型取舍：承诺进程重启/不通过内部备份复活，暂不承诺断电文件系统耐久性。没有修改业务代码或运行测试；此复审不替代实现后的正式代码评审。

第二且最后一次协议复审确认其余修正，补出同一路径的一处缺口：共享收尾的 Store.Update 会自身产生并记录路径错误，handle 还需包含收尾失败。主已将底层 profile 专用写入/诊断和该失败断言加入上述有效 Plan；不启动第三次协议复审，交由实际实现与测试证据验证。

实现期间的独立只读代码检查（2026-10-10，草稿而非正式 SDLC Review）补出有源码依据的问题：完整操作超时、提交后取消终态、runner 管道读取次序及真实包版本、严格排序、输入限额/错误分类/多余 query/文件增长、空白名称，以及 Engine 原始诊断。主接受这些定位，并在当前 Task 中修正；新增写权限只覆盖上述 Engine 私有诊断路径，仍不新增用户功能或第三次协议复审。最终交付必须以更新后的源码和测试证据重新核对，草稿检查不构成通过决定。

r4 稳定核心快照的增量只读检查仍定位到 P2 问题：普通 Synthesize 错误详情/最终持久化错误优先级回归、JSON 尾随空白超限分类、非法 query 被丢弃、显式空/null 参考输入未拒绝。主同时明确 runner 失败分类，并接受删除/重启/只读/重复登记/并发快照及实际 native 参数、stdout/stderr 隐私测试的有效断言缺口。原型的 83.5% 覆盖率及 2+1 真实识别结果不替代这些验收；由同一 TASK-001 worker 按上面更新后的有效 Plan 修正，保留 r4 证据但尚未接受。已有小范围 race 与 Python 检查通过不代表本次新定位已修复；后续以新源码指纹、针对性断言及最终完整检查为准。
