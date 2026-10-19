# FEAT-002 工程上下文 — Mac App 声纹交互验证页面

本文件的 Plan 为主 Agent 接受的唯一有效工程方案。生命周期进度只由 state.json 保存。产品依据为 `.sdlc-v1/context/REQ-002.product.md`；已接受依赖为 REQ-001 / FEAT-001。当前分支 `codex/speaker-validation-ui`；初次 Plan 采集 HEAD 为 `83c1366b53d8ce0c5ff80fe0a80714cfde96c090`，2026-10-14 恢复基线为 `822c0a4b7b0465e5f70a0331bb396e2fedadd901` 加既有未提交实现。产品及 state 的未提交变化由主 Agent 管理。

## Plan

### 2026-10-18 页面可见性与原生验收显示前置检查

本节 supersedes 既有空白页返修中尚未确定的显示实现与验证策略；其他产品规则、已接受的三项正式评审修正和接口保持。沿用同一 FEAT-002/TASK-001、Terra 实施者、全部失败历史与已经使用的主诊断 allowance，不清零尝试次数。2026-10-18 用户另行授权的一次零模型定位已结束，当时后续 App 启动与模型运行尚未授权；之后各次明确授权与实际结果见 Validation，不构成自动重跑权限。

新因果证据见 `artifacts/verification/feature-002-goal-acceptance-20261017/opacity-diagnostic-attempt-001/primary-assessment.json` 及同目录 raw result/screenshot：两个嵌套 page-panel 的 page-in 为 pending/currentTime=0、opacity=0；同一阻挡元素持续透明 30 秒后，仅取消内层动画就使该元素 opacity 从 0 变为 1，外层仍为 0，截图仍空白。document.visibilityState=hidden、hasFocus=false，两次 RAF 等待超时。仅启动一次、35 秒、零声纹写操作，源码/App/资产/普通 State 稳定，自有进程均退出。该证据确认页面可见性对入场动画的依赖有实际影响，未证明 WebView 为何 hidden，也不能代表 Feature 验收通过。

- 页面内容在动画尚未开始、暂停或绘制帧回调未到达时必须仍可显示。最小修改 `.page-panel` 的透明入场动画依赖，覆盖外层与内层；不对所有元素强制 opacity，不改其他弹层/播放器/录音的动画、导航挂载和数据生命周期。不引入依赖或新的 UI 框架。
- 原生 DEBUG smoke 在执行任何旅程脚本前，有限检查宿主窗口与 WebView 已连接、尺寸有效、窗口可见且未最小化、document.visibilityState=visible。等待使用至多 10 秒的有限期限；无权限/不可见/脚本错误须在业务动作和模型操作前明确失败并保存脱敏的原生及页面显示事实。hasFocus 和 active/occlusion 可作为诊断事实，不以“无焦点”等同不可见。可在 DEBUG smoke 开始时沿用已有公开激活 API 做一次明确的显示请求，不增加自动重启、周期性抢焦点或生产激活行为变化；不要猜测隐藏原因已经修复。
- 保留主流程和重启的两次 RAF、当前有限超时、祖先可见性、命中可达、业务断言与截图要求；不把绘制超时改为成功，不通过 reduced-motion、禁用所有动画、隐藏窗口或合成截图绕过原生验收。后续必须由主查看新包的真实截图和状态/档案结果。
- 在现有测试资产中增加可观察回归：把旧页面入场动画停在首帧时，旧实现的有效祖先透明度断言实际失败；修复后暂停动画的页面仍可见，默认与窄窗口的声纹关键控件可访问。原生显示前置检查覆盖不可见/超时必须早于脚本执行、只请求一次显示、不执行业务操作，并保存失败事实；优先复用既有脚本替身与 Swift 测试边界，不另建通用框架。
- 实施写边界为 `web/src/app/styles.css`、相关既有 browser/e2e 测试、`desktop/macos/Sources/VoiceWorkbenchMac/main.swift` 的 DEBUG smoke 区域及必要最小显示检查 helper/对应 Swift 测试、`scripts/desktop/check-speaker-validation-macos.py` 和确有必要的原生检查脚本、`CHANGELOG.md`、现有唯一 `docs/usage.md` 及其原生验收详细操作链接。不改 Go/公开 RPC、识别算法、持久化、阈值/超时、Windows 或产品范围。主独占本 Plan/state，worker 不提交推送。
- 同一 worker 完成有界实现、确定性红绿证据、完整 Web lint/typecheck/build、全部 browser 与 tester-army e2e（零模型、零重试、strict/read-only cache）、受影响的脚本与 Swift 检查，并使用原有构建入口生成全新独立 App/sidecar。归档实际退出码和源码/完整包/最终签名/打包 dist 的绑定；旧候选与失败产物均保留。本阶段禁止启动真实 App、服务、模型、TTS/Mic/provider 或下载。
- 本次修改不改变用户故事或参数来源；usage 沿用现有声纹故事，仅补齐原生验收的桌面可显示条件和显示前置检查失败后的恢复说明，不虚构新故事。当前实现仍 incomplete；新包可审查后再请求明确的一次原生文件旅程预算。通过真实旅程及必要回归后才可 Task done、干净提交、独立正式 Validate/Review；既有功能分支及验收记录 commit/push 授权继续适用。

### 2026-10-17 正式评审返修：刷新、模型切换与路径字面量

独立正式 Review 对 validation commit `767b65c…` 提出三项有代码因果链的 P2，主接受：迟到 list 快照可覆盖删除后的刷新；声纹生成模型选择只改 modelId，普通文本的 Index 2.5 日语切 Index 2 会保留 schema 不接受的 ja；生成的 FFmpeg shell 包装器把合法路径中的 shell 字符再次解释。沿用同一 Task/实施者/全部失败历史及已用诊断 allowance，不以新阶段清零。

- 刷新拥有递增请求身份，仅最新刷新可以提交 runtime/profiles/列表终态或错误，仍保留读取失败时已有列表；组件失效后不写状态。用既有 listDelayMs 先保存含目标的旧快照、删除并完成新刷新、再让旧响应到达，实际证明目标不重新显示；旧请求错误也不能覆盖新成功。
- 复用原 Inspector 的模型切换规则（当前 inspector.tsx:91）：同家族保留适用字段、跨家族使用已有默认语义；不支持的旧 Index 语言按原页面规则回到 zh。可从现有 inline handler 提取最小具名共享函数，原创建页与声纹页共同调用，避免第二份长期重复规则；不重写原页面。保留正文、合法情绪/选项、独立草稿和绑定语义，覆盖 Index 2.5 ja→Index 2，并让实际 wire 与 Go schema 的验证对应。
- 包装器把已经规范化的 FFmpeg 路径作为 POSIX shell 字面量输出，例如标准库 shlex.quote；参数原样传递。必要最小 writer 可供构建和检查共同调用。零模型替身真正执行生成的包装器，覆盖空格、$、单双引号、反引号/命令替换文字，确认执行正确文件、参数保持、无额外命令执行；不只检查实现字符串。
- 同一 worker 先保存三个缺陷的确定性红证据，再最小修正；写边界为 SpeakerPage、原 Inspector/已有模型选项模块或最小共享 helper、相关现有 e2e/fixtures、必要 desktop schema 测试、build/check 与最小 FFmpeg writer、必要 CHANGELOG/usage。不改识别算法/公开 API/持久化/Swift/产品规则，不写 Plan/state或Git。完整 Web lint/typecheck/build、e2e/browser与脚本检查必须重验；增加 Go schema 测试时补 vet/完整 race，未变相关输入才可复用其余证据。
- 完成后构建全新独立 App/sidecar，保存真实退出码与源码/完整包/最终签名/15项dist绑定；旧产物全部保留。此返修阶段不启动 App、模型、provider/TTS/Mic/GPU或下载。主核对新候选后再安排必要原生验证，随后干净提交及独立 Validate/Review；没有通过前不推进验收。用户已有功能分支提交/推送与必要修正授权继续适用。

### 2026-10-17 正式验证返修：取消测试必须有界观察终态

正式 Validate 在已提交 `9c851702a7310de2927db656fe85ba882270e524` 上完整 Go race 超时。解除沙箱后原始栈只剩 `TestEnrollSpeakerCancelsBeforeCommit` 在 `speakers_test.go:240` 的 `<-accepted` 等待，登记协程已退出，结果被缓冲在未读取的 result 中。已观察到的缺陷是测试的无界等待掩盖了提交钩子之前的操作终态；操作早退的实际错误尚不明确，不据此修改生产逻辑或超时。

同一 TASK/实施者继续，所有失败历史与已用诊断 allowance 保留。先在该测试中同时观察提交钩子、提前操作结果与有限期限；失败路径也必须释放自己的提交钩子，取消后读取结果同样有界。保留原“提交接受点前取消、不落盘”的断言，不跳过测试、不扩全局 90 秒期限。优先复用现有局部测试同步方式，不增加通用测试框架。

写边界仅 `internal/workbench/speakers_test.go` 的该测试及必要局部同步。实际提前结果若明确为夹具 2 秒预算触发 DeadlineExceeded，可只为该非超时语义测试设置有限且足够的夹具预算（最多 10 秒），保留原错误与因果依据；不能修改生产默认/校验或其他真正超时用例。其他错误或两次无新定位/验收进展立即返回主，不盲猜扩大修改。完成定向 race、必要格式/vet与完整 race，记录实际结果。

这次不启动 App、真实模型或新诊断实验，不重建已验过的生产包。若修改确实只有测试和 `.sdlc-v1/**`，当前原生、Web、Swift 等相关实现/配置/资产未变，主可按逐文件及语义核对复用独立成功证据，Go 必须验证修改后的提交。用户对本轮功能分支及验收记录 commit/push 的授权继续适用。返修后由主决定 Task done、干净提交和再次独立 Validate；未通过前不进入 Review。

### 2026-10-17 Goal 验收：候选档案 ID 契约修正

用户要求建立 Goal 并完成验收，不明确的预期及时提出。沿用同一 FEAT-002/TASK-001、实施者、失败历史与已用诊断 allowance。输出目录冲突已通过父目录记录、尚不存在的 `smoke/` 子目录由脚本独占创建解决；源码 `41d8d2f5…a4535` 的新 App 已完成 27 秒真实文件旅程，原始证据见下方 Validation，不重复该次运行。

主核对真实截图发现候选行只有标签、分隔符和分数，缺少 RULE-UI-05 要求的档案 ID。源码已定位：Go `recognition.Candidate.ProfileID` 序列化为 `profileId`，桌面直接返回该结果，而 TypeScript `SpeakerCandidate`、页面及合成 fixture 错用了 `id`。这是实际截图与跨语言字段定义共同确认的契约缺陷，不是新的产品取舍，也不需要额外诊断实验。

- 保持 Go/CLI/HTTP/desktop 的现有 `profileId` wire 不变，修正前端候选类型、显示和 React key，修正相关合成 fixture，不引入双字段兼容或重算识别结果。
- 同范围 DTO 只读审查定位另一项实际缺陷：合法阈值 `T=0` 被 `SpeakerRuntime.threshold,omitempty` 省略，合法亚秒 timeout 经整数除法丢失精度再被省略。运行状态已配置时须保留实际数值（包括零阈值与小数秒），未配置时继续不携带未成立的配置元数据；字段名及数值 JSON 类型保持，不更改配置合法范围或推理超时。TypeScript Identification 补齐真实返回的 `modelSha256`，合成 fixture 遵守真实字段。
- 在既有合成测试中输入真实服务结构的 `profileId` 候选，断言各候选的确切 ID 可见，matched/unknown/ambiguous 的确定身份语义保持；增加原生旅程候选 ID 的可观察断言，避免只有主结果 ID 通过而遗漏候选字段。
- 同一 worker 完成有界修正、必要的定向及完整 Web 检查、脚本检查和新包构建。Runtime DTO 修正补实际 JSON 序列化边界回归、Go vet/完整 race；Swift 未变的相关证据按同输入复用，不扩大功能、重构或 22 场景范围。代码保持正常多行格式。
- 先回传新源码指纹、完整检查、新 App/sidecar 和包资源绑定；此纠正阶段不再启动 App、模型或其他推理。主接受后再依据已说明预算和实际授权安排必要的最终原生检查；不以旧包原生通过代表修改后的 UI 已验证。
- worker 仅修改 `web/src/shared/speakers.ts`、声纹结果呈现、现有合成资产、Workbench 的 SpeakerRuntime DTO/返回值与对应测试、声纹原生检查/对应脚本测试及必要 CHANGELOG/usage；不写 state/Plan，不提交推送。正式 Validate/Review 仍需干净实现 commit 和独立阶段；涉及 Git 时主先展示具体修改并按 soul.md 请求确认。

