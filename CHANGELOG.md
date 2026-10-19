# Changelog

各版本的重要更新记录。日常改动记入 `Unreleased`；发布前合并重复项，整理为简短的功能、改进和修复说明。

## [Unreleased]

- 新增功能引导：首次进入主界面时依次介绍新建作品、语音生成、故事配音、音乐生成、声音库、设置和 Ocho，可随时跳过，并可在“设置 › 常规”中重新查看。
- 「编曲」更名为「编曲生成」，并入侧栏的「音乐生成」：页面顶部用标签在音乐生成与编曲生成作品之间切换。
- 设置新增“资源”页：集中查看所有生成音频、故事配音片段、参考音频、时间轴素材和 AI 图片，显示大小、所属作品与使用情况；可按类型、作品、使用情况和关键词筛选，按大小、时长或时间排序，试听或预览，并批量删除未被使用的项目，被引用的项目会标出使用者且不能误删。资源页和日志页都是一行工具栏，只有表格区域滚动。
- 界面新增繁體中文、日本語、한국어、Español、Français、Deutsch、Português (Brasil)、Русский、Italiano 和 Tiếng Việt，可在设置中随时切换，首次启动按系统语言自动选择；缺少的文案回退为英文。原生菜单与对话框在中文以外的语言中显示英文。
- `yovoice serve` 与桌面本机接口在 `/docs/` 提供交互式 API 文档（Swagger UI），可填入令牌后直接试调用；新增架构说明文档。
- 故事配音新增“导入小说”：支持 TXT、Markdown 和 EPUB，按章节生成故事作品，自动拆分旁白与对白、识别说话人并从声音库选角；配置 AI 服务后可让 AI 补全说话人和角色特征。
- YuE2 歌曲可以先写旋律谱：按歌词写出人声旋律与和弦，在轨道视图或 ABC 谱面里查看和修改，调速度、只保留旋律、导入 `.abc`，或让创作助手改谱，再按谱生成整首歌；每个版本保存所用的谱，复用时一并恢复。
- 新增翻唱：用 SheetSage2 从一首歌提取旋律（去掉原曲和弦），由 YuE2 按新的风格和歌词重新演唱。
- AI 为 YuE2 写歌词时，风格描述按 YuE2 的写法给出演唱语言和速度。
- AI 服务新增“ChatGPT (Codex)”：通过本机官方 Codex CLI 使用 ChatGPT 订阅额度，设置页可检测安装与登录状态、一键用 ChatGPT 登录并读取模型列表；yovoice 不读取或保存登录凭证，请求在临时目录的只读沙箱中运行。
- 设置新增“日志”页：按级别、模块、时间和关键词查看服务日志，点开查看完整字段，并可导出诊断包（不含推理进程原始输出）用于反馈问题；导入导出、音色与素材、模型与运行时、AI 服务、推理进程启停、本机 API 请求和界面导出等关键操作都会留痕，日志不记录正文、歌词（包括 AI 输出）和密钥，用户主目录显示为 `~`。
- 故事配音的“导出音轨”可选择导出 SRT 或 WebVTT 字幕：每个配音片段一条字幕，时间按片段在时间轴上的实际位置（含裁剪），支持全部、所选范围、单条音轨或某位说话人，可附带说话人名；音乐播放页新增“导出歌词（LRC）”，按校准、自动定位或估算的歌词时间输出。
- 新增 AI 助手 Ocho（右下角入口、标题栏或 ⌘J / Ctrl+J）：用对话新建和修改作品、写词写谱、改 YuE2 旋律谱、挑音色、调参数、发起语音/音乐/配乐生成，操作即时反映到界面。回复逐字流式输出，思考和每一步操作收在可展开的过程里，结束后自动收起；Ocho 是一只会动的小幽灵，运行时按思考、查找、修改作品、生成音频等阶段显示不同的点阵动画和带流光的状态文字。浮窗可拖动、缩放和拉高，位置会记住；右下角入口可移走，移走后改从标题栏打开。输入框左下角可直接切换模型，当前作品自动随消息带上。对话按会话组织，历史从左侧抽屉切换。AI 写作统一走助手，编辑器里不再有单独的 AI 写作栏。助手需要接口类 AI 服务，暂不支持 ChatGPT (Codex) 订阅。Ocho 能看到你在编辑区选中的文字、故事配音里选中的台词、播放位置、作品所用模型是否已安装以及最近一次生成的结果和错误，“这句”“刚才那段”不用再描述；选中的内容会以引用标签显示在输入框里，点 × 即可不带。Ocho 熟悉各个模型的特点、页面与设置的位置和每个错误码的处理办法，能查看已装模型、推理引擎、任务和最近的错误日志来排查问题。删除作品、生成记录或声音，下载模型或推理引擎，清理存储，切换计算设备或下载源时，Ocho 会给出确认卡片，点确认才执行。在对话里说出的偏好（常用声音、风格、语速等）会被记住，可在新增的“设置 › Ocho”中查看和删除。生成完成或失败、模型下载结束时，Ocho 会在右下角给出建议，一键交给它处理；可在“设置 › Ocho”中关闭。
- 首次启动播放约 8 秒的开场动画：粒子汇成随声音起伏的 3D 声音球，再聚成声波图标，随后出现标语与四种创作方式，点“开始探索”进入主界面；可在“设置 › 常规”中重新播放。之后启动只显示简短的声波加载动画。开启“减少动态效果”时不播放动画。macOS 与 Windows 窗口在界面出现前使用相同底色，不再闪白。
- 移除“请求超时”设置，AI 请求的超时改由内部统一处理；AI 服务与编曲文档同步更新切换模型、编程套餐和音色库入口的说明。
- 语音生成的播放器工具栏始终显示“历史生成”入口，新作品还没生成过时也能进入历史页。
- 移除创作区顶部的“开始生成前还需准备”清单：缺参考音频时点生成直接打开声音选择，缺模型或推理引擎时打开设置里对应的页面。
- 界面细节打磨：按钮、导航和分段控件有按压回弹，悬停过渡更顺滑；检查器、侧栏、设置等可滚动区域的上下边缘平滑淡出，不再生硬截断；助手输入框有层次并在聚焦时亮起；新回复由虚到实浮起；时间、大小等数字等宽不抖动。
- Ocho 能改右侧检查器里的全部声音设置和各模型的高级参数（声音设计描述、情绪、语速、温度、种子等），并能新建和修改声音库里的角色、给作品或故事角色套用角色；只想建角色时不再新建作品、不再跳转页面。历史里的生成结果新增“保存为角色”，生成时用的模型和全部参数一起存成角色。
- YuE2 支持纯音乐：先写谱，再把人声旋律交给乐器演奏，和弦、速度和调不变，歌词可以不填；旋律谱菜单可直接把人声旋律转给乐器。YuE2 新增“按旋律谱 / 自由发挥”两种生成方式，高级设置里可调推理步数以及写谱和演唱的采样参数。
- Ocho 能直接为 YuE2 作品出谱、从本地歌曲扒谱、去和弦、改速度、把人声旋律转给乐器；改和声、换风格保旋律、换词和翻唱有了成套做法，改谱后会按小节说明改了哪里。
- Ocho 现在可以读写文件、搜索文件、运行命令和联网查资料（工作目录为它自己的工作区）；对作品、角色和设置的改动仍只通过应用内的工具进行。
- 音乐播放页可以在同一作品的各个版本之间切换对比试听：从顶部选择版本，切换时保持播放位置；“切回版本 N”或按 B 键在最近两个版本之间来回切。
- 设置中音乐相关的模型（ACE-Step、YuE2、SheetSage2）单独放在“音乐模型”页，原“模型”页改名为“语音模型”；修复侧栏最近作品的删除按钮按下时位置下移。
- 新建作品默认使用已下载的模型，并沿用上一次同类作品选的模型；Ocho 新建作品与新建声音设计角色同样优先用已下载的模型。
- 同一模型的多种量化（Q4、Q8、BF16 等）合并为一项：设置里每个模型一行，用精度切换下载或移除，并标出已下载的精度；创作区的模型选择器每个模型只列一项，下载了多个精度时在下方切换。
- Ocho 写歌词时按曲风和篇幅选择不同的曲式（主副歌、AABA、分节歌、说唱、贯穿式等），不再每首都是同一种段落结构；纯音乐可以用 [Guitar Solo] 这类段落标记安排器乐段落。
- YuE2 按旋律谱生成时，作品还没有谱的话，生成后自动把 YuE2 这次写的谱填入旋律谱，可以接着改谱再生成；歌词的“插入段落”补充了 Post-Chorus、Hook、Final Chorus 和间奏、独奏等器乐段落。
- 旋律谱新增五线谱视图（默认显示，人声与伴奏成组、和弦标在上方）和“试听”：用已下载的编曲音色库把谱渲染出来先听一遍，不占用推理；谱中途换拍子（如 4/4 里夹一小节 9/8）也能试听。旋律谱的操作报错改用全局提示，不再插在谱面上方。Ocho 为按旋律谱生成的 YuE2 歌曲先写谱、填入旋律谱，请你看过听过确认后再生成整首歌；说“直接生成”时才跳过。

