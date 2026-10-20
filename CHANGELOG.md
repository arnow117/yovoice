# Changelog

各版本的重要更新记录。日常改动记入 `Unreleased`；发布前合并重复项，整理为简短的功能、改进和修复说明。

## [Unreleased]

- 新增 Breeze TTS 2（中英，Q4 / Q8 / BF16）：可用文字描述设计音色，或用参考音频和原文克隆并以描述调整语速、情绪和语气；默认缩短分段、限制单段生成长度并在显卡上使用 bf16 推理，减少拖音和长段静音。模型需要 v0.9.1 内核，权重仅限非商业用途。
- 新建作品菜单新增“导入文件配音”：导入 TXT 或 Markdown 文本直接生成语音作品，导入 SRT、VTT、ASS 字幕生成故事作品；自动识别 UTF-8、UTF-16 与 GBK 编码，Markdown 的标记符号会被去掉。
- CLI 新增 `yovoice mcp`，以 stdio 方式提供 MCP，Claude Desktop、Cursor 等本机客户端可直接作为子进程启动，无需令牌；结果直接返回生成音频的本机路径，`upload_voice` 也可按本机路径导入参考音频。
- 设置 › 常规新增“本机接口”：开启后在 127.0.0.1 上提供与 `yovoice serve` 相同的 HTTP / MCP 接口（默认端口 8080，可修改），本机其他软件凭访问令牌即可调用桌面应用里的模型生成语音；令牌可复制、重置，端口被占用时直接提示。
- AI 服务新增 OrcaRouter（兼容 OpenAI 接口）：点“免费连接 OrcaRouter”在浏览器登录并同意授权后自动添加，默认使用免费模型，不用复制密钥；免费模型被拒绝时分别提示还需等待几秒、内容超过免费长度上限或需要绑定 GitHub。
- Windows 与 macOS 应用内更新提示：发现新版本时弹出提示并在侧栏显示下载进度，可查看对应语言的更新内容，下载完成后一键重启更新；设置页新增“版本与更新”，可随时检查更新，更新后首次启动会提示新版本内容。
- 修复语音生成面板中“更多情绪”与“微调情绪”折叠按钮残留边框与背景的问题，统一为轻量幽灵折叠外观。
- 修复缺少乐谱数据的旧编曲作品在启动或打开时导致整个窗口白屏的问题，这类作品会自动补上空乐谱。
- 侧栏导航与新建作品菜单将“语音生成”置于首位（排在“故事配音”上方）；桌面端重新打开时默认展示作品列表（不自动打开内容页），并记住离开前所在的分类页面。

### Added

- English: Added opt-in local speaker-profile CLI and HTTP workflows backed by an externally configured WeSpeaker/Sherpa-ONNX runtime. Profiles remain private to the local data directory and can optionally select an existing TTS voice.
- 简体中文：新增可选的本地声纹档案 CLI 与 HTTP 流程，使用外部配置的 WeSpeaker/Sherpa-ONNX 运行时；档案只保存在本地数据目录，可选绑定已有 TTS 音色。
- English: Added the macOS App Speaker profiles page for explicit temporary-audio registration, identification, profile management, and bound-profile generation, plus a separate local validation bundle for the original App host.
- 简体中文：新增 macOS App 的声纹档案页面，支持临时音频的显式登记、识别、档案管理和绑定档案生成；同时提供独立的原 App 本地验证包。

### Fixed