### 2026-10-17 原生产物绑定纠正：停止运行，仅修复构建校验

本轮唯一原生旅程在 `artifacts/verification/feature-002-restart-ui-20261017/native-cf2-run1/` 失败并停止：initialize通过，main的登记控件可见性等待30秒失败，配置已通过，模型提取0次、restart未运行、自有进程已退出。主只读比较发现其AppBundle和WebResources与run6逐项完全相同；preflight实际路径仍为 `worker-20261017-config-fix/yovoice.app`，buildLog也指向旧config-fix。当前源码cf2和旧App被同时记录不等于构建绑定；该次不能为cf2新页面背书，也不能据此确定历史空白唯一原因。没有新构建或新候选原生通过。

这是已观察到的产物选择/校验缺陷，沿用原owner、失败历史、既有诊断allowance。主不增加界面诊断实验，也不自动第二次启动App/模型。用户已授权且尚未完成的新本地App构建继续执行，先把下一动作做成可审查产物，再请求必要的运行授权。

- 构建脚本必须生成可校验的本地build receipt，包含本次source manifest/hash（构建前后源码稳定）、真实打包host/service/Web资源指纹以及固定格式版本；receipt不包含绝对私有路径、凭据或原音频。复用现有capture脚本的source/tree/hash能力，可增加最小共享校验命令，不引入新的运行服务或公开接口。
- 构建中已观察到签名顺序问题：包内receipt在host/service签名后生成，随后整个App签名改变host字节，最后验证报host fingerprint mismatch；该失败日志保留，另一次sandbox cache拒绝不归为代码根因。依据Apple TN2206/TN3126（程序签名会修改主可执行文件，资源由签名封装），当前最小方案确定为包外同级sidecar `<app>.receipt.json`：先完成service→host→bundle全部签名与strict verify，再生成receipt绑定最终包。receipt增加完整App tree指纹及host/service/Web分项，sidecar不在App资源签名内，消除自引用。所有默认路径由同一helper从App路径推导，smoke也只认这一sidecar。
- 新包与sidecar作为一对交付，复制/更名后保留对应命名关系；旧包无sidecar直接拒绝。构建发布两个文件失败时保留/恢复旧产物或明确留下不可运行的校验失败状态，不能以staging清理丢失既有验证包；本次使用全新独立目录，历史包/receipt不覆盖。替身签名应模拟最终bundle签名再次修改host，证明receipt在最后签名后取hash；不通过重复重签、剥离签名或跳过校验解决循环。
- smoke在首次host启动前严格验证所选App的receipt存在、格式合法、源码与本次候选相符、打包host/service/Web内容匹配receipt。旧App、无凭据包、篡改资源或不同源码包必须在host被调用前有限失败，不再把“当前源码文件没变”冒充“App由此源码构建”。保留六项规范化配置与现有有限超时/自有进程清理。
- 下一次允许的UI检查不能再次只给DOM存在/不可见二值结果：在原smoke/restart的本地失败诊断中保留有限拒绝原因、首个阻挡祖先的安全class/testid、数值rect/clip及display/visibility/opacity，区分隐藏、零尺寸、祖先裁剪、视口外和命中遮挡。不收集整段DOM/用户正文/路径/原音频；成功谓词及业务动作不放宽。可扩相应脚本和替身检查，当前仅实现诊断，不执行新App实验。
- 同一worker用零模型替身/隔离文件验证这些拒绝与接受路径，旧包拒绝时明确断言host调用次数0。尽量复用既有build/check/capture脚本，写边界只增加这几处及必要用户说明；不修改生产Web/Swift/Go、不再扩22场景。未变的56合成/57browser/214Go通过可按相关输入复用。
- 将当前源码构建到全新独立输出目录，保存本次实际build命令、退出码、源码/receipt/包内容绑定，主亲自核对新包与旧包不相同、打包资源与本次dist相同。本阶段不启动App、服务、识别、TTS或Mic；不重复native-cf2-run1，不复用其候选命名为新通过。
- 全部更正后只返回可审查新App、源码/构建凭据及检查记录，仍为incomplete。待主说明旧包执行失误和剩余UI未知后，下一次运行需要明确新的有限计划与授权。Git提交、正式Validate/Review仍未执行，state继续in_progress；不以补receipt清零失败次数或恢复诊断allowance。

### 2026-10-17 重启就绪、可见界面与验收补齐

用户在 run6 整体未通过后明确授权继续定位重启/空白、修复并完成 Feature 验收，需要授权及时提出且不能进入循环。沿用同一 TASK/原 Terra owner/全部失败历史；先前诊断 allowance 已用或未知的状态不重置。run6 的识别业务成功不等于整体验收，当前输入候选为 `71308c600f9f091de6c533b0e805287d70769cffe02dcc42e4fd909077d08d3d`。本节 supersedes 上节已经耗尽的一次运行安排，保留其失败证据。

- 新的因果定位：现有 restart 在 didFinish 中直接查导航即报 missing；主一次有界零推理检查记录 initialNav=false，而等待导航/list ready 后约617ms完成，且同一 App 的声纹页截图完整。证据在 `artifacts/verification/feature-002-restart-ui-20261017/primary-zero-inference-probe/`。因此导航必须等待 React 与异步列表终态，不能用网页文档加载完成代替可操作就绪。有限等待失败仍必须保留诊断并退出，不重启兜底。
- 主同一有限诊断的第二个零推理对照仅在隔离副本删除档案，919ms终态、截图完整；它排除了“删除必然导致空白”，但没有证明 run6 历史空白的唯一原因。不能把空白预先归因于CSS/崩溃或无条件改动生产布局。两个实验总启动2次、各90秒上限、实际约8秒、0推理，全部退出；此后不增加主诊断循环。
- 同一 worker 修复 restart 的有界等待，补真实命中/祖先裁剪/当前页终态的必要可见性与截图准备检查。原生截图应在本次UI操作提交、dialog关闭、选定声纹区域进入视口、绘制稳定后采集；任何等待都有限，不以固定长sleep或重启掩盖故障。可在 Swift DEBUG smoke 内必要增强截图/失败诊断，不改发行信任、公开RPC或数据格式；无因果证据不重写生产UI。用延迟挂载等确定性替身回归证明旧脚本会失败且修复后通过。实际截图由主检查，DOM非零rect不能单独当作可见通过。
- 场景核对已定位真实生产缺口：recorder定时停止后，SpeakerPage完成回调只显示通用录入状态，没有RULE-UI-01要求的60秒自动停止反馈。允许同一worker最小扩充内部RecordingCompletion的停止原因（由该session的限时timer明确设置，不能仅按异步回调duration推断），SpeakerPage读取并显示中英文notice；手动停止、旧session、离页清理保持，VoicePicker未使用新增字段时语义保持。写边界增加use-temporary-recorder.ts、speaker-page.tsx及对应本地化/测试；不变更公开API或持久化。用fake时间验证自动/手动原因区别和UI反馈，不自动采集真实麦克风。
- 验收必须按REQ-002的22场景映射实际断言/命令/证据层级，不按测试数量推导。在既有合成桥接/Playwright/Go中补有实质缺口的必要行为断言，优先精确T/M与舍入、导航返回原正文、删除取消、空库无候选/分数、错误/无TTS恢复、键盘/长ID/窄窗可达；底层已证不变的事实直接引用，不镜像实现或盲重跑。若出现真实业务缺陷先回报原因/最小修复，不扩大功能。
- 新SCN12回归在中英文两目标均证实刷新失败时旧档案行从DOM消失。主源码定位为SpeakerPage列表渲染的listError三元分支只显示alert、跳过仍保留在state的profiles；不是已证实的陈旧请求覆盖。允许最小拆开错误反馈与已有列表渲染，错误且无成功数据时不显示空库成功，有已有profiles则继续显示且标示读取失败；刷新恢复后加载新数据。无并发因果证据不增加epoch/状态框架。写边界使用已接受speaker-page.tsx及现有测试，中英文两目标重新验证该失败场景。
- 底层证据复核发现既有HTTP取消/shutdown用例使用callOverride，证明context/registry清理但未经过真实上传接收；允许在internal/workbench/speakers_test.go复用既有受控runner/converter，验证From reader输入在取消/关闭终态清除接收文件与recognition临时目录，State/档案不变。以现有HTTP context传播测试加该真实Workbench路径形成分层证据，不声称新增全HTTP实模通过、不增加生产测试hook。此测试变动补vet/完整race；其余未变Go结果按精确测试名条件复用。
- 先完成零推理脚本和受改Web/Swift检查，回传不可变候选与22场景覆盖表。主验收后新建同源本地bundle；已授权的一次固定CPU旅程仍是双登记、双known精确ID、一unknown无ID、删除/重启，单线程T=.6/M=.05/120s，无provider/下载/TTS/GPU/物理Mic。每phase600秒、截图/退出15秒、TERM3秒、全旅程20分钟，失败立即保存并停止，不自动第二轮。开始前记录当前负载与具体运行计划；只有主确认本次候选和运行权限后启动，相关源码运行中保持不变。
- 写边界为 speaker smoke/restart/init/check脚本及必要新增共享测试helper、Swift DEBUG smoke截图、现有speaker合成/浏览器测试资产；CHANGELOG/usage仅必要说明。生产UI只在可复核根因且主接受后修复。worker不写state/Plan、不提交推送、不改产品契约、不删除历史。所需checks随实际改动选择：脚本检查、Web lint/build/定向及必要全量、Swift测试；未变Go沿用同源码race证据。
- 功能检查满足后仍需干净implementation commit及独立Validate/Review；新Git提交/推送授权须由主提供具体可审查改动后请求。物理Mic与真实TTS听测单列NOT_RUN，不能伪装文件输入或fake引擎为实机采集/音质结果。首次/恢复触发非收敛规则时保留原历史与allowance，停止报告，不另起诊断或换人清零。

### 2026-10-17 配置失败定位、修复及一次原生复验

恢复后第二次完整 race 的唯一失败为本次新增的配置日志回归。实施者用定向 race 复现日志只有 state.open，并确认当前 speakers.go 缺少应有的 diag.Log 调用；它在主已归档/审查的 tree `13fca2480a32126242d55027b9af46043e816199` 中本已存在。允许最小恢复这一已接受调用；不改其合同、不归为异步时序、不推定尚未留存的修改来源。保留第二次原日志及可用源码差异，未保存的失败源码指纹不得补造。恢复后定向 race 已通过，下一次完整 race 为基于该新静态证据的收敛检查；若再失败立即返回，不扩成其他调试循环。此记录写完后主不再改变跟踪文件，供实施者固定最终指纹并执行唯一原生旅程。

首次本候选完整 race 在既有 `TestExtractRejectsBadResponseAndTimeout` 的协议错误分支失败：30ms 的共用配置先触发 deadline，返回 `context deadline exceeded`，未抵达期望的坏协议分类；日志 `artifacts/verification/feature-002-config-fix-20261017/go-race-current.jsonl` 保留。生产 Extract 未在本轮修改。测试应把协议/输出负例预算与真正的超时用例分开：前者给足但有限的进程启动时间，最后 sleep1 超时子例仍明确使用30ms并断言 DeadlineExceeded。不得放宽生产超时、删断言或通过无改动整套重跑掩盖失败。新增配置传递测试也不依赖事先存在 ignored artifacts 目录；若原生已经启动则待其终态后才改测试，不在运行中改变源码指纹，且不能因此再启动第二次旅程。

用户明确授权“继续定位具体是哪一项校验失败，修好后再跑一次验证”。这覆盖下述局部修复、必要检查、新构建和一次既定 CPU 声纹旅程；保留同一 TASK、原实施者、此前失败历史和诊断次数，不把这次恢复当作重置交付段。HEAD 仍为 `822c0a4b7b0465e5f70a0331bb396e2fedadd901`，输入候选为 `87ca0196bcae4f3510d8470d0d8b6f8e3a530c5f5d5e2f0f7d8ef02e620d6e99` 加本节。

