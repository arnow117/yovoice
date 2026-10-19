# Usage index / 使用导航

English · 简体中文

This index maps the non-realtime speaker-profile workflows to their product scenarios and authoritative parameter guides. Speaker profiles are private local records, not proof of a person's identity. They store a 256-dimensional embedding outside `state.json`; normal status and voice-library responses do not include it.

本页将非实时声纹档案流程映射到产品场景与参数权威来源。声纹档案是私有本地记录，不证明现实身份；它将 256 维特征保存在 `state.json` 之外，普通状态和声音库响应不会包含该特征。

| ID | Product scenarios | Scenario | Entry and parameter owner | Observable result / 恢复方式 |
| --- | --- | --- | --- | --- |
| USG-SPEAKER-ENROLL | SCN-01, SCN-07, SCN-08, SCN-13–15 | Register one 1–60 second recording, optionally bind an existing voice. | [CLI](cli.md#speaker-profiles) / [CLI 中文](cli_zh.md#声纹档案); [API](api.md#speaker-profiles) / [API 中文](api_zh.md#声纹档案). Local `YOVOICE_SPEAKER_*` configuration is documented in [development](development.md#speaker-recognition-development). | A new public profile JSON is returned. `runtime_not_configured`, invalid audio, capacity and feature-version errors do not create a profile. / 返回新档案；配置、音频、容量或版本错误不写入档案。 |
| USG-SPEAKER-IDENTIFY | SCN-02–09, SCN-14–15 | Compare a recording to saved profiles. | Same CLI/API guides; threshold, margin and timeout are local environment parameters. | `matched`, `unknown`, or `ambiguous`; an identify request never imports the query as a voice. / 返回三种状态之一，查询不会导入声音库。 |
| USG-SPEAKER-GENERATE | SCN-10–11, SCN-14, SCN-16 | Generate through a profile's bound voice. | [CLI](cli.md#generate-through-a-profile) / [中文](cli_zh.md#通过档案生成); [API](api.md#speaker-profile-generation) / [中文](api_zh.md#按档案生成). TTS model parameters remain owned by the existing generation schema. | Returns the normal WAV response or result JSON. Missing binding, deleted voice, incompatible model, or explicit primary voice/reference input must be corrected. / 仍返回既有生成结果；绑定缺失、音色删除、模型不兼容或冲突输入需修正后重试。 |
| USG-SPEAKER-DELETE | SCN-09, SCN-14, SCN-16 | Remove a profile. | `yovoice speakers delete ID` or `DELETE /v1/speakers/{id}`. | `{id,deleted:true}` is idempotent. The saved TTS voice and history remain. / 重复删除也成功；TTS 音色与历史保留。 |
| USG-SPEAKER-VERIFY | SCN-01–16 evidence map | Reproduce the manifest-driven local recognition evidence. | [Development verification](development.md#speaker-recognition-development) / [中文](development_zh.md#声纹识别开发与验证). | The asserted CLI and isolated HTTP regression covers two enrollments, two independent known matches, one unknown non-match, public list results, fixed T=0.6/M=0.05/120s, before/after run fingerprints, and a redacted run summary. Deterministic tests provide the remaining scenario coverage; this sample is not a population-accuracy or anti-spoofing claim. / 带断言的 CLI 与隔离 HTTP 回归覆盖两次登记、两次独立已知匹配、一次未知非匹配、公开列表、固定 T=0.6/M=0.05/120s、运行前后指纹和脱敏运行摘要；其余场景由确定性测试覆盖，不代表人群准确率或防伪能力。 |
| USG-SPEAKER-DESKTOP | SCN-UI-01–22 | Use the original macOS App's **Speaker profiles** page to register, identify, manage and explicitly choose a bound profile for generation. | macOS App navigation → Speaker profiles. The source of truth for recognition assets remains [development](development.md#speaker-recognition-development) / [中文](development_zh.md#声纹识别开发与验证); TTS model and generation parameters remain owned by the existing draft schema. | File, temporary recording, and existing-voice inputs are explicit. Temporary inputs never create a Voice. Matching does not select a generation profile. Refresh after an unknown result, timeout, or unconfirmed cancellation; list/delete remain available when recognition is not configured. / 文件、临时录音和已有声音均需显式选择；临时输入不会创建声音库素材；匹配不会自动选择生成档案。结果未知、超时或取消未确认时先刷新；未配置识别时列表和删除仍可用。 |
| USG-SPEAKER-MAC-VALIDATION | SCN-UI-22 | Build and open the local validation bundle using the original WKWebView host and fixed local assets. / 构建并正常打开原 WKWebView 本地验证包。 | [Local macOS validation / 本地验证步骤](#local-macos-validation--本地验证步骤). Recognition assets and the six `YOVOICE_SPEAKER_*` values remain operator-provided configuration from the development guides. | A/B enrollment, two independent known matches, one unknown result, then delete B and restart with only A retained. The bundle uses private data and does not download TTS assets. System microphone recording and human TTS listening are reported separately. / 双登记、双独立已知匹配、单未知查询，删除 B 后重启仅保留 A；使用独立数据，不下载 TTS，真实录音与听测单列。 |

## Local macOS validation / 本地验证步骤

Use a local validation bundle built by `scripts/desktop/build-speaker-validation-macos.sh`. Keep its original `<app>.receipt.json` sidecar beside it; the receipt binds the build source to the final signed App, host, service and web resources. This is a local development build, without notarization or a binary release. The asset paths and recognition parameters come from the existing development guides and build configuration; the five sample roles and hashes come from `scripts/recognition/eval-manifest.json`.

使用 `scripts/desktop/build-speaker-validation-macos.sh` 生成本地验证包，保留同级原始 `<app>.receipt.json`；它绑定构建源码与最终签名后的 App、宿主、服务和 Web。此包未公证、未发行。资产路径和识别参数来自既有开发指南与构建配置；五段样本的角色及散列由 `scripts/recognition/eval-manifest.json` 提供。

1. Use an unlocked, logged-in macOS desktop. Choose the App path, a fresh private data directory, and writable stdout/stderr log files. These operator-selected paths are `validation_app`, `validation_data`, `validation_stdout`, and `validation_stderr` below; do not use the daily data directory. / 在已解锁并登录的桌面，选择验证 App、全新私有资料目录及可写日志文件；下列四个变量由操作者填写，不使用日常资料目录。
2. Start through LaunchServices with the following command. It waits until normal App exit. No smoke script is loaded. / 用以下命令正常启动；命令等待 App 退出，不加载内嵌冒烟脚本。

```sh
env -u WORKBENCH_SMOKE_SCRIPT /usr/bin/open -W \
  --stdout "$validation_stdout" --stderr "$validation_stderr" \
  --env WORKBENCH_DATA="$validation_data" --env WORKBENCH_DEBUG=1 \
  --env OMP_NUM_THREADS=1 --env OPENBLAS_NUM_THREADS=1 --env MKL_NUM_THREADS=1 \
  "$validation_app"
```

3. In the visible App, open **Speaker profiles** and wait for the ready configuration and saved-list loading to finish. Missing local recognition configuration needs correction and a deliberate restart; the public error and service log use redacted reason categories. / 在真实窗口进入「声纹档案」，等待配置与列表读取完成；配置缺失时修正本地配置后明确重启，公开错误和服务日志只展示脱敏原因。
4. Use the native file picker to enroll A and B, then identify the independent A query, B query and unknown query from the manifest. Record the complete public IDs, candidates, scores and result counts. The first two must match their respective IDs; the unknown query must not select an identity. / 通过原生文件面板登记 A/B，再依次识别独立 A/B 查询和未知查询；记录完整公开 ID、候选、分数与分母，前两者匹配各自 ID，未知查询不选定身份。
5. Confirm deletion of B, quit normally, and repeat the same launch command with the same data directory. The saved list must contain only the original A ID. Quit and check that this run's host, service and model processes have ended; remove only disposable private test features after recording public results. / 确认删除 B，正常退出，再用同一命令和资料目录启动；列表仅保留原 A。退出并核对本轮宿主、服务、模型进程已结束，公开采证后仅清理本轮可丢弃的私有测试特征。

Keep the App/receipt and fixed input versions unchanged across the journey, and compare ordinary State/Voices before and after. Stop at the first failure, preserve logs and the actual window state, and do not automatically repeat a model operation or restart. A failed or unknown operation requires reading the saved list before deciding the next action. Record the whole journey duration; per-query latency remains a separate follow-up for the current functional acceptance, without a latency benchmark claim.

旅程中保持 App/receipt、固定输入版本不变，并比较前后普通 State/Voices。首次失败即停，保留日志和实际窗口状态，不自动重复模型操作或重启；结果失败或未知时先核对已保存列表，再决定下一步。记录整段旅程耗时；本次功能验收的逐条查询时延另行补测，不作延迟基准结论。

`scripts/desktop/smoke-speaker-validation-macos.sh` remains an optional automated harness: it verifies the receipt before launch and requests one visible presentation before checking window/WebView/document visibility for up to 10 seconds. Its embedded script exits the App when finished. A visibility-preflight failure is a stopped automated run, not evidence that the normal App crashed; preserve its diagnostics and do not loop. The normal launch and native UI journey above is the documented manual path.

`scripts/desktop/smoke-speaker-validation-macos.sh` 保留为可选自动检查：启动前核对 receipt，请求一次显示后在最多10秒内检查窗口、WebView和文档可见性，内嵌脚本结束时主动退出 App。显示前置检查失败只说明该次自动检查已停止，不能据此认定正常 App 崩溃；保留诊断，不循环重跑。正常启动及原生界面操作按上述手动步骤执行。
