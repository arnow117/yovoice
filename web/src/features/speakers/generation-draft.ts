import { createDraft, type Draft, type ModelPackage } from '../../shared/workbench';

const indexTwoLanguages = new Set(['zh', 'en']);
const indexTwoPointFiveLanguages = new Set(['zh', 'en', 'ja', 'es', 'ar']);
const qwenLanguages = new Set(['', 'auto', 'zh', 'en', 'ja', 'ko', 'de', 'fr', 'ru', 'pt', 'es', 'it']);
const modes = new Set<Draft['mode']>(['speaker', 'reference', 'vector', 'text']);
const voiceModes = new Set(['', 'design', 'clone']);
const voxModes = new Set(['', 'design', 'clone', 'continuation']);
const speakerModelOptionFamilies = new Set(['index_tts2', 'qwen3_tts', 'omnivoice', 'voxcpm2']);

function numberInRange(value: unknown, minimum: number, maximum: number) {
  return typeof value === 'number' && Number.isFinite(value) && value >= minimum && value <= maximum;
}

function integerInRange(value: unknown, minimum: number, maximum: number) {
  return Number.isInteger(value) && numberInRange(value, minimum, maximum);
}

function valueOrDefault<T>(value: T, valid: (candidate: T) => boolean, fallback: T) {
  return valid(value) ? value : fallback;
}

function validEmotionVector(value: unknown): value is number[] {
  return Array.isArray(value) && value.length === 8 && value.every(item => numberInRange(item, 0, 1));
}

function supportedSynthesisLanguage(value: unknown, model: ModelPackage | undefined, fallback: string | undefined) {
  if (typeof value !== 'string') return fallback;
  if (model?.family === 'qwen3_tts') return qwenLanguages.has(value) ? value : fallback;
  if (model?.family === 'omnivoice') return value.length <= 32 ? value : fallback;
  return value;
}