- 主已完成一次零推理最小实验：真实 smoke wrapper 接收相对的 Python/model/runner 输入，规范化后的入口检查通过，但打印配置后立即退出的替身仍收到三个原始相对环境值。证据 `artifacts/verification/feature-002-config-fix-20261017/before-fix-config-probe.json`。结合现有 LoadConfig 顺序，首个拒绝项是 `YOVOICE_SPEAKER_PYTHON` 的非绝对路径；MODEL/RUNNER 也存在相同问题。run5 的历史启动环境未保存，不能把该复现冒充其唯一根因。
- smoke 启动宿主时明确传递已经规范化的三个路径，保留 venv Python 入口、现有相对路径基准、空格引用和六项显式配置优先级；不以放宽 Go 校验或更改 Swift env 优先级绕过错误。补自包含替身回归，证明入口检查和子进程收到的配置一致，scalar 值和原失败证据保留。
- 复用现有 recognition.Error：配置失败附带私有的白名单字段和有限原因，提供只读提取方式；Error() 与 Code(err) 继续只返回 runtime_not_configured。校验顺序、范围、默认值、文件/Hash检查保持。只在 Workbench 的 speakerConfigOnce.Do 中复用 diag.Log 写一次 speaker.config_rejected(field,reason)，不输出原始配置值、路径、系统错误、特征或凭据。SpeakerRuntime DTO、CLI/v1/desktop 的公开错误码和错误正文不变，不增加新的对外配置诊断协议。
- 必要测试覆盖相对路径进入子进程、各六项的配置拒绝字段/原因、原错误码与默认值不变、本地日志白名单及一次性、公开响应/错误无私有值。先红后绿验证已定位问题；实际检查命令和结果绑定新候选。Go 修改运行离线 vet 与完整 race；脚本检查含既有有界退出/替身场景。Web、Swift 的相关源码与配置未改时复用已记录的 46/55/10 项证据，不为改日志盲重跑；新增 Swift 或 UI 改动需主重新接受范围。
- 同一原 Terra owner 完成修复、必要验证、新验证 bundle、唯一一次原生旅程。已有固定 Python/sherpa/模型/样本/FFmpeg，CPU 单线程、T=.6/M=.05/120s、双登记、双独立 known 精确ID、一 unknown 无选定ID、删除/重启及 State/Voices、资产/源码/App前后指纹；不新增下载、provider、真实 TTS/GPU 或物理麦克风。正常构建时间不当作失败。原生每 phase 总上限600秒，截图/退出15秒、TERM宽限3秒，整体旅程20分钟有限期限；只回收自有进程。
- 原生执行前先回传零模型修复/检查节点、精确失败字段与实际命令；主可并行审阅不可变源码，但不用再请求用户逐阶段批准。主接受/构建完成后继续原授权的一次旅程。该次出现新失败立即保存准确字段、reason、日志和截图并停止，不能另起第二次。主不在旅程期间改跟踪源码，避免指纹失效。没有 Git 提交/推送、正式发行或 TTS 运行授权。
- 写边界为 smoke/check 脚本、recognition.go 及其配置测试、workbench/speakers.go 及必要日志测试；CHANGELOG 与 usage 可做最小用户可见修复说明。原生宿主与公开协议不在默认改动范围。子 Agent 不修改 state/有效Plan，不提交推送；主接受最终结果，native失败仍为 incomplete。

### 2026-10-15 晚间增量审查：有限退出与本次请求证据

本节派发前，唯一真实旅程已经在 `artifacts/speaker-validation-macos/worker-20261015/smoke-run-5-visible-current` 启动并返回失败：initialize 成功，main 7.47 秒报告 `speaker runtime is not configured`；诊断 RPC 成功但 `configured:false`，未进入本轮真实识别。实施者已退出自有 App/service，无残留 PID。本轮不能再执行下文原生旅程；仅完成三个已定位的零模型修正与必要验证，返回 `incomplete`。保留失败原件和其源码/App基线，后续源码指纹不得冒充该次原生已通过。当前没有新的 native/TTS 运行授权；先向用户报告失败证据及有限后续方案，不追加配置诊断试跑。

独立 reviewer 对不可变 tree `200156b237d6697b721657ad0d2fa4cc738e73ce` 的增量审查发现三处具体证据缺口；这是新源码定位证据，沿用同一交付段、实施者和失败历史，不恢复已经使用或未知的诊断 allowance。此前 46/46 的合成报告继续保留，受改场景必须重新验证；唯一真实声纹旅程已经按上一段记录终止，不再重跑。

- 宿主先写 `smoke-result.json`，再等待截图。wrapper 已收到结果后不能离开超时监视，直接无期限 `wait`。先归档并检查失败结果；截图/退出使用明确有限期限。EXIT 清理同样有界，仅处理该脚本记录的自有 PID，必要时 TERM 后有限宽限再 KILL，不结束日常 App 或共享服务。用零模型替身覆盖「先写失败结果后不退出」以及「成功结果后截图/退出挂住」；断言有限失败、原始结果保留、自有进程已回收。现有脚本静态检查不能继续要求无超时 wait 先于失败检查。
- 每次显式点击生成前保存调用计数或等价本次请求边界；等待严格新增的 `speaker.generate` 请求，校验全新 requestId、预期 modelId，再读取 JSON-safe draft。特别是 Omni 自动模式后切换 Vox 的第二次请求，不能借前一次 Omni 的空 voxMode 通过。保持生成正文、模式、选项及来源不变等业务断言。
- story fixture 使用合法且非空的旧轨道/片段，操作前断言当前来源确为 story、旧 cue 及轨道/片段存在。请求 draft.id 必须对应预期 story 来源，目标正文正确、wire 无 subtitles/timeline，原来源完整快照保持。
- 同一 Terra owner 完成上述三个局部修正、必要零模型脚本检查、受影响合成用例与 lint/build；已完成且相关输入未变的全量浏览器/Go/Swift证据可复用，受改输入不能复用旧通过。修正收敛后固定最终源码指纹；原生 App 保持 run5 构建及失败基线，不再另起第二次。真实 TTS、下载、Git提交与发布仍不在本节授权内。

### 2026-10-15 晚间：断言收敛与原生验收执行策略

用户在了解真实识别与绑定生成的资源差异后授权“按这个方向去做”。沿用同一 TASK-001、原 Terra 实施者、全部失败记录与旧诊断 allowance 状态；当前恢复快照为 `42bb2e0ac23500bb740cd91f7af3415a1a5723b0`，HEAD 未变。不把换轮次或补断言当作重置交付段。

- 合成桥接的取数边界须对应真实 JSON wire。先断言已经捕获本次 `speaker.generate` 请求，再返回 JSON 可序列化的快照，之后检查必要字段。不能以跳过/删除业务断言掩盖 undefined 错误。普通 Draft 的 modelOptions 可保留已知 TTS 家族的合法设置供切换模型后复用；不能因选择 Qwen 就要求整个映射为空。当前夹具中的 Index chunk 设置应保留，music/score 的非 TTS 选项则不能泄漏进独立 TTS 草稿。
- 新故事用例必须先断言真正选中了带非空旧字幕/时间线的 story 来源。当前桥接夹具在每次导航 reload 时重新构造初始 state，直接修改后 reload 会丢失 story 设置，不能用它证明剥离逻辑。生成请求只含本页目标正文与 TTS 设置，原 story 快照保持不变。
- 主只读检查发现 smoke 的 `publicRuntime` 声明在 try 块内、catch 却从块外调用，失败路径会额外抛 ReferenceError。将诊断 helper 放入 catch 可访问的词法作用域，用零模型强制失败回归验证确实返回 `{failure, diagnostics}`；保留公开 DTO 字段白名单。配置/页面未就绪及时返回分辨信息，不能再等 180 秒才报告笼统 runtime timeout。
- 第一批完成必要的合成用例、完整 lint/build/Playwright，以及受改脚本与 Go DTO 的检查。以新的归档目录保存报告并绑定源码；零模型验收完成时实施者主动回传一次命令/退出码/候选指纹，再继续已授权的构建与原生旅程，不要求用户逐步批准。主同步接受源码与断言质量。现有用例内补实际不自动生成调用、生成选择、来源状态和请求 ID 的必要断言，不扩成新功能。
- 使用新 bundle、新资料及结果目录，保留 venv Python 入口，运行尚未启动的唯一一次固定 CPU 声纹旅程。检查两个准确 known ID、unknown 无选定 ID、删除/重启不复活、完整普通 State/Voices 保持及输入/源码/App 指纹。新失败立即保存并返回，不在 worker 内换目录重跑。上次运行未执行该旅程，不能把旧 run4 当本次结果。
- TTS 资源准备与上述实现并行，只读查询已知 App/项目缓存、引擎版本、模型家族/大小和参考音频是否有保存原文。不得自动导入或更改日常数据，不拿官方识别样本做克隆素材；找不到就如实列缺项。没有新增下载、真实 TTS/GPU 推理、provider 或外部发布授权；条件具体后由主提出有限运行方案及需要的授权。
- 禁止无信息的长等待。实施者在零模型检查结束和原生终态两个完成节点主动交接；权限拒绝、需要用户操作或真正阻塞时立即报告具体操作与原因。主在有新因果证据或验收结果时推进；两个无收敛尝试依既有规则停止，不重复派发同一失败。必要检查运行期间不因正常耗时或单次 wait timeout 伪造阻塞。

### 2026-10-15 配置边界与原生失败报告恢复方案

本节优先于下方同事项。用户在要求停止重复等待后授权继续；这不重置同一交付段的失败计数或未知的旧诊断 allowance。当前源码快照为 `6684228185ad7febc458af229876c1cdddc2b44b`（HEAD 仍为 `822c0a4b7b0465e5f70a0331bb396e2fedadd901`）。主 Agent 的本次只读审计没有启动 App、模型或重复测试。

- 构建输入允许用户提供相对路径，但输出的验证 bundle 不得依赖启动 cwd。构建脚本在任何构建或替换操作前，以仓库为基准规范化 Python、模型、runner、FFmpeg、App 输出及验证数据目录的路径，并检查既有运行文件的类型/可执行性；plist 和 FFmpeg wrapper 只写可用的绝对路径。保留现有环境覆盖优先级，不改变 Go 的识别配置合同，不用宽松校验绕过错误。普通发行入口和日常数据目录不变。
- smoke 同样规范化显式相对路径和自己的结果/数据目录；从非仓库 cwd 启动也不能把 logs 写向只读根目录。零模型脚本/Swift 检查应覆盖含空格的相对路径、缺失资产拒绝、bundle fallback 与环境覆盖。使用实际 Go `go test -cover ./internal/recognition` 检查既定识别覆盖率，不把缺少 Python coverage 包误报为此项无法执行，不为本段新增安装依赖。
- 原生主旅程的就绪失败必须区分导航/页面未挂载、`speaker.runtime` 明确 `configured=false`、RPC 错误与等待超时；保留可公开的 runtime DTO、导航/关键控件存在性及页面错误，不输出私有配置路径或特征。只读 RPC 可用于诊断和核对，登记/查询仍必须由真实 UI 按钮及文件输入完成。
- 每个 smoke phase 产出 `{failure: ...}` 时立即失败并保留现场，不能把存在 result 文件当成功继续 restart。配置与页面就绪检查先于固定资产识别；零模型谓词与打包配置回归通过后，仅运行一次使用新源码、新 bundle 和新目录的既定真实旅程。该轮出现新的失败先返回证据，不由 worker 改名目录后自行再跑；不启动物理麦克风或真实 TTS。
- music/score 转换按真实持久化 JSON 处理 TTS 专属字段的空串、null 和零占位，不能让它们覆盖合法 TTS 默认值；只在独立临时草稿内补齐，保留合法显式参数及原作品。用该实际序列化结构验证 UI 转换结果与桌面 DTO/schema/BuildRequest 的组合，不再只从已合法的 createDraft/DefaultDraft 构造测试。Omni/Vox 未设置模式时，控件必须明确显示“自动”或与服务绑定后实际默认相同的模式；保留显式 design/clone/continuation，不静默转换。Vox 自动续写时不显示可编辑但会被忽略的风格指导。
- 主 Agent 只结束已核对属于本段的作业。上轮记录的 PID 67274/67277 在本次只读进程查询中均已不存在。实现、必要检查和修正仍交原 Terra worker；独立 reviewer 只审不可变源码。worker 不写 state、不提交/推送、不请求正式通过；缺失证据保持未完成。

### 2026-10-14 当前有效恢复方案

本补充沿用 REQ-002 的 22 个场景、单一 TASK-001 和既定写边界，优先于初次采集的旧行号与“尚未实施”描述。当前实现已存在，但尚未完成本地 App 验收。REQ-003 是已捕获的后续需求，本 Task 不提前实现共享音色查找或故事角色新入口。