- English: Failed speaker-profile deletion now shows its error inside the confirmation dialog, keeping the selected profile available for an explicit retry. Successfully deleted profiles disappear even if the following refresh fails.
- 简体中文：声纹档案删除失败时，在确认框内显示错误并保留当前档案，用户可明确重试；删除成功后，即使随后的刷新失败，档案也会及时移除。
- English: Speaker-profile pages no longer rely on a transparent page-entry animation, and the macOS validation host now records a bounded display-preflight failure before it runs any smoke actions.
- 简体中文：声纹档案页面不再依赖透明入场动画；macOS 验证宿主会在执行任何冒烟操作前记录有界的显示前置检查失败。
- English: The macOS speaker-validation bundle now includes a versioned sidecar receipt binding its source candidate to the final App tree, packaged host, service, and web resources; smoke rejects a missing, stale, or modified bundle before it starts the host.
- 简体中文：macOS 声纹验证包现在配套带版本的同级构建凭据，将源码候选与最终 App 树、打包的宿主、服务和 Web 资源绑定；凭据缺失、过期或包被修改时，冒烟会在启动宿主前拒绝运行。
- English: The macOS speaker-validation smoke now passes normalized local runtime paths to the App host and records only a redacted configuration field/reason category when local recognition setup is rejected.
- 简体中文：macOS 声纹验证冒烟现在会把规范化后的本地运行路径传给 App 宿主；本地识别配置被拒绝时仅记录脱敏后的字段与原因分类。
- English: Speaker-profile CLI generation now imports an independent IndexTTS emotion reference, reports native generation failures as `runtime_failed`, and distinguishes caller cancellation from a generation timeout.
- 简体中文：声纹档案 CLI 生成现在会导入独立的 IndexTTS 情绪参考音频；原生生成失败返回 `runtime_failed`，并能区分调用方取消和生成超时。
- 修复删除最后一个作品时的保存与残留问题，切换历史版本或新建作品失败时显示更具体的错误信息。
- 新作品优先使用已安装模型，创作页模型选择器减少未安装模型干扰。
- 远程服务的每次生成请求不再新建作品，避免长期运行后状态文件持续增大；生成失败时返回具体错误码。
- 生成历史不再重复保存作品时间轴，减小状态文件体积。
- macOS 退出时作品保存失败会提示并取消退出，不再静默丢失最后的编辑；Windows 本地服务意外停止后可以正常退出，并可备份当前作品；退出流程出错时可选择“仍要退出”，不再只能用任务管理器结束。
- 失败提示保留具体原因（如文件不存在、下载连接超时），不再只显示“出了点问题”；长提示在窄窗口中正常换行。
- 修复部分界面尺寸失效：音量滑块过宽挤掉“保存为角色”按钮、窄屏导航与空状态留白异常。
- 重新安装或切换推理后端后清理旧的运行时，过时的更新安装包不再保留，启动时回收中断操作遗留的临时文件。

## [0.2.0] - 2026-10-11

### English

- New Music projects: set the style with tags, write lyrics with section markers or go instrumental, and generate full songs with ACE-Step 1.5 Turbo / XL Turbo or YuE2 3B (vocal songs whose length follows the lyrics; CC BY-NC 4.0, non-commercial). Each take is kept as a version you can preview, compare, reuse and export. Models made of several files can now be downloaded.
- New Score projects: write notes per track by bar and beat, import MIDI, or let an agent compose, then render every track with a General MIDI sound font and mix by role. The track area spans the bottom of the window with sections on the timeline, zoom, scrolling and a playhead; export MIDI or score JSON. Choose MuseScore General, GeneralUser GS or SGM-V2.01. The CLI adds `yovoice score render|midi|from-midi` with stems, MCP adds `render_score`, and the Agent Skill adds a score guide.
- Optional AI providers (DeepSeek, Qwen, Zhipu, Kimi, Doubao, MiniMax, OpenAI, Anthropic, OpenRouter, coding plans, local and custom endpoints) write a whole score or song style, lyrics and title from one sentence, run in the background, and can switch models on the spot.
- A refreshed interface: ticket-style project lists with record and cassette covers, grouped settings, a separate sound font page, side panel toggles in the title bar (the right panel toggle only appears in a workspace), and hover hints only for truncated text or icon buttons.
- The audio.cpp engine is upgraded to v0.9.1 and updates separately from the app; new Windows CUDA 12.4 and 13.3 installers.
- Fixes for long AI requests timing out, saving on exit, Windows time-zone saves, timeline menus, deleting the last project, and clearer error messages and diagnostic logs.

### 简体中文

- 新增“音乐生成”：用标签定风格，写带段落标记的歌词或改为纯音乐，由 ACE-Step 1.5 Turbo / XL Turbo 或 YuE2 3B（带人声、长度随歌词，CC BY-NC 4.0 仅限非商业）生成整首歌曲；每次生成保留为版本，可试听、对比、复用和导出。支持下载由多个文件组成的模型。
- 新增“编曲”：按小节和拍写各声部音符，或导入 MIDI、让 Agent 写谱，用 General MIDI 音色库逐声部渲染并按角色混音。轨道区横跨窗口底部，段落标在时间轴上，可缩放、滚动并显示播放指针；可导出 MIDI 或乐谱 JSON。音色库可选 MuseScore General、GeneralUser GS 或 SGM-V2.01。CLI 新增 `yovoice score render|midi|from-midi`（含分轨），MCP 新增 `render_score`，Agent Skill 新增编曲参考。
- 可选接入 AI 服务（DeepSeek、通义千问、智谱、Kimi、豆包、MiniMax、OpenAI、Anthropic、OpenRouter、编程套餐、本地与自定义接口），一句话写出整份编曲或歌曲的风格、歌词和标题，在后台进行，可随时切换模型。
- 界面焕新：票根式作品列表配唱片与磁带封面，设置页分组，音色库单独成页，左右面板开关集成到标题栏（右侧面板开关只在工作区显示），悬浮提示只在文字被截断或图标按钮需要说明时出现。
- 推理内核升级到 audio.cpp v0.9.1，与应用分开更新；新增 Windows CUDA 12.4 与 13.3 安装包。
- 修复长 AI 请求超时、退出保存、Windows 时区作品保存、时间线菜单、删除最后一个作品等问题，错误提示和诊断日志更具体。

