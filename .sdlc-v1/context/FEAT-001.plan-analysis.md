# FEAT-001 — 有界工程分析与方案候选

本文是主 Agent 汇总输入，不是生效 unified Plan；关联 ready 的 `REQ-001.product.md` 和 `project.md`。只读采集 HEAD `2401e134d3b0ecd0f25f52a8e1d1e4527159dfde`，分支 `codex/speaker-recognition-mvp`；本分析没有执行新测试、安装或业务代码修改。

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
- **主收敛的取消接受点**：edit 内最后一次 ctx 检查通过即进入短原子提交区；此前取消不得提交，此后保存成功返回成功，不能在 Rename 后再把成功改报 ctx 错误。保存失败保留旧文件并返回失败，不把半写文件发布。
- 删除用同一接受点重写剩余档案，无可恢复旧向量备份；不存在 ID 返回已删除。成功后任何新操作不再使用该档案；开始较早的识别允许旧快照，profile-generate 必须重新检查存在。
- 临时文件权限 0600、目录 0700；所有返回路径清理。崩溃遗留的专用 `downloads/recognition-*` 与 `.speakers-*` 临时物由持有 service.lock 的启动清理处理，绝不恢复为档案或删除其他素材。
- 模型/runner hash 在提取前验证并纳入快照，提取后确认未变；兼容性检查包括每份旧档案，不能静默跳过不兼容档案返回 unknown。新文件不进入 state 主副本/备份，也不经 state.get、/v1/status 或普通日志泄露。

## Python runner 信任协议

- Go 使用指定 Python 的 `-I` 执行指定 runner（二者为本地受信配置），过滤 Token/PYTHONPATH 等无关凭据；`exec.CommandContext` 配合 `platform.ConfigureProcess/KillProcess`、有界等待和 deadline 清理进程组及管道。
- stdin 只含一个内部 JSON：`protocolVersion:1, modelPath, modelSha256, audioPath`；Go 创建的 PCM16/mono/16k WAV 路径和本地配置由 Go 组合，不透传远程原始 JSON。runner 重新核对模型 hash 和音频格式/长度，读取标准 wave + NumPy，固定 sherpa-onnx 1.13.8、CPU/一线程。
- 成功 stdout 恰好一个 JSON：`protocolVersion:1, modelSha256, runtimeVersion:"1.13.8", dimension:256, embedding:[...]`；失败输出约定 `errorCode` 并非零退出。禁止 stdout 日志、多对象/尾随内容，Go 严格解码/校验；stdout 上限 64 KiB，stderr 上限 8 KiB，超过即失败并终止，不把 stderr/traceback原样返回或写日志。
- 进程是本地受信程序执行，不是沙箱安全边界；本原型不抵御操作者故意替换 Python/runner/model。协议校验防误配置、失控输出和不兼容结果，不把 runner 自述当作文件来源证明。

## 拟新增 CLI 与 HTTP 契约（尚未实现，非已可用命令）

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

- Identify 字段：`status`、`reason`（matched/empty_library/below_threshold/close_candidates）、`profileId`（仅 matched）、`score`（非空库最佳分数）、`candidates`（最多 3 个 `{profileId,name,score}`）、`modelSha256,featureVersion,threshold,margin`；按分数降序、同分按 ID 稳定排序，先 <T 判断 unknown，再判断并列/差值<M，边界用明确 1e-12 比较精度。
- 新 HTTP 错误统一 `{error:{code,message}}`：400 invalid_input/audio/feature/unsupported_clone；404 profile/voice_missing；409 busy/capacity/feature_mismatch；413 input_too_large；503 runtime_not_configured；502 runtime_failed；500 persistence_failed；504 deadline；408 cancelled（连接仍可写时）。401/403 保留既有认证响应。CLI 新错误可增加 code 字段、非零退出，旧错误/旧正常结果保持原格式。
- 所有新 HTTP 入口继承先鉴权、拒绝带 Origin、no-store 和 nosniff；enroll/identify/profile-generate 先取得共享 API.busy 再读有界请求并调用核心，不修改旧 /v1/generate、jobs 或 MCP 的字段/幂等语义。

## 绑定合成与旧入口兼容

- 抽出严格 JSON 解码/单对象/正文检查 helper；旧 decodeGeneration 仍在其后立即 Validate，新 profile 路由解析绑定/模式后再 Validate。不把 profileId 加入 SynthesisSettings，旧 /v1/generate 继续拒绝该未知字段。
- 在 edit 内查 profile、bindVoiceId 与参考文件，注入现有 VoiceID 和保存的 ReferenceText，再校验克隆能力：IndexTTS；VoxCPM clone/continuation；OmniVoice clone；Qwen Base。拒绝 Kokoro、CustomVoice、VoiceDesign 及显式 design 模式。
- 新 profile 分支默认 VoxCPM 有原文时 continuation、无原文时 clone；OmniVoice 默认 clone 且缺原文按已有规则失败；显式 Vox clone 与非空绑定原文冲突时说明需 continuation。IndexTTS 保持原引擎请求不透传不支持的原文；其余家族按现有 BuildRequest 处理。
- 避免“解析绑定 → 解锁 → 启动生成”的删除竞态：抽出 startGeneration 的锁内部分，profile 解析与开始生成处于同一 edit 短事务；之后复用 Synthesize 等待/取消/历史结果，不在 edit 内等待推理。没有 profile 的旧 CLI 分支仍按原解析/生成行为执行。
- CLI profile 分支在打开 Workbench 后完成绑定/模式/Validate；不能在前置参数检查阶段误判为缺参考或声音设计。新 profile CLI 可走同步 SynthesizeProfile 后复制 WAV，保持输出不覆盖；引用输入用私有临时副本，文件删除竞态不破坏正在提取的查询。

## TASK-001 单个完整实现交付建议

标题：`实现非实时声纹 CLI/API、私有档案与迭代验证`；无 Task 依赖，同一 Terra worker 负责实现、必要测试、纠正、usage 和双语 CHANGELOG，交由主 Agent 接受。

- 写边界：新 `internal/recognition/**`、`internal/workbench/speakers*.go`，必要窄改 workbench/generation/cleanup；识别音频转换函数；`cmd/yovoice/**`、`internal/remote/api.go/speakers*.go` 及相关测试；`scripts/recognition/**`；README 双语入口、docs usage/cli/api/development 双语说明、CHANGELOG、必要依赖许可证说明。
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
- 主决定已明确 RMS+ready 与提交接受点，无阻塞产品问题；仍需主在统一 Plan 固定上述字段/错误/容量/超时/比较精度和源码范围，确认 ONNX/示例许可表述，再派 TASK-001。本候选不自行生效。
- 风险/回滚：新识别仅需去掉运行配置和停止使用新入口，保留 speakers.json 供显式处理；旧 State/TTS 无迁移。私有持久化和公共接口需独立 Sol review，关注删除复活、取消结果、数据泄露和旧输入行为。
- 交付后按产品 7 天窗口由用户/开发者观察误匹配、拒绝率、时延和绑定失效；记录小样本 baseline 与分母，新增范围/反馈回流 Requirement，不新增自动监控任务。