- 原实施者句柄在本次运行时已不存在；主 Agent 的一次权威运行树检查只返回 `/root`。新 Terra worker 接手同一 TASK-001 的实现、必要验证和修正，保留旧工作树及失败历史；不新建假 Task 或重置交付段。
- 原生验证先修正有因果证据的错误谓词：先等待页面的识别配置就绪状态，再准备登记输入，随后验证登记按钮启用。不能在无输入时等待登记可用，也不能删除就绪断言来掩盖配置问题。重启路径等待真实页面和列表加载终态，不用固定睡眠猜测。 档案行必须以明确的行标记读取，不能用同时匹配标签输入和绑定选择器的 `speaker-profile-` 前缀计算数量；删除和重启均比较服务公开 ID 与实际档案行。原生执行前用零模型断言确认两行、删除一行及列表异步终态的谓词。
- 定向取消的本地总期限包含每次 RPC 等待，以既定 5 秒墙钟上限约束。未确认只报告未确认；后台仍占用单操作槽时不伪装已停止，不无限重试。查询在本地取消/输入失效/离开后不采纳迟到结果；登记和生成则保留提交接受后真实成功的合同。Blob 转换前后、操作发送前后、卸载都核对自己的请求和输入版本。取消意图、取消 RPC 在途和原请求终态分别记录；快速 `false` 不能在约 300 ms 耗尽整个注册前取消窗口，以有限次数、有限间隔覆盖剩余墙钟期限。未确认后仍在处理时允许用户再明确尝试，不能由 `cancelIntent` 永久拒绝；服务成功已返回后，迟到取消 ack 不得覆盖成功或阻止列表刷新。
- 补齐既定 UI：录音计时、60 秒自动停止说明及播放协调；档案登记时间和公开版本；绑定参考只读展示及模型支持的情绪/速度等设置，复用现有 Inspector/控件，不能重新开放冲突的主参考输入。保留 music/score 来源的独立 text 草稿兼容修复。
- 用有界可控的合成桥接和 HTTP 在途测试覆盖 22 场景中缺失的状态/取消/关闭/已接受成功/删除与资源清理。记录每个场景的真实断言与证据等级；不能把 6 个成功用例当成全部场景通过，也不能以合成结果替代原生识别。
- 验证产物绑定完整候选源码、完整 Web assets、宿主/服务/运行包和固定资产指纹。核对 eval manifest 的预期 SHA、实际 Python/sherpa 版本、每个查询的结构化结果和耗时；State/Voices 前后比较涵盖完整业务字段，若排除易变元数据需明确逐项说明。
- 首先完成必要零模型检查，再构建当前源码的独立验证 App。仅使用既有固定本地资产、CPU 单线程，运行既定双登记、双 known、一 unknown 和删除/重启旅程；无 provider 调用、无模型下载、无真实 TTS/GPU 批量运行。只结束本任务明确拥有的 App/服务，不能停止用户正在使用的实例或共享服务。
- 本地 App、源码和可审查证据完成后，由主 Agent 处理本 Task 的 Git 授权与正式 Validate/Review/Release；本次 worker 不提交、推送、修改 state 或请求正式 pass。真实系统麦克风和人工 TTS 听测如未执行继续明确 NOT_RUN。

### 目标、授权与统一交付边界

在原 yovoice 的导航、共用 Web 界面、Swift 宿主和 Go Workbench 中实现声纹登记/识别/列表/删除/绑定生成。交付同一源码的本地 Mac 验证构建，走真实 WKWebView、本地服务和已经准备的固定识别资产。保留普通 App 的构建、原 CLI / v1 HTTP、公开 Profile JSON、State 和持久化格式。

一个 TASK-001、一个新 Terra worker 拥有完整实现、必要测试与修正。此任务跨 Web、桌面传输、Workbench、原生宿主、构建及验证资产，分拆多个实施者会让取消、临时素材和验证构建契约分散；目前没有可独立交付的第二实现单元。主 Agent 已接受本工程 Plan，以 `worker` / `fork_turns=none` 派一个完整实现任务。需要的分析视角为 architect、client-dev、server-dev、design、security、qa；验证为 correctness、e2e:Web、桌面 RPC 集成及固定实模 eval。macOS AppKit/WKWebView 不是移动 App，本需求不使用移动 Maestro 代替真实 Mac 验证。

已授权本地页面、必要测试、原 App 本地构建。没有本次 Git commit/push、主线合并、公证、发行或 GB 级 TTS 下载授权。完成可审查源码、检查与本地 App 后，主 Agent 处理必要 Git 授权；在未获授权形成干净 implementation commit 前，不请求正式 validation pass / review / release。工作树检查证据先绑定明确的未提交源码指纹，不能伪装成已提交验证。

## 当前源码锚点

所有行号对应上述 HEAD，新增代码位置由 worker 在既有职责边界内选择。

| 位置 | 当前事实与复用用途 |
|---|---|
| `internal/desktop/rpc.go:18` | `Call` 解码桌面 RPC；defer 中记录 `diag.Error`，所以声纹失败必须在返回前脱敏 |
| `internal/desktop/server.go:16`、`:33` | Server 已拥有关闭 context、在途调用 RWMutex；cookie / loopback / Origin 校验在路由前，body 上限 28 MiB |
| `internal/workbench/speakers.go:28`、`:39`、`:52` | 服务进程识别配置只加载一次；列表只返回 PublicProfile，删除独立私有仓库且幂等 |
| `internal/workbench/speakers.go:71`、`:143`、`:176` | 显式标签、临时 reader 登记和查询入口；From 方法负责接收临时输入并清理，不需要导入 Voices |
| `internal/workbench/speakers.go:186`、`:252` | `SynthesizeSpeaker` 在编辑锁内重新确认档案/绑定，再启动现有生成；禁止 caller primary voiceId/referenceText；正文和支持的情绪仍由 Draft 提供 |
| `internal/workbench/speakers.go:390`、`:404` | 识别等待当前 op，取消只取消其 op；已接受成功不会改成取消；与 TTS 共享 `w.cancel` 单槽，但识别没有 Activity |
| `internal/workbench/workbench.go:240`、`:248` | 旧 `Cancel` 取消当下全局操作，不能给新页面的迟到请求复用；Close 清理全局槽 |
| `internal/recognition/recognition.go:40`、`:70`、`:151`、`:312` | 稳定错误原因、配置、PublicProfile 与 Identification 是权威；配置允许有限 timeout 至 10 分钟，默认 T=.6/M=.05/120s |
| `internal/workbench/voices.go:100`、`:119` | `receive` 限制上传 20 MiB，错误移除部分临时文件 |
| `web/src/shared/lib/bridge.ts:23` | 每个 desktopCall 自己的 Promise/消息 ID；当前统一 120000 ms timeout，ID 带 UUID 连字符 |
| `web/src/app/sidebar-nav.tsx:10`、`web/src/app/workbench-chrome.tsx:47`、`:187` | 原导航和 page state，添加声纹页，不重建 AppShell 或覆盖创作 session |
| `web/src/features/media/voice-picker.tsx:15`、`:123`、`:149` | 已有 getUserMedia/MediaRecorder、1–60s 录音、计时/播放；保存和 import 会写 Voices，需分开临时录音与持久保存 |
| `web/src/features/create/inspector.tsx:48`、`shared-controls.tsx:8` | 模型/情绪/高级参数拥有者，已有 embedded、generationAction 与管理控制；绑定 primary reference 需要只读展示 |
| `web/src/shared/workbench.ts:80`、`:107`、`:111` | Draft 默认与 runtimeStatus/runtimeUsable；生成准备判断复用现有规则，不能把识别 configured 当作 TTS ready |
| `desktop/macos/Sources/VoiceWorkbenchMac/main.swift:17`、`:99`、`:106`、`:120` | 默认 data root、服务启动/迁移与环境传递；只按 env 判断迁移会让 bundle fallback 意外迁移日常目录 |
| 同文件 `:22`、`:208`、`:215`、`:239`、`:351` | 相同 bundle ID 激活旧实例；DEBUG smoke 注入已存在；可信 main frame 麦克风权限已存在；HTTP timeout 当前 120s |
| `cmd/yovoice-service/main.go:54` | 仅 bundled engine 存在时 UseBundled；无 TTS engine 的真实识别服务可以启动 |
| `scripts/desktop/build-macos.sh:23`、`:41`、`:43`、`:44`、`:75` | 原标准构建涉及宿主、audio.cpp、FFmpeg/Kokoro 打包及签名，不能冒充无依赖验证包已跑完整入口 |
| `scripts/desktop/sign-macos.sh` | 标准 App 签名要求 engine/ffmpeg 的 Mach-O；不能直接给无 engine 验证 App 使用 |
| `scripts/desktop/smoke-macos.js` | 旧 smoke 明确要求 Metal runtime 且导入 Voices，与本需求真实识别旅程不同，保留原入口 |
| `scripts/recognition/eval-manifest.json` | 固定模型/5 段音频/哈希、T=.6/M=.05/120s/256；真实验收不修改样本或阈值 |
| `web/package.json`、`web/playwright.config.ts` | 原 pnpm/Astryx/React/Playwright 配置；没有 tester-army，保留现有 lint/build/test 和套件 |

## 复用评估与取舍

| 需求 | 候选与证据 | 决定 |
|---|---|---|
| 桌面声纹链路 | ① 复用 Workbench + 扩私有 desktop RPC；② 在 App 外启动原 v1 服务；③ 重做专用客户端 | 选①；单数据目录、原信任和共享单槽成立。②增加端口/Token/进程及数据互斥成本，③不符合原 App 验证要求 |
| 临时录音/试听 | ① 抽出真实两处使用的 recorder/playback；② 直接使用 VoicePicker 保存；③ 引入录音库 | 选①，保留 VoicePicker 原保存流程；②会写 Voices，③现有浏览器和宿主能力已够用 |
| 模型生成参数 | ① 小范围扩 Inspector/Reference 控件以锁定绑定输入；② 复制情绪与模型规则；③ 新参数 schema | 选①或抽出确实复用的参数部分；选择由 worker 在接口约束内决定。参数语义仍由既有 Draft/schema/ModelOptions 拥有，不复制规则 |
| 页面设计 | 现有 AppShell + Astryx rows/List/Table、状态/Token/确认控件；未发现需要替换设计系统的缺口 | 扩现有依赖。主已成功执行 Astryx build/layout；worker 再按 AGENTS 读实际所用组件 props，不 init/shadcn/新 UI 框架 |
| 真实 Mac 验证构建 | ① 原标准 build 下载并编译全部依赖；② 原宿主/service/Web 的独立本地验证 bundle + 已装外部 FFmpeg；③ 浏览器预览 | 选②，保留①生产入口；③只能辅助，不满足 SCN-UI-22。②不造假 engine，不宣称发行 8.1.2 转换器或真实 TTS 已验证 |
| Web 生命周期验证 | 原 Playwright 补充 + tester-army/e2e CLI/Web engine；不自研 runner | 引入固定成熟 devDependencies。主已在线/registry 核对 e2e 0.19.0、@e2e-dev/web 0.14.0、playwright-core 1.63.0 和 Node 兼容；worker 安装后以包内 docs/实现确定具体 API，不能猜 config/CLI 参数 |

不变更识别算法、仓库格式、模型训练、公开远程协议；只补现有能力无法提供的 desktop adapter、单请求取消句柄与本地验证构建。

## 桌面私有接口与版本兼容

方法名固定为 `speaker.runtime/list/enroll/identify/delete/generate/cancel`。都是既有 `/api/call` 上新增 method；旧 method、旧 envelope ID 与 error wire 不变，无公开 v1 版本升级。新 operationId 不借用 envelope 的 UUID ID：在 data 内使用名为 `requestId` 的全新 32 位小写十六进制 ID，每次用户显式执行生成一次，终态后不复用。

| method | data contract | result / owner |
|---|---|---|
| `speaker.runtime` | `{}` | 新公开 DTO：configured；成功配置时 threshold/margin/timeoutSeconds/modelSha256/featureVersion/dimension。由 Workbench 读取其缓存配置并只挑公有字段；失败为 configured=false。不能暴露 Python/model/runner path 或命令，configured 只表示连接配置通过，非实模质量通过 |
| `speaker.list` | `{}` | `PublicProfile[]`；固定字段照 Go 类型，空列表返回数组，读取失败返回 typed error，不依赖模型已准备 |
| `speaker.enroll` | `{requestId, source: {base64} 或 {voiceId}, name?, bindVoiceId?}` | `PublicProfile`；source 恰一；不支持 path/model/runner 配置。显式 name 空白非法，省略才用 ID；transport 检查显式空白再调用现有 From/Named 方法，不必增加新的持久化协议 |
| `speaker.identify` | `{requestId, source: {base64} 或 {voiceId}}` | `Identification`；服务 status/reason/score/candidates/T/M/版本是权威，至多 3 候选。音频/查询不写 Voices |
| `speaker.delete` | `{id}` | `{id, deleted:true}`；只删除私有档案，幂等；普通声音/作品/历史不变 |
| `speaker.generate` | `{requestId, profileId, draft}` | 既有 `Generation`；draft 沿现有默认和校验。wire 不含 primary voiceId/referenceText，与 existing emotionVoiceId 等不混淆；绑定解析只在 Workbench |
| `speaker.cancel` | `{requestId}` | `{cancelRequested:boolean}`，表示是否找到该在途操作并请求取消，不代表操作已取消或落盘被撤销 |