// 音乐和编曲作品来自不同 schema，序列化后会保留 mode:""、speed:0 等
// TTS 无效占位。只为独立的临时草稿补齐这些无效值，不能用一般的 falsy
// 替换，因为 emotionStrength=0、intervalSilenceMs=0 等仍是有效用户设置。
function standaloneTTSSettings(source: Draft, defaults: Draft, model: ModelPackage | undefined) {
  const sourceMode = valueOrDefault(source.mode, value => modes.has(value), defaults.mode);
  const sourceInferEmotion = typeof source.inferEmotion === 'boolean' ? source.inferEmotion : defaults.inferEmotion;
  const sourceEmotionText = typeof source.emotionText === 'string' && source.emotionText.length <= 500
    ? source.emotionText
    : defaults.emotionText;
  const emotionText = sourceMode === 'text' && !sourceInferEmotion && sourceEmotionText.trim() === ''
    ? defaults.emotionText
    : sourceEmotionText;
  const indexLanguages = model?.id.startsWith('index-2.5') ? indexTwoPointFiveLanguages : indexTwoLanguages;
  const sourceLanguage = typeof source.language === 'string' && indexLanguages.has(source.language)
    ? source.language
    : defaults.language;
  const modelOptions = speakerModelOptions(source.modelOptions);

  return {
    ...defaults,
    title: typeof source.title === 'string' && source.title.trim() ? source.title : defaults.title,
    text: typeof source.text === 'string' ? source.text : defaults.text,
    speaker: typeof source.speaker === 'string' ? source.speaker : defaults.speaker,
    mode: sourceMode,
    emotionVoiceId: typeof source.emotionVoiceId === 'string' || source.emotionVoiceId === null ? source.emotionVoiceId : defaults.emotionVoiceId,
    emotionText,
    inferEmotion: sourceInferEmotion,
    emotionStrength: valueOrDefault(source.emotionStrength, value => numberInRange(value, 0, 1), defaults.emotionStrength),
    emotions: validEmotionVector(source.emotions) ? source.emotions : defaults.emotions,
    randomEmotion: typeof source.randomEmotion === 'boolean' ? source.randomEmotion : defaults.randomEmotion,
    language: sourceLanguage,
    speed: valueOrDefault(source.speed, value => numberInRange(value, 0.5, 2), defaults.speed),
    temperature: valueOrDefault(source.temperature, value => numberInRange(value, 0.05, 2), defaults.temperature),
    topP: valueOrDefault(source.topP, value => numberInRange(value, 0.01, 1), defaults.topP),
    topK: valueOrDefault(source.topK, value => integerInRange(value, 1, 200), defaults.topK),
    repetitionPenalty: valueOrDefault(source.repetitionPenalty, value => numberInRange(value, 0.1, 20), defaults.repetitionPenalty),
    maxTokens: valueOrDefault(source.maxTokens, value => integerInRange(value, 50, 4000), defaults.maxTokens),
    intervalSilenceMs: valueOrDefault(source.intervalSilenceMs, value => integerInRange(value, 0, 2000), defaults.intervalSilenceMs),
    doSample: typeof source.doSample === 'boolean' ? source.doSample : defaults.doSample,
    numBeams: valueOrDefault(source.numBeams, value => integerInRange(value, 1, 10), defaults.numBeams),
    lengthPenalty: valueOrDefault(source.lengthPenalty, value => numberInRange(value, -2, 2), defaults.lengthPenalty),
    seed: source.seed === null || integerInRange(source.seed, 0, 2147483647) ? source.seed : defaults.seed,
    synthesisLanguage: supportedSynthesisLanguage(source.synthesisLanguage, model, 'auto'),
    omniSpeed: valueOrDefault(source.omniSpeed, value => value === 0 || numberInRange(value, 0.5, 2), defaults.omniSpeed),
    voiceMode: voiceModes.has(source.voiceMode ?? '') ? source.voiceMode : defaults.voiceMode,
    voxMode: voxModes.has(source.voxMode ?? '') ? source.voxMode : defaults.voxMode,
    voiceDescription: typeof source.voiceDescription === 'string' && source.voiceDescription.length <= 500 ? source.voiceDescription : defaults.voiceDescription,
    guidanceScale: valueOrDefault(source.guidanceScale, value => value === 0 || numberInRange(value, 0.5, 5), defaults.guidanceScale),
    inferenceSteps: valueOrDefault(source.inferenceSteps, value => value === 0 || integerInRange(value, 1, 50), defaults.inferenceSteps),
    ...(Object.keys(modelOptions).length ? { modelOptions } : {}),
  };
}

function speakerModelOptions(options: Draft['modelOptions']) {
  return Object.fromEntries(
    Object.entries(options ?? {}).filter(([family]) => speakerModelOptionFamilies.has(family)),
  );
}

// 声纹页面从音乐和编曲作品启动时，必须以独立的、可被 TTS schema 接受的
// 草稿生成；原作品的正文和显式参数仍会保留在这个临时草稿中。
export function speakerGenerationDraft(draft: Draft, installedModels: ModelPackage[]): Draft {
  const selectedModel = installedModels.find(model => model.id === draft.modelId) ?? installedModels[0];
  const isStandaloneGeneration = draft.kind === 'music' || draft.kind === 'score';
  const defaults = createDraft(false, draft.language === 'en' ? 'en' : 'zh-CN', selectedModel?.id ?? draft.modelId);
  const settings = isStandaloneGeneration ? standaloneTTSSettings(draft, defaults, selectedModel) : draft;
  const supportedOptions = speakerModelOptions(settings.modelOptions);
  const synthesisLanguage = supportedSynthesisLanguage(settings.synthesisLanguage, selectedModel, 'auto');

  const {
    kind: _kind,
    score: _score,
    subtitles: _subtitles,
    timeline: _timeline,
    lyrics: _lyrics,
    instrumental: _instrumental,
    ...wireSettings
  } = settings;

  return {
    ...wireSettings,
    id: isStandaloneGeneration ? crypto.randomUUID().replaceAll('-', '') : draft.id,
    kind: 'text',
    modelId: selectedModel?.id ?? draft.modelId,
    voiceId: null,
    referenceText: '',
    synthesisLanguage,
    ...(Object.keys(supportedOptions).length ? { modelOptions: supportedOptions } : {}),
  };
}
