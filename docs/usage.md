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