source 结构使用两侧同名 typed DTO；transport 严格检查恰一、字段类型、ID、base64 及解码后 <=20 MiB，不让超大编码先形成更多无界分配；保留既有 28 MiB HTTP body 上限。音频时长/格式/有效语音由现有转换与识别处理。初步 UI 大小检查不是 server 检查的替代。path 等禁止字段不可被静默当作有效来源；来源缺失/冲突不能落入正常 unknown。

新桌面错误以有限 `@yovoice.error.speaker*` 消息码返回，`msg.All` 与中英文 catalog 一起更新。用 `SpeakerErrorCode` 的有限原因映射；TTS model/runtime 必需错误在降为普通 recognition fallback 前单独保留为可恢复 typed 提示，避免被错误显示为 persistence_failed。未知错误映射安全通用码，params 不携 raw err/detail/path。新 method 的参数解码和 Workbench error 在到达 `Call` 的 diag defer 前已转换，不能只在 Server encode 时脱敏。不改旧 CLI/HTTP 的 Code/Profile JSON。

## 并发、取消、超时与关闭合同

Server 拥有仅在途的 `requestId → entry(context.CancelFunc)`；最多一个新 speaker 昂贵请求（enroll/identify/generate），短查询/删除/cancel 不占登记表容量。注册、查找与删除只短持独立 mutex，不能跨 Workbench 调用、等待 op.done、编码大音频或 shutdown 等待持锁。核心昂贵操作仍由 Workbench 共享单槽判 busy；此注册表不创建第二执行调度器，也不让识别向普通 State 写假 Activity。

- 原请求的 r.Context 和 s.lifetime 取消本次 owned context；以 existing calls RWMutex 的关闭等待顺序收尾。重复在途 requestId 拒绝，不能覆盖 cancel handle；收尾只移除仍属于本 entry 的记录。
- cancel 先取得本 entry 的 cancel func，再释放 map 锁后调用。A 终态后迟到 cancel A 没有作用；A 的旧 cancel func 即使晚调用也只属于 A，不能调用全局 `Workbench.Cancel()` 并误停 B/TTS。未知/已完成 requestId 不留 tombstone。
- 注册前取消竞态：cancelRequested=false 时，UI 只对仍 pending、requestId 相同、取消意图仍有效的本次操作，以 100 ms 有界重试、总计最多 5 秒；收到 true 或原请求终态即停止。5秒是包括RPC等待的墙钟期限，不能只计算重试间隔；迟到ack仅更新仍相同的操作。到期显示「取消未确认」，不得声称已取消。登记/识别/生成本体绝不自动重发。
- 原操作成功终态优先于取消意图。取消接受点之前的失败不迟到保存；已经落盘成功仍显示成功，取消 RPC 的 ack 不覆盖这个结果。取消/超时分类沿已交付 core，transport 不能在 Core success 之后额外检查 ctx.Err 并改成失败。
- **已由主确认：** JS desktopCall 和 Swift URLRequest 仅对新 speaker 同步昂贵方法使用固定 660 秒有界 transport timeout，旧方法仍 120 秒；核心识别 deadline 继续配置值（本次固定 120s、合法最大 600s）。新 method 达到 transport 期限则结果未知/恢复提示、定向取消与刷新；不能自动再登记。生成沿现有 Workbench cancellation/result，不更改 TTS 推理设置。
- 在 scope 内新增并发和取消测试必须观察因果同步点，不用睡眠猜顺序：A完成→B启动→cancelA→B继续；注册前false→注册后true；重复ID拒绝且A句柄仍有效；accepted success+cancel返回成功；deadline分类；r.Context/关闭取消且临时输入清理。

## UI 数据流与状态所有权

在原 SidebarNav/WorkbenchChrome 增加 `speakers` page。页面接收普通 state/catalog/engine 仅用于已有声音、模型与 TTS readiness，不把档案/识别特征并入 State。`speaker.list/runtime` 在进入/刷新时读取，档案成功变更后重读。新增 TS 公有 DTO 可放 `shared/speakers.ts` 或等价职责文件，字段严格对齐 Go 公有返回。

输入是临时 File/Blob 或已存在 voiceId，登记与查询动作清楚区分。File/Blob 只在当前页面持有；Object URL/MediaStream/timer/AudioContext 在替换、清除、停止及离开页清理。抽出的 recorder/preview 在 VoicePicker 和新页面实际使用，旧 picker 的导入/保存继续显式调用原声音保存；新页面不调用 importVoiceFile、voice.record、裁剪保存或生成 Voices 的 helper。录音限时 60s 明确停止、少于1s失败；文件超过时长不静默裁剪。

页面为每个显式昂贵动作持有 `{requestId, kind, inputEpoch, pending, cancelIntent}`。Promise 仅更新仍属于当前 inputEpoch/操作的结果；更换输入清除结果或明确旧输入，不自动重试失败/未知。本页 pending 防重复操作；已有 Activity running 时提示/禁用相关昂贵按钮，但最终 busy 由 core 判断。页面停止可向自己的 requestId 请求取消并释放采集，绝不触发全局 operation.cancel；结果成功与刷新按上述 accepted commit 语义处理。

列表使用原 rows/List/Table 语义，标签、ID、创建时间、绑定状态与公开版本可扫描；重名可区分。空态与读取失败分开，显式刷新；删除使用原 ConfirmDelete 体验，取消不变、失败可重试，成功重新加载。公共 DTO/错误/页面持久状态不包含向量或新识别执行路径；普通 State 既有非声纹路径不在本任务内做无关重写。

生成使用独立本页 form/draft，不复写创作 session。已识别档案不自动选为 form 的档案；用户明确选择可用已绑定 profile，再点击生成。模型列表只含现有支持克隆的 family/variant，依据 `applySpeakerBinding` 的现有语义；无已安装模型或 TTS runtime 时禁用/显示恢复提示。原 Inspector 的绑定声音/原文应只读展示；可以传只用于呈现的 bound reference，但提交时必须构造去除 primary voiceId/referenceText 的 wire，不能把 caller 原文冒充绑定原文。保持目标 text 和该模型支持的情绪/速度/语言/选项。不要把设计模式转换为克隆模式，或者新增未匹配模型适配来绕过错误。

browser preview 新 method 默认拒绝/标示不可执行真实推理；E2E 的 native bridge fixture 明确是 synthetic，只模拟公有契约，不能通过给 preview 写假档案/假模型成功伪装原生效果。中英文、明暗主题、键盘/焦点/状态名称及默认1280×840、最小390×600窗口对应产品 SCN-UI-21。

## 原 App 本地验证构建与 Finder 启动

拟新增独立 build/smoke/check 脚本（建议 `scripts/desktop/build-speaker-validation-macos.sh`、`smoke-speaker-validation-macos.sh/js` 及脚本/配置检查）；它们由 worker 实际运行确认后才能写成已验证使用命令。普通 `build-macos.sh`、sign/package/run 标准入口不改其生产依赖和语义。

- 使用当前 Web dist、当前 Go service、当前 Swift debug host（debug 仅为复用原 smoke hook），在 ignored `artifacts/speaker-validation-macos/yovoice.app` 中组装；正常可双击使用，验证脚本在原 WKWebView 内执行。用稳定独立 bundle ID `app.voiceworkbench.desktop.speaker-validation`，表明同源码本地验证构建，避免激活日常 App；原 bundle ID 保持。
- 验证包可有 `YovoiceSpeakerRuntime` 六项白名单字典：PYTHON/MODEL/RUNNER/THRESHOLD/MARGIN/TIMEOUT 对应现有环境名，不添加执行参数、下载地址或任意 RPC path。从操作者明确环境/已准备本地资产生成 artifact plist，源码不含个人绝对路径。Swift 子服务 env 中显式同名环境值优先（存在空值也不偷偷改用 fallback），不存在才使用 bundle 字典；无字典的标准包行为保持。runtime configured/readiness 由 Go 验证，不由 plist 存在推断。
- 可用 `YovoiceSpeakerValidation` 标记和 `YovoiceValidationDataDirectory` 指定验证包私有 data root；显式 WORKBENCH_DATA 优先。原默认包继续 `~/.yovoice`。解析后只有 standard default root 才调用 `--prepare-data`；env/custom/validation root 都跳过。不写 Preferences、State 或新的 UserDefaults。界面/使用文档明确这是独立验证资料，不冒充日常数据。
- 不造 fake audiocpp engine，不下载 audio.cpp/FFmpeg/Kokoro/TTS。使用受信、已安装 Homebrew FFmpeg 的绝对路径，验证包 tools/ffmpeg 为正确 shell quoting 的 exec wrapper（或验证支持的 symlink）；记录外部 binary SHA/version。**不能 codesign、替换、修改、复制后覆盖或终止全局 Homebrew 资源。** 仅 ad-hoc 签副本 service、host、bundle 本身，沿原 entitlements 和先子后包顺序；不调用标准 sign-macos.sh，不公证/发行。
- 构建前检查自身输出和目标 bundle 是否正在运行，运行中则退出并解释，不能替换 App、杀日常 App 或用户服务。只在本脚本自有 staging/输出目录中做原子替换；不清除原数据、识别资产或其他证据。
- 新 Swift 配置 helper 用可注入 env/bundle 字典的纯解析方法测试优先级/白名单/无字典兼容/迁移选择；不让单元测试启动系统麦克风或默认资料迁移。无需改变既有 main-frame 麦克风信任验证。

## 验证准备、证据和 22 场景映射

所有下列项目是实施与验收要求，Plan 阶段没有执行它们，不称为通过。已有测试命令源于 project.md/Makefile，新增脚本及框架命令待 worker 实际验证。

| 场景 | 实施责任与必要证据 |
|---|---|
| SCN-UI-01 | Sidebar/Chrome 真实导航 + E2E 断言原正文保持；native进入声纹页 |
| SCN-UI-02 | RPC 临时文件登记、公有返回/绑定及 Voices 不增；E2E 表单/列表；native 真文件登记 |
| SCN-UI-03 | recorder synthetic 流完成、未绑定标示/不写 Voices；真实系统麦克风采集独立 NOT_RUN 可报告，不以假流代替 |
| SCN-UI-04 | getUserMedia denied/notfound fixture，恢复指引及文件路径能继续；原信任权限 Swift 回归 |
| SCN-UI-05 | 重录/clear/离开 tracks/timer/url 清理、60s自动stop及少于1s拒绝；inputEpoch旧结果保护 |
| SCN-UI-06 | RPC返回 matched完整字段/查询只读；E2E无生成/无自动选择；native known查询 |
| SCN-UI-07 | core空库unknown无score；E2E空库结果不是错误或自动登记 |
| SCN-UI-08 | 公有unknown/ambiguous fixture有限候选、无身份/生成；既有core精确判断回归 |
| SCN-UI-09 | 人工精确T/M/舍入可相等的结果fixture，UI只展示服务status；既有recognition边界测试 |
| SCN-UI-10 | desktop source/type/base64/20MiB、标签；已有core1–60s/坏音频测试；E2E恢复无Voice增量 |
| SCN-UI-11 | runtime公开DTO缺配置/版本/runner错误；列表删除可用；Swift env/fallback重启；E2E错误不是unknown |
| SCN-UI-12 | 公有列表/同名ID/refresh错误；native重启确认档案，CLI同目录变更只能App退出后进行，不突破进程锁 |
| SCN-UI-13 | 幂等删除/确认取消/错误不丢列表；core参考Voice历史不变；native删除后重启不复活 |
| SCN-UI-14 | core识别完整快照/生成重新查存在；desktop stale profile生成错误；E2E恢复不回退其他档案 |
| SCN-UI-15 | desktop按profile调用现有fake engine断言参考音频/原文、目标text/情绪；E2E参数及正常播放器；真实音质 NOT_RUN，无TTS下载 |
| SCN-UI-16 | 缺绑定/绑定删除/不支持模型错误；E2E明确选择及禁用/修复；无第一候选自动生成 |
| SCN-UI-17 | 无TTS runtime/models页面提示禁用，识别仍可用；native验证包无engine真实打开且识别成功 |
| SCN-UI-18 | registry/core共享busy、重复点击、deadline/取消、lateA不影响B、关闭、注册前竞态、accepted success；UI pending与ack不假终态 |
| SCN-UI-19 | unknown response先refresh、无body重试；同名同音频显式新登记独立ID，core现有断言+UI计数 |
| SCN-UI-20 | desktop auth/Origin/forbidden source，错误与diag先脱敏、公开DTO/State无embedding/private路径；native query前后Voices/State一致 |
| SCN-UI-21 | tester-army确定性locator/expect旅程：1280×840/390×600、zh/en、两主题、键盘焦点/图标名称/长ID/列表和错误态；截图辅助 |
| SCN-UI-22 | 新构建真实 WKWebView UI双登记/双独立known/一unknown；固定资产和参数，样本预期/实际/分数/耗时/分母及全链路指纹 |

