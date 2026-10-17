import type { Draft, ModelPackage } from '../../shared/workbench';

type ModelSwitchPatch = Pick<Draft, 'language' | 'modelId' | 'speaker' | 'synthesisLanguage' | 'voiceDescription'>;

const indexTwoLanguages = new Set(['zh', 'en']);

// 模型切换同时发生在创作检查器和声纹独立草稿中，必须沿用相同的族内保留规则。
export function modelSwitchPatch(draft: Draft, catalog: ModelPackage[], modelId: string): ModelSwitchPatch {
  const nextModel = catalog.find(model => model.id === modelId);
  const currentModel = catalog.find(model => model.id === draft.modelId);
  const sameFamily = nextModel?.family === currentModel?.family;
  const preservesSpeaker = sameFamily && !modelId.startsWith('kokoro-');
  const supportsCurrentLanguage = modelId.startsWith('index-2.5') || indexTwoLanguages.has(draft.language);

  return {
    modelId,
    speaker: preservesSpeaker ? draft.speaker : '',
    synthesisLanguage: sameFamily ? draft.synthesisLanguage : 'auto',
    voiceDescription: sameFamily ? draft.voiceDescription : '',
    language: supportsCurrentLanguage ? draft.language : 'zh',
  };
}