## [0.2.1] - 2026-10-16

### English

- Add direct Kokoro-82M 1.1-zh downloads from Hugging Face and ModelScope, and simplify model management to downloads from the app or CLI.
- Add first-run preparation guidance, faster speech generation, batch reference-audio deletion, readable generated-audio filenames, and safer shared-file cleanup.
- Add a full music playback page with synchronized lyrics, vocal alignment and calibration, plus improved score limits, AI naming, and style editing.
- Move projects, voices, models and settings to transactional SQLite storage with backups, migration from `state.json`, storage relocation, recovery, and lifecycle cleanup.
- Improve desktop reliability across Windows and macOS, including service recovery, process cleanup, tray behavior, narrow-window layouts, localized Windows UI, signing checks, and automatic updates.
- Add Breeze TTS 2, text and subtitle file import, local MCP and API integration improvements, OrcaRouter, and clearer audio, migration, disk, and save-error handling.

### 简体中文

- 新增从 Hugging Face 和 ModelScope 直接下载 Kokoro-82M 1.1-zh，模型统一通过应用或 CLI 下载，并自动识别模型目录中的完整模型。
- 新增首次生成准备清单、快速语音合成、参考音频批量删除、可读的生成音频文件名和更安全的共享文件清理。
- 新增带歌词同步、人声定位与校准的音乐播放页，改进编曲时长限制、AI 自动命名和风格编辑。
- 作品、音色、模型和设置迁移到带事务与备份的 SQLite，支持从 `state.json` 迁移、存储位置迁移、故障恢复和生命周期清理。
- 提升 Windows 与 macOS 桌面可靠性，涵盖本地服务恢复、进程清理、托盘行为、窄窗口布局、Windows 界面本地化、签名校验和自动更新。
- 新增 Breeze TTS 2、文本与字幕文件导入，改进本机 MCP 与 API、OrcaRouter，以及音频、迁移、磁盘和保存失败提示。

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