### correctness 和现有回归

- 按源仓约定运行 formatter/vet、`go test -race ./... -count=1 -timeout=90s`；新增 desktop RPC/registry/脱敏/源边界/取消 race 测试，现有 CLI/v1/Speaker/生成断言不能退化。检查 goroutine/临时输入清理和 shared-slot busy，不能只断言无 panic。
- 运行 `make check-recognition` 所含 Python runner/eval 4+1，以及 recognition 有效覆盖 >=80%；修改识别算法/runner/manifest不是本任务默认范围。对新增/修改桌面代码收集可用覆盖率与分支行为清单；无法统计的范围明确说明，不把未测当通过。
- `swift test --package-path desktop/macos` 含原5项和配置/超时选择解析回归；新增脚本检查替身验证拒绝运行中重写、路径quoting、不下载、不碰Homebrew、签名顺序、缺资产/失败保留证据。
- 完整 `pnpm --dir web run lint`、`pnpm --dir web run build`、`pnpm --dir web test`；保留现有84项套件并补受影响回归，不能靠改旧断言规避既有流程。构建 dist 必须来自当前候选源码。
- 原标准macOS service/smoke前提是标准bundle/Metal；本次无engine验证bundle不适用其运行结果，记录 NOT_RUN/不适用原因，保留原入口。真实外部FFmpeg验证及新本地native smoke替代的是本需求的验证包旅程，不声称标准发行包通过。

### tester-army / Web

在 web 的 devDependencies 精确固定 e2e=0.19.0、@e2e-dev/web=0.14.0，保留原包管理与配置。新增独立测试脚本名、e2e.config 与已跟踪 *.e2e.ts/fixtures，不用 init 覆盖 Playwright。安装后先读包内 docs、实际运行 CLI help/list，确认测试确实收集，再写使用命令。

环境为当前本地 Vite/构建入口和新鲜browser context，不使用既有旧服务为新dist背书。新fixture注入原native桥以模拟公有返回，标示 `synthetic`；fixture不包含embedding/path，不运行python/TTS。确定性 locator+expect，无 agent.act/assert/extract/waitFor 等模型步骤；provider/凭据为 none，预算0调用，workers=1、retries=0、telemetry关闭，Validate显式 read-only cache（strict策略由安装包支持的实际API固定，不宣称strict等于0调用）。复用当前1.63浏览器缓存，不额外下载大浏览器；具体版本与用例数在实际报告记录。每条核心旅程覆盖成功、错误、空态及恢复，截图不能代替确定性业务结果。

原始report/JUnit（若实际启用）、日志/trace/screenshots放ignored测试产物，新运行使用独立归档目录，成功和失败都保留；不得覆盖旧失败结果。框架运行不得调用模型provider或引入未知预算。CI已有：增加单worker无模型的本地e2e检查及报告上传可在原check workflow内最小接入，不触发新push或发行；独立Validate重跑同一配置不改测试。

### 真实原 Mac App / 固定模型

新smoke复用原 DEBUG `callAsyncJavaScript` 注入，fixture数据准备在自有临时目录；仍在真实 WKWebView 页面使用 input File/Blob 与按钮完成业务，不直接用RPC替代登记/识别按钮。可在harness注入官方样本为File/DataTransfer来避免自动控制系统选文件面板；它验证文件UI路径，不声称真实人类麦克风。读only RPC用于核对公开列表/State可以作为业务断言附件。

严格检查eval-manifest的model、5sample、runner SHA和前后源码/manifest/assets不变；沿已准备Python3.12.13/sherpa1.13.8/CPU1thread/256/T.6M.05/120s，不校准。先等App普通State稳定，保存query前后State/Voices规范化指纹；页面UI双登记生成A/B public ID，再用两段独立query断言matched各自正确ID，一段unknown断言未选择身份。不能把同段复制当独立查询。保留每次公开Identification、预期/实际、分数/分母、耗时及UI终态；检查响应没有向量/私有路径。

随后列表/删除/重启确认不复活（独立本次data-dir，不在日常目录做清理）。harness有限超时、只停止自有PID，失败保留原日志/报告；私有临时特征资料在完成断言后仅按自有临时目录清理，不作为永久证据上传。App/Webdist/service/Swift host/plist/FFmpeg wrapper/外部FFmpeg/model/runner/samples/工具版本、构建commit或未提交源码指纹前后都入脱敏manifest；个人路径仅artifact内受信配置，不写跟踪docs或输出为前端错误。无新模型、无TTS音质结论，真实Mic/人工听测单列NOT_RUN。

## TASK-001 写边界、完成条件与使用说明

标题：`实现原 Mac App 声纹页面、定向取消和本地验证构建`；无Task依赖（REQ-001已满足）。以下是新worker完整纵向任务范围，不是允许任意清理整个目录：

- Web：`web/src/app/sidebar-nav.tsx`、`workbench-chrome.tsx`；新增 `web/src/features/speakers/**`；必要的media录音/试听抽取及VoicePicker两处使用；Inspector/Reference/Vox等锁定绑定的最小复用改动；`shared/speakers.ts`、`shared/lib/bridge.ts`新方法timeout、preview拒绝、新typed错误/中英文catalog；相关 scoped样式/token和测试。
- Go：`internal/desktop/{rpc,server}.go`或职责清楚的新speakers adapter/registry文件与测试；`internal/workbench/speakers.go`只新增public runtime DTO/accessor等必要适配，不改已交付core决策/持久语义；`internal/msg/msg.go`公有typed code清单及测试。既有CLI/remote/recognition/store/schema不修改协议、算法或格式，测试可补必要回归。
- Swift：`desktop/macos/Sources/VoiceWorkbenchMac/main.swift`及最小host配置/timeouthelper，新测试；原权限/trust/退出机制保持，必要环境/data解析变更在上述契约内。
- 构建与验证：新增 `scripts/desktop/*speaker-validation*`脚本/检查；原script生产行为不改；`web/package.json`/pnpm锁、独立e2e config/tests/fixture、`.gitignore`仅新增runner产物ignore；原 `.github/workflows/check.yml`可最小增e2e/附件；不改publish、发行签名/公证配置。
- 文档：`CHANGELOG.md` Unreleased面向用户变化；全局 `docs/usage.md`增加稳定USG导航及SCN-UI关联，链接现有英文/中文开发/使用说明。文档明确操作前提、六项配置owner、独立验证资料、当前真实/未运行范围。不复制产品全文或写个人绝对路径/secret；新命令须worker实际跑通后记录。根soul/user/AGENTS/CLAUDE不改。
- 产物写ignored `artifacts/speaker-validation-macos/**`及本轮verification自有目录，测试runner自有ignored输出；不覆盖旧证据。worker不修改state/有效Plan，返回必要工程决策和实际证据，由主记录。

完成条件：产品22场景映射的必要行为都有实际断言/旅程证据；完整核心/Web/Python/覆盖/Swift/新脚本检查通过；已安装框架真实收集运行受版本管理的候选测试（Build可在未提交快照运行，正式Validate按commit一致性执行），0provider调用、无retry/缺失用例；新原Mac App真UI固定双known+一unknown通过，queryState/Voices不变；安全定向取消及脱敏/临时清理可证；可Finder打开独立本地App且TTS缺失可恢复；实际Mic/TTS听测诚实列未运行。正式生命周期commit/validation接受留主，在现有授权下不push/发行。

## 风险、回滚与发布后观察

主要风险是迟到/注册前取消、120秒transport抢先超时、误把临时输入保存为Voice、只在服务层脱敏导致diag泄露、Finder无环境配置、默认迁移触及日常资料、签名脚本修改Homebrew或标准bundle依赖，以及synthetic/旧App被算作实模证据；各项已有明确合同与反例测试。两个无收敛实施/修正尝试的同segment必须遵循根非收敛停止规则，不用改名Task或重派worker清掉历史。

回滚只撤回新入口/新method/本地bundle配置支持，停止并移走本次独立验证App（确认自有且未运行），保留用户资料/私有档案/Voices/历史及旧证据；不重置Git、不清日常目录、不删除模型、不改原CLI/v1。旧标准包和原TTS请求仍可用。

长期方向文档none，用户原App验证请求为方向依据；预期结果和观察按REQ-002：首次本地交付后7天，用户/开发者人工记录误匹配、unknown/ambiguous、时延、录音/绑定/恢复失败，并注明模型/样本/源码/App版本。无遥测、无自动monitor；改变范围的反馈进入新的依赖REQ-002的Requirement。具体release状态需要主按实际交付与授权决定，不因生成验证bundle就声称完成外部发行。

### 已接受契约与未决

主已接受：新speaker同步transport660s、bundle独立data解析后跳过默认迁移、独立稳定bundleID、注册前cancel仅同pendingID有界5s重试，以及不修改/codesign外部Homebrew资源。本 Plan 的接口、写边界、验证策略和验收条件均已接受；常规实现选择由同一 Terra worker 完成。没有阻塞实施的产品问题或新增外部授权要求。


## Build evidence

### 2026-10-15 中断核对及配置定位

上轮 `smoke-run-3/main-result.json` 为 delete confirmation 超时；`final-current/smoke-run-4-current/main-result.json` 为 speaker page runtime configuration 超时，service 日志最终于 00:58:03 正常停止。原 worker 的交接记忆停留在该文件出现前，不能以其“无 main 结果、进程仍在”的旧观察替代磁盘终态。已保留全部失败目录。

只读审计确认 run-4 的 plist 将 Python、模型、runner 和验证数据目录写为相对路径；HostConfiguration 原样传递运行配置，而 `recognition.LoadConfig` 明确拒绝相对路径。因此 bundle fallback 不满足既有配置合同；是否另有启动环境覆盖不能从该报告确定。Finder 只读 logs 弹窗与相对数据目录相符，但缺少现场 cwd/NSError，不能宣称其唯一原因已证实。run-4 的 runtime 就绪谓词已不再要求无输入时登记按钮启用；截图内容空白仍缺 DOM/runtime/错误信息，按新失败报告要求补证，不能把配置修正当作原生已通过。

主 Agent 冻结的候选树 `6684228185ad7febc458af229876c1cdddc2b44b` 及与上一候选的差异存于 `artifacts/verification/feature-002-recovery-20261015/`。现有 `web/e2e-results/report.json` 的 `run.summary` 为 42/42、failed/skipped/flaky 均 0，`run.usage.modelTokens` 为 0；这仍是工作树合成桥接证据，不是正式 Validate 或真实模型/App 通过。

独立只读预审确认原先模型字段、显式模式、跨面板录音暂停/播放阻止、重复 key、绑定试听及清理旧输出修正已存在；剩余两项 P2 为真实 music/score 持久化零值覆盖 TTS 默认，以及 Omni/Vox 空模式的画面与后端默认不一致。证据归属于上述不可变树，不是正式 Review；同一 worker 按本节补齐后才冻结最终候选。

### 2026-10-15 12:54 限时交接：未完成

主 Agent 在已说明的截止时间中断实施者，收到 `incomplete` 交接；没有推进生命周期、提交或推送。中断/耗时不是测试失败或技术根因。当前没有待批准或被拒绝的操作授权；实施者报告的 E2E PID 90271 已结束，上轮 App/service PID 67274/67277 也不存在，未停止共享进程。

源码已补绝对路径规范化（保留 venv Python 符号链接入口）、分阶段失败诊断及立即停止、music/score 序列化零值的 TTS 默认补齐、Omni/Vox 自动模式展示。主定向阅读又发现普通来源不再剥离字幕/时间线会使 Workbench 走分段生成，原实施者已恢复对所有来源剥离组成字段，并区分 Index 2/2.5 的语言集合；这些最后修正尚无最终全量检查背书。

实施者实际证据：`python3 -B scripts/desktop/check-speaker-validation-macos.py` 13 项通过；两个 JS 的 `node --check`、两个 shell 的 `sh -n`、`git diff --check` 通过；`go test ./internal/desktop -run 'TestSpeakerGenerateRequestAccepts' -count=1` 通过；`go test -cover ./internal/recognition` 为 84.4%。此前 lint/build、55 项 Playwright 的通过不能外推到最后修改。