## [0.1.7] - 2026-10-05

### English

- Run a remote speech service with HTTP API and MCP support. Submit, query, cancel, and download generation tasks with authentication, retry deduplication, and configurable timeouts.
- Use the Agent Skill with either a local CLI or a remote service. User guides and CLI packages now include separate English and Chinese documentation.
- Fix Windows save-error reporting and allow a recovery backup when saving on exit fails. Improve diagnostic logs for saving and generation failures.

### 简体中文

- 新增 HTTP API 与 MCP 远程语音服务，支持任务提交、查询、取消和音频下载，提供认证、重试去重与可配置超时。
- Agent Skill 支持本地 CLI 和远程服务；用户指南与 CLI 安装包提供独立中英文文档。
- 修复 Windows 保存错误提示，退出保存失败时可备份作品；完善保存和生成失败的诊断日志。

## [0.1.6] - 2026-10-01

### Added

- 角色支持保存多种演绎方式，每种演绎可独立选择模型和参数，故事台词和语音项目可分别选用。

### Fixed

- 统一演绎选择框与新增、更多按钮的尺寸和对齐。
- 修复音轨面板调高区域遮挡播放按钮的问题，拖拽仅在顶部横线附近触发，横线贴近面板上边线外侧，并缩小播放控制图标。
- 移除应用图标底板边缘的光带，清理透明轮廓杂点，并校准 macOS 图标的主体边界和小尺寸留白。

## [0.1.5] - 2026-09-29

### Added

- 新增故事配音与语音生成两类作品，支持字幕导入、分段生成和可编辑音轨。
- 新增音轨裁剪、撤销与重做、混音及 WAV 导出，支持作品归档导入导出与状态恢复。
- 新增 Kokoro 官方 BF16 模型下载。
- 新增版本变更日志，发布时自动将对应版本的说明展示到 GitHub Release 页面。

### Changed

- 移除待处理台词批量生成，播放时正文始终自动跟随。

### Fixed

- 优化音轨缩放、滚动、片段拖动与磁吸裁剪交互，统一单人和多人播放指针样式。
- 修复旧版 WebKit 下拉浮层样式，通知展示不再挤动工作台布局。
- 改进 Windows 窗口尺寸适配与位置保存，以及 macOS 应用启动和图标边距。

## [0.1.4] - 2026-09-26

### Added

- 新增角色库，保存并复用音色与声音设置；声音库与创作流程共用编辑能力。
- 新增 Kokoro 官方 Q8 模型下载及 49 个内置音色，支持实验性多语言和中文版本导入。

### Changed

- 优化声音库、生成历史、参考音频片段与原文管理，统一搜索、头像、音频试听和弹窗交互。

### Fixed

- 修复 macOS 麦克风权限回调崩溃，兼容旧版 macOS 的下拉选择器。
- 修复 VoxCPM2 长参考音频的编码器容量问题。
- 修复服务退出时未等待清理完成的问题，并保留 Windows 界面兼容修复。

## [0.1.3] - 2026-09-19

### Changed

- 优化 Windows 窗口边框、滚动、原生菜单与应用对话框。
- 新增代理开关和设置自动保存，调整下拉菜单定位。

### Fixed

- 修复 Windows 下载归档目录路径处理，完善 .NET 8 构建兼容性。

## [0.1.2] - 2026-09-18

### Added

- 新增 OmniVoice 与 Qwen3-TTS 模型及对应的声音设计、克隆和生成参数。
- 新增英文界面，支持语言偏好及 macOS 菜单、提示本地化。

### Changed

- 整理偏好设置、模型目录操作与生成控制面板。

### Fixed

- 修复 macOS 自动更新时 `lipo` 架构校验参数顺序错误。

## [0.1.1] - 2026-09-16

### Added

- 新增 VoxCPM2 Q8 / BF16，支持声音设计、音色克隆和参考原文辅助克隆，App 与 CLI 均可使用。
- 新增声音描述、引导强度、推理步数和随机种子设置。

### Changed

- 优化模型管理布局与按钮，下载期间可移除其他模型的登记。
- 简化 `make app-run`，分离日常启动与安装包制作；采用完整生成后播放。

### Fixed

- 修复 macOS 麦克风权限归属，改用原生权限请求。

## [0.1.0] - 2026-09-16

### Added

- 首个公开版本，提供 macOS Apple Silicon 与 Windows 桌面安装包。
- 支持 IndexTTS 2.0 / 2.5 本地生成、模型下载及 GGUF 导入。
- 支持参考音色、参考演绎、情绪调节、文字引导和参考音频录制。
- 提供声音库、作品历史、版本管理与音频试听。
- 提供 macOS、Windows、Linux 独立 CLI 与 Agent Skill，内置精简 FFmpeg 处理常见音频格式。