最新合成 E2E run `01a13de3-ebe2-72a5-9812-b6f305efbc1c` 为 44 项中 40 通过、4 失败、0 skipped/flaky/interrupted、0 模型调用。两个窗口配置分别出现：music/score 用例的 evaluate 返回值含 undefined、不满足框架 JSON-safe 合同；普通文本用例仍断言空 modelOptions，与保留合法 Index 选项的既定合同不符。失败原始 report 已另存 `artifacts/verification/feature-002-recovery-20261015/current-e2e-failed-report.json`；42 项旧通过报告单独归档为同目录 `pre-recovery-e2e-report.json`，不能以旧绿结果覆盖当前失败。

下一步需让用例检查真实序列化 wire、断言保留合法模型参数，完成受最后修改影响的全量 Web 检查，再构建新 App 并按本 Plan 运行尚未启动的唯一一次固定资产原生旅程。此项仍是既有技术工作，不缺新增产品决定或操作授权。本次没有新 bundle 或 native pass；physical Mic / human TTS listening 均为 NOT_RUN。保留同一交付段的失败历史和旧诊断 allowance 状态，不能另起 Task 清零。

### 2026-10-14 恢复审计与原生失败历史

初次“尚未开始实施”是历史记录；截至当前恢复基线，45 个已保留路径包含实际实现，原生 App 仍为 2026-10-12 构建，不能为上游合并后的源码背书。2026-10-13 的 Go race、Web 53 项、合成声纹 6 项、Swift 10 项和脚本 5 项成功记录只按同源码/config/环境复用，新增修正须追加必要检查。

已有原生轮次按原目录保留：172432 的空对象源于脚本外层缺 return；172503 无 main 结果/JavaScript exception；172712 缺 nav-speakers；172733 和 173353 均在 speaker page runtime 超时；173158 更改样本加载但没有 main 终态。最后两次明确失败使用了同一错误谓词——无登记输入却等待登记按钮启用，期间样本载入变化没有解决该原因。至少两次未收敛复测历史不因上游合并或实施者更换而清零。

2026-10-14 不可变预审候选 `3d7d762e442a0e581888865de41166a2f90ed8ab` 新发现：`speaker-validation-smoke.js:36` 与 `speaker-validation-restart.js:16` 的前缀选择器同时匹配 `speaker-profile-name`、`speaker-profile-bind-voice`，真实档案行以外至少多两个控件；外层删除后数量断言必然失败。重启脚本第 14 行仅等登记按钮挂载，不能证明异步列表读取已完成。这是源码中可复核的谓词因果证据，不是新模型结果；恢复方案据此增加明确档案行标记与零模型终态断言，旧失败计数和 allowance 状态保留。

本次两个只读审计根据脚本第 41 行和页面登记禁用条件给出了新的可复核因果定位；这不是再次原样运行 smoke，也没有执行主 Agent 诊断实验。旧原生问题段的主诊断 allowance 是否已使用，现存文档无法确认，保留为未知，不视为可再次使用。后续若在相同问题上重新触发无进展停止，先报告并保留现场，不凭未知 allowance 增加实验或盲目派工。取消/录音/呈现等缺口是本次源码合同审计发现，不能称为已有运行回归或伪造旧失败记录。


初次实施尚未完成验收。当前恢复候选已通过 Web lint、TypeScript、录音 Playwright 3 项与合成声纹 e2e 28 项（`web/e2e-results/worker-20261014-4/report.json`，0 模型调用）；完整检查和原生验收仍由同一 worker 完成。不可变预审候选与独立 findings 保存在 `artifacts/verification/feature-002-recovery-primary-review/`；它们不是正式 Validate/Review 通过记录。最终证据仍需绑定实际完成的源码与 App。

## Validation

当前 Feature 验收未通过。当前业务提交 `a001a0fa74e97a4923c44f5637525f79ff97d15c` 的新包在最近一次原生初始化前置检查失败，归因 INCONCLUSIVE；真实登记、识别、删除和重启未执行。正式独立 Validate 的历史通过仅绑定 `767b65ce18f75752a55ebb610e52ffc712950ed3`，不能代表当前提交通过。全部历史失败、Build 与原生证据保留。物理 Mic 与真实 TTS 听测仍为 NOT_RUN。

### 2026-10-19 当前包初始化观察与完整旅程停止

被测业务提交为 `a001a0fa74e97a4923c44f5637525f79ff97d15c`，App 为 `artifacts/speaker-validation-macos/worker-20261018-visibility-fix-final/yovoice.app`，源码 385 文件指纹 `133823b8029e7675dc3974bc43f905a2da2d90d21fdc4e0b301a05d52ca2b570`、完整 App `7f1e692377ffc9ecccc7c548097908671d00df8ec6d7c7987e39f33acc12ac9d`。主已核对最终 receipt、strict codesign、打包资源及实际检查记录；Build 为 E2E 62/62、browser 59/59、Swift 17、脚本 24，Web lint/typecheck/build 通过，均不等同原生旅程通过。原始记录在 `artifacts/verification/feature-002-visibility-fix-20261018/` 的 `primary-acceptance.json`、`build-handoff.json`、各命令日志及根级 `web-e2e-results/report.json`（run `01a14f81-b686-7179-8737-2bd189994ba0`）。Go 未变部分的 218 pass/12 skip 属于此前相同相关输入的 Build 证据；当前提交尚无正式 Validate/Review。

用户手动解锁后，各次授权分别消费并结束，未自动重跑：

| 实际命令与证据目录 | 结果 | 验收边界 |
|---|---|---|
| `python3 artifacts/verification/feature-002-goal-acceptance-20261018/final-visibility-001/run-authorized-native.py` | exit 1，15,465ms；`document_not_visible`，visibility 为 unavailable、active=false、occlusionVisible=false | 初始化前置失败，0 次提取，未执行业务脚本与重启，无截图；原始 result/outcome 与主核对在该目录 |
| 单次宿主使用原 `scripts/desktop/speaker-validation-init.js`，实际参数见 `artifacts/verification/feature-002-goal-acceptance-20261019/init-observation-001/native-command.json`；2026-10-19 15:47:31–15:47:35（Asia/Shanghai） | host exit 0，4 秒，`initialized:true`；主查看真实 `smoke.png`，声纹页正常绘制 | 仅初始化观察，0 次提取；用户看到图标短暂出现，未确认实际窗口到前台。成功路径保存结果/截图后由 DEBUG smoke 主动 `NSApp.terminate`，该退出不是崩溃证据；不构成完整 Feature 验收 |
| `python3 artifacts/verification/feature-002-goal-acceptance-20261019/final-observed-001/run-authorized-native.py`，本次服务日志 2026-10-19 16:00:04–16:00:21（Asia/Shanghai） | exit 1，21,827ms，非外层超时；同一初始化前置失败 | 仅 1 次 host、0 次提取，无登记/识别/删除日志，main/restart 与业务截图均未产生；15 秒观察延时尚未执行到 |

最后一轮输入只对验收产物中的初始化脚本增加导航就绪后 15 秒等待；driver 仅改为产物 shell，shell 仅固定仓库与初始化模板路径，原 App、main/restart、显示和业务断言未改。逐项差异及静态校验见 `final-observed-001/derivation-check.json` 与 `static-checks.json`，原首次静态命令参数错误及缩进比对错误分别保留，均未启动 App。授权原文与预算见同目录 `authorization.json`；限制为一次初始化→主流程→重启、最多 3 次 host/5 次本地 WeSpeaker 提取、CPU 单线程、T=0.6/M=0.05/120 秒、全程 20 分钟，首次失败停止。

最后失败 raw `smoke/initialize-result.json` 显示 windowVisible=true、miniaturized=false、WebView attached 且 1280×840、applicationActive=true，但 windowOcclusionVisible=false、documentVisibility=unavailable。这只能证明在有限期限内没有取得有效的文档可见性值；不能断言 document 实际 hidden、回调从未返回或 App 崩溃，也不足以归因为环境。服务日志记录正常 stopped；worker 确认无自有 host/service/runner 遗留，主只读桌面 inventory 确认 yovoice 未运行、窗口为 0。主核对为 INCONCLUSIVE，见 `final-observed-001/primary-native-assessment.json`；该次预算已耗尽，未追加启动、模型或新诊断。

TASK-001 保持 in_progress；不进入正式 Validate/Review，不复用旧包的原生成功冒充当前提交通过。既有失败历史和已用主诊断 allowance 不变，停止相同输入的盲目重跑，等待用户观察及下一步方向。本节及 project 的后续修改仅同步事实记录，不改变产品规则、实现或当前方案的行为要求；本次 App/receipt 仍绑定运行前的 `a001a0f` 源码快照，不改写旧 receipt 来适配这些记录变化。

### 2026-10-18 空白页定位与当前 Build 边界

`5bec2b86ca7a26bec489ec0420560a3a1cf082bd` 的确定性 Build 检查为 E2E 62/62（零 retry/skip/flaky）、browser 58/58、脚本 23、Go race 218 pass/12 既有 skip，Web lint/typecheck/build、vet、签名与 receipt 核对均通过；原始命令、报告及主核对在 `artifacts/verification/feature-002-review-fix-20261017/`。该目录 `red-script-check-special-path.log` 仅证明缺少 helper，不能称为旧行为红证据；实际旧 wrapper 执行命令替换与新 writer 参数保持的差异由最终绿色的差分执行用例证明。

该提交新包的真实旅程 `final-review-fix-001/` 在 35 秒后因登记控件的透明祖先而失败并立即停止；配置 configured=true，零提取，未执行重启。源码 manifest `37a0c4de2c73feedee359e06bcc941b406f27b2dd3fe9a5dd77e17b3be47325f`，App `9099a18f5df11bdcf02424f30daa9af6f553adc1b8f4dd3576216e123b3dfab2`；见 `artifacts/verification/feature-002-goal-acceptance-20261017/final-review-fix-001/native-outcome.json`。

随后用户另行授权的一次零模型定位 `opacity-diagnostic-attempt-001/` 已执行并结束：2026-10-18 15:00:10–15:00:45（Asia/Shanghai），driver exit 0 表示完成取证，不表示验收通过。内外 page-panel 都为 page-in pending/currentTime=0、opacity=0；只取消 depth5 内层动画后该层 opacity=1，depth11 外层仍为 0，截图仍空白。document 为 hidden、无焦点，绘制帧等待超时，诊断状态 `animation_intervention_paint_timeout`。主独立查看 raw JSON/截图，比较六组源码、App/Web、固定资产、manifest、普通 State 均稳定，登记/识别/生成/删除调用均为 0，PID 63372/63376 已退出。证据为同目录 `primary-assessment.json`、`diagnostic/result.json`、`diagnostic/diagnostic.png` 及 `wrapper.outcome.json`；首次外层 shell 解析失败发生于 driver/host 调用前，单独保留在 `dispatch-parse-failure.json`，实际 App 仅启动一次。

这次取证证明动画透明度依赖的实际影响，未证明 hidden 来源、完整页面恢复或当前真实识别旅程成功。原生一次预算已耗尽；2026-10-18 当前方案进入同 Task 的有界修复/无模型回归/新包构建，尚无修复后新包原生通过或正式 Validate/Review。不得将任一旧成功包、合成页面或本次 driver exit 0 当作 SCN-UI-22 通过。

后续主使用 `cua.getState()` 只读核对可用桌面会话，原生工具明确返回 Mac 已锁定、检测到物理输入后自动解锁暂停，要求用户手动解锁。已向用户提出解锁问题；当前可继续代码、无模型浏览器测试及构建，不能继续原生界面操作。该观察晚于上述诊断，只证明当前桌面条件不满足，不能反推上次 hidden 的唯一原因；新包的明确运行预算仍需单独确认。

### 正式独立 Validate 重验 — 767b65ce18f75752a55ebb610e52ffc712950ed3

结果 **PASS，主已接受**。独立验证者 `/root/speaker_formal_validation` 结束时 HEAD 为本提交、工作树干净，未修改实现。原始完整 argv/env/cwd/UTC 时间和输出在 `artifacts/verification/feature-002-formal-validation-20261017/go-revalidation-767b65c/`。

2026-10-17 UTC 13:00:40–13:01:12：`gofmt -l cmd internal` 输出为空、`git diff --check`、`go vet ./...` 均 exit 0；`GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local GOCACHE=/private/tmp/yovoice-formal-validation-go-cache go test -json -race ./... -count=1 -timeout=90s` exit 0、31.176 秒，229 启动、217 pass/12 skip/0 fail，15 测试包通过、3 无测试包。此前未启动的 13 项声纹 Workbench 测试全部实际通过；取消测试 0.91 秒，仍要求提交前取消、context.Canceled 和无档案落盘。主独立解析本次 JSON 日志和 HEAD/干净工作树，结果一致。原早退错误未重现，未改 2 秒夹具或补造其根因。

独立比较与 `9c85170…` 的差异仅为该测试同步和两项 `.sdlc-v1` 记录，生产 Go/Web/Swift、fixture、配置、usage、依赖及资产均未变，未新增产品规则。因此复用本验证阶段首轮亲自执行的 58/58 E2E、57/57 browser、Swift 10、脚本 22、Python 4+1、recognition 85.3% 和 Web lint/typecheck/build；各命令/时刻/版本及 0 模型、0 retry/skip/flaky 证据见上一正式记录。此前 PARTIAL 的 SCN-UI-10、13–16、18、20 已由完整新 race 补齐为声明范围 COVERED，其余既有映射保持。

原生再次逐文件核对仅 `.sdlc-v1` 记录与不编译进 service 的 `_test.go` 不同，其余源码、实际 App/receipt/raw native、固定模型/runner/五段样本及三个阶段 State/资产均一致；本次 `input-applicability-output.json` 保存逐项比较。SCN-UI-22 复用 `final-profileid-001/smoke/report.json` 的真实 5 次提取、确切 ID、候选完整 ID、unknown、删除和重启结果，没有再次运行模型。physicalMic/humanTTS、Windows/发行包/真实 TTS/GPU 的未验证边界不变。首轮失败及 12 个既有 skip 原因继续保留，不计为通过。

### Go 取消测试返修 Build 证据

唯一业务路径修改为 `internal/workbench/speakers_test.go` 的 `TestEnrollSpeakerCancelsBeforeCommit`：3 秒内同时观察提交钩子、提前错误与期限，失败/取消释放一次性钩子，取消后 1 秒内读取操作终态，保留 context.Canceled 与无落盘断言。初次 1 秒观察先于夹具终态而失败；改为有依据且仍有限的 3 秒后通过，未改夹具 2 秒配置。原正式失败中的提前错误未再次出现，不补造其具体根因。

`artifacts/verification/feature-002-go-cancel-wait-20261017/check-summary.json` 及原始日志保存全部命令/退出/耗时：定向 race 首次 exit 1、修正后 exit 0（2.580s），Go vet exit 0，完整 race exit 0、217 pass/12 既有 skip，15 测试包通过/3 无测试包，gofmt/diff-check exit 0。主已审查实际 diff；生产 Go/Web/Swift/配置/资产均未改变，原生与独立 Web/Swift 等证据按相同相关输入可复用，但 Go 必须在测试修正后的干净提交上独立重验。这一 Build 通过不是正式 Validate pass。

### 正式独立 Validate — 9c851702a7310de2927db656fe85ba882270e524

结果 **INCONCLUSIVE / 未通过**；独立阶段 `/root/speaker_formal_validation` 只读、结束时业务工作树干净。原始完整 argv/env/cwd、UTC 起止时间、退出码及输出均保存在 `artifacts/verification/feature-002-formal-validation-20261017/` 的 `*.command.json` / 对应日志。本轮没有真实 App/模型/麦克风/TTS/provider/下载。

| 实际检查（2026-10-17 UTC） | 结果与证据 |
|---|---|
| `GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local GOCACHE=/private/tmp/yovoice-formal-validation-go-cache go test -json -race ./... -count=1 -timeout=90s`，12:42:06–12:43:45 | exit 1；181 pass、12 skip、1 未完成，workbench 超时；13 个后续声纹测试未启动。`go-race-local.log` 保存完整超时栈 |
| 同环境 `go vet ./...`，12:40:28–12:40:44 | exit 0；`go-vet.log` |
| `go test -coverprofile=artifacts/verification/feature-002-formal-validation-20261017/recognition.cover ./internal/recognition`，12:41:24–12:41:26 | exit 0，原生摘要 85.3%；`go tool cover -func=…` exit 0、总计 85.6%，两个原始口径分别保留 |
| `make check-recognition`，12:40:28 | exit 0，runner 4、eval 1；已有 venv Python，无安装 |
| `swift test --package-path desktop/macos`，12:42:10–12:42:40 | exit 0，XCTest 10/10；隔离 module caches；额外 Swift Testing 零测试不计覆盖 |
| `python3 -B scripts/desktop/check-speaker-validation-macos.py`，12:41:22–12:41:38 | exit 0，22/22 |
| `web/` 下 `./node_modules/.bin/eslint .`、`./node_modules/.bin/tsc -b`、`./node_modules/.bin/vite build`，12:40:28–12:40:53 | 全部 exit 0 |
| `E2E_TELEMETRY_DISABLED=1 ./node_modules/.bin/e2e run --config e2e.config.ts --workers 1 --retries 0 --strict-cache --output e2e-results/formal-validation-20261017`，12:42:15–12:45:53 | exit 0，58/58，skip/flaky/interrupted=0、每项 1 attempt、modelTokens/maxModelCalls=0；run `01a149e2-676b-7295-b801-054228b182a2`，report/JUnit 归档于证据目录 `web-e2e/` |
| `./node_modules/.bin/playwright test --workers=1 --retries=0 --output <repo>/artifacts/verification/feature-002-formal-validation-20261017/playwright-results-local`，12:42:34–12:44:51 | exit 0，57/57；原配置复用自有 Vite 4173，结束后已回收自有 PID |
| `codesign --verify --deep --strict --verbose=2 artifacts/speaker-validation-macos/worker-20261017-runtime-dto-profileid/yovoice.app`，12:44 | exit 0；未启动 App |

先前沙箱 Go loopback bind / Vite listen EPERM 与 Swift sandbox_apply 拒绝均保留，解除已证实的限制后才执行上表对应检查；解除后的 Go 超时单独归 INCONCLUSIVE。tester-army 首次因本阶段将 output 指到项目根外而 exit 2、0 用例，保留原件，不计为通过；只修正调用输出目录后执行已提交测试。环境为 darwin arm64、Go 1.26.3、Node 25.9.0、Swift 6.3.3、recognition Python 3.12.13，e2e 0.19.0/Web engine 0.14.0/Playwright 1.63.0；更完整环境和每条命令见原始 JSON。

22 场景继续引用既有断言映射：01–09、11–12、17、19、21 在声明的确定性范围 COVERED；10、13–16、18、20 为 PARTIAL，因为 Go 超时使相关后续 Workbench 测试未启动，不能用 Web 通过替代；22 为经独立核对的真实原生复用。独立比较 382 文件仅 `.sdlc-v1` 三个事实/状态文件不同，所有业务文件与原生候选相同；App/receipt/raw native、固定资产、15 项 dist 与五段包内样本解码 hash 一致，三个阶段完整 State/Voices 一致，亲看 main/restart 截图可见。原始核对输出为 `native-applicability-output.json`。

physicalMic/humanTTS 为 NOT_RUN。12 个 Go skip 是既有 10 项音乐/音色库分流及 2 项本平台 GPU 不适用；没有冒充通过。Web 保留 fake Blob/synthetic media 请求失败和测试注入 runtimeFailed 消息的 i18n warning，不能作为真实音频播放/音质证据。完整 Go race 必须通过后才可登记 pass、进入正式 Review。

### 最终工作树候选与原生核对（2026-10-17）

用户已明确授权最终一次 CPU 复验、本轮 58 个文件及验收记录的功能分支 commit/push、独立正式验证和评审。当前完整源码指纹 `7eb2610a22c5fc209682ca4bc7b2321a51e29cc64fcb84d2e11166314182a438`（382 文件），新 App 指纹 `d27b25003056ce7f7f088e40f31cc5602359f8083b7ba88a7e0fb139884ce844`。包为 `artifacts/speaker-validation-macos/worker-20261017-runtime-dto-profileid/yovoice.app`，同级保留 receipt。主亲自核对 receipt/current source、完整包与 strict codesign 均 exit 0，15 个 dist 文件逐路径匹配。

工作树确定性检查：零模型 E2E 58/58（29 场景×双目标、0 retry），browser 57/57，脚本 22/22，ESLint/TypeScript/Vite、Go vet/race 均 exit 0；Go 217 pass、12 skip，recognition 同输入覆盖率 85.3%。原始日志与归档 E2E report/JUnit 在 `artifacts/verification/feature-002-runtime-dto-20261017/`。候选字段和 Runtime JSON 边界的红绿失败历史保留；独立增量审查未发现必须修复项。这些检查不代替提交后正式独立 Validate。

最终原生一次运行：`artifacts/verification/feature-002-goal-acceptance-20261017/final-profileid-001/smoke/report.json`，wrapper exit 0、24.609 秒、未重跑。CPU 一线程、固定 T=.6/M=.05/120s，2 登记、2 独立 known、1 unknown，共 5 次提取；A/B 查询精确对应登记 ID，分数分别为 0.818149/0.838868，耗时 417/628ms；未知查询 0.464850、无选定 ID、519ms。每次两个候选均包含真实完整 ID，主亲看 main/restart 截图确认显示；删除 B 后重启仅 A，导航及档案可见可达。主直接比较 before/main/restart 的 source/App/Web/固定资产/识别 manifest/完整普通 State 一致；Voices 未增加，自有 App/service/runner 已退出。物理 Mic、真实 TTS 听测未运行，分别保留为 NOT_RUN。

22 场景断言映射沿用 `artifacts/verification/feature-002-restart-ui-20261017/worker-current/coverage-22-scenarios-contract-precision.json` 的实际测试名称，当前 58 项用例补齐真实 `profileId` 和 Runtime 数值边界；其旧脚本数、旧原生 pending 状态由本节对应结果替代，不改写旧产物。SCN-UI-22 的当前真实结果为上述最终新包；麦克风和音质边界仍按产品规格分别报告。后续只更新 `.sdlc-v1/**` 的事实索引/状态/验证记录，不改变产品规则、有效方案的行为要求或被测业务文件；正式阶段须独立核对相关业务文件与此候选逐项相同，不能用正文记录替代验证。

2026-10-17 Goal 工作树原生证据：`artifacts/verification/feature-002-goal-acceptance-20261017/attempt-001/smoke/report.json`，wrapper exit 0、27 秒，源码 `41d8d2f5d0b4a60cdec1697286940d47ef2eb6976ca3bddb84e97906783a4535`，App `9c81fa604406442d276ee44a0553d5bc94d86440ce52379354e54432777d1748`。CPU 单线程提取 5 次；两段已知查询分别精确 matched 两个新登记 ID（分数 0.818149/0.838868），未知查询 0.464850、无选定 ID；删除 B 后重启只剩 A。主亲自查看 initialize/main/restart 三张截图，页面完整可见；独立比较原始 before/main/restart 的 source/App/Web/固定资产/识别 manifest 和完整普通 State 一致，Voices 未增加，固定资产与 sherpa 版本符合要求。自有进程已退出，物理 Mic/真实 TTS 听测未执行。此项证明本版本的真实文件识别与重启路径，但截图暴露候选 ID 字段缺失，完整验收仍为未通过；修正后必须重新验证相关 UI，不能直接登记正式 pass。上次旧 App 和预检目录冲突的失败证据全部保留。

## Review

2026-10-17 独立正式评审 `/root/speaker_formal_review`，绑定 validation commit `767b65ce18f75752a55ebb610e52ffc712950ed3`，结论 **changes_requested**。三个未处理 P2：`speaker-page.tsx:169` 迟到刷新覆盖删除后的列表；`:491` 模型切换遗漏原 Inspector 已有的语言兼容规则，普通文本 ja 进入 Index 2 后被 `schema/validate.go:114–123` 拒绝；`build-speaker-validation-macos.sh:118` 的 FFmpeg 路径没有按 shell 字面量转义。主逐项核对当前源代码并接受，实际行为合同见当前 Plan。

有界搜索覆盖声纹页 setProfiles、模型变更/草稿构造，以及新增 build/check/path 脚本。模型切换是已有规则重复实现后产生的实际语义漂移；其余未发现有证据支持的冗余依赖或不必要抽象。评审核对现有 Go/E2E/browser/Swift 原始记录、App/receipt/raw native 和固定资产，查看 main/restart 截图；这些证据在原声明范围有效，但未覆盖上述边界，不能掩盖 P2。返修后须做相关回归、适用 Validate/Review；物理 Mic/真实 TTS 听测等原未验证范围保持。

## Release

未发布。用户已明确授权本轮 FEAT-002 功能分支代码、文档与验收记录 commit/push，以及必要正式验证/评审；本地 App/模型运行仍遵守各次明确预算。主线合并、公证与二进制外部发行不在本次授权范围内。
