// tester-army 的确定性桌面桥接夹具：只测试 UI 协议和迟到结果保护，绝不表示真实模型识别。
(() => {
  const profiles = [];
  const calls = [];
  const listeners = [];
  let nextProfileNumber = 1;
  const controls = {
    cancelResults: [],
    enrollDelayMs: 0,
    enrollPostAcceptErrorCode: null,
    generateDelayMs: 0,
    generateErrorCode: null,
    identification: null,
    identifyDelayMs: 0,
    identifyStatus: null,
    listDelayMs: 0,
    listErrorCode: null,
    runtime: null,
    runtimeConfigured: true,
  };
  const initialLocale = navigator.language.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en';
  let storySourceID = null;
  let noTTSModels = false;
  try {
    storySourceID = localStorage.getItem('yovoice-synthetic-story-source');
    noTTSModels = localStorage.getItem('yovoice-synthetic-no-tts-models') === 'true';
  } catch (_) {
    // 注入脚本还可能在 about:blank 执行；该 origin 没有 localStorage。
  }
  const state = {
    characters: [],
    previews: [],
    drafts: [
      {
        id: '00000000000000000000000000000011',
        kind: 'music',
        title: 'Synthetic music source',
        text: 'warm synthwave',
        lyrics: '[verse]\nSynthetic lyrics',
        instrumental: true,
        synthesisLanguage: 'yue',
        modelId: 'ace-step-1.5-turbo-bf16',
        voiceId: null,
        referenceText: '',
        mode: '',
        emotionVoiceId: null,
        emotionText: '',
        inferEmotion: false,
        emotionStrength: 0,
        emotions: null,
        randomEmotion: false,
        language: '',
        speed: 0,
        temperature: 0,
        topP: 0,
        topK: 0,
        repetitionPenalty: 0,
        maxTokens: 0,
        intervalSilenceMs: 0,
        doSample: false,
        numBeams: 0,
        lengthPenalty: 0,
        seed: null,
        voiceMode: '',
        voxMode: '',
        guidanceScale: 0,
        inferenceSteps: 0,
        modelOptions: {
          ace_step: { seed: 7 },
          index_tts2: { text_chunk_size: 256, text_chunk_mode: 'tag_aware' },
        },
        timeline: { tracks: [] },
      },
      {
        id: '00000000000000000000000000000022',
        kind: 'score',
        title: 'Synthetic score source',
        text: 'score-only text',
        modelId: 'musescore-general-sf2',
        voiceId: null,
        referenceText: '',
        mode: '',
        emotionVoiceId: null,
        emotionText: '',
        inferEmotion: false,
        emotionStrength: 0,
        emotions: null,
        randomEmotion: false,
        language: '',
        speed: 0,
        temperature: 0,
        topP: 0,
        topK: 0,
        repetitionPenalty: 0,
        maxTokens: 0,
        intervalSilenceMs: 0,
        doSample: false,
        numBeams: 0,
        lengthPenalty: 0,
        seed: null,
        voiceMode: '',
        voxMode: '',
        guidanceScale: 0,
        inferenceSteps: 0,
        lyrics: 'score lyrics',
        instrumental: true,
        modelOptions: {
          soundfont: { reverb: 0.3 },
          index_tts2: { text_chunk_size: 384, text_chunk_mode: 'endline' },
        },
        subtitles: { speakers: [], cues: [] },
        timeline: { tracks: [] },
        score: { tempo: 120, timeSignature: [4, 4], tracks: [{ id: 'piano', name: 'Piano', program: 0, notes: [{ bar: 1, beat: 1, pitch: 60, length: 1, velocity: 90 }] }] },
      },
      {
        id: '00000000000000000000000000000033',
        kind: 'text',
        title: 'Synthetic text source',
        text: 'A normal text project.',
        synthesisLanguage: 'en',
        modelId: 'qwen3-tts-base-q8',
        voiceId: null,
        referenceText: '',
        mode: 'text',
        emotionVoiceId: null,
        emotionText: '',
        inferEmotion: false,
        emotionStrength: 0.6,
        emotions: [0, 0, 0, 0, 0, 0, 0, 0.5],
        randomEmotion: false,
        language: 'en',
        speed: 1,
        temperature: 0.8,
        topP: 0.8,
        topK: 30,
        repetitionPenalty: 10,
        maxTokens: 1500,
        intervalSilenceMs: 200,
        doSample: true,
        numBeams: 3,
        lengthPenalty: 0,
        seed: null,
        modelOptions: {
          index_tts2: { text_chunk_size: 512, text_chunk_mode: 'default' },
        },
      },
    ],
    voices: [{ id: '11111111111111111111111111111111', name: 'Bound voice', fileName: 'bound.wav', duration: 3, referenceText: 'reference' }],
    models: noTTSModels ? [] : [
      { id: 'index-2.5-q8', managed: false },
      { id: 'qwen3-tts-base-q8', managed: false },
      { id: 'omnivoice-q8', managed: false },
      { id: 'voxcpm2-q8', managed: false },
    ],
    history: [],
    preferences: { downloadSource: 'modelscope', backend: 'cpu', modelDirectory: null, uiLocale: initialLocale },
    // 这是 fixture 标志，不是宿主路径；页面仅据此显示已就绪状态。
    runtimePath: 'synthetic-runtime',
    runtimeBackend: 'cpu',
    runtimeVersion: 'v1.0.0',
    activity: null,
  };
  if (storySourceID === '00000000000000000000000000000033') {
    const story = state.drafts.find(draft => draft.id === storySourceID);
    if (story) {
      story.kind = 'story';
      story.text = 'Synthetic story narration for the speaker page.';
      story.subtitles = {
        speakers: [{ id: 'narrator', sourceName: 'Narrator' }],
        cues: [{ id: 'cue-a', start: 0, end: 1, text: 'old cue', speakerId: 'narrator' }],
      };
      story.timeline = {
        tracks: [{
          id: 'story-track',
          name: 'Narration',
          muted: false,
          clips: [{
            id: 'story-clip',
            generationId: 'old-story-generation',
            start: 0,
            offset: 0,
            duration: 1,
          }],
        }],
      };
    }
  }
  const catalog = [
    { id: 'index-2.5-q8', name: 'Index TTS 2.5', family: 'index_tts2', version: 'test', precision: 'q8', remotePath: '', size: 0, sha256: 'synthetic' },
    { id: 'qwen3-tts-base-q8', name: 'Qwen3-TTS 1.7B Base', family: 'qwen3_tts', variant: 'base', version: 'test', precision: 'q8', remotePath: '', size: 0, sha256: 'synthetic' },
    { id: 'omnivoice-q8', name: 'OmniVoice', family: 'omnivoice', version: 'test', precision: 'q8', remotePath: '', size: 0, sha256: 'synthetic' },
    { id: 'voxcpm2-q8', name: 'VoxCPM 2', family: 'voxcpm2', version: 'test', precision: 'q8', remotePath: '', size: 0, sha256: 'synthetic' },
  ];
  const bridge = {
    addEventListener(name, listener) {
      if (name === 'message') listeners.push(listener);
    },
    postMessage(message) {
      calls.push(message);
      queueMicrotask(async () => {
        const response = await respond(message.method, message.data ?? {});
        if (!response) return;
        for (const listener of listeners) listener({ data: { id: message.id, ...response } });
      });
    },
  };
  const chrome = window.chrome ?? {};
  chrome.webview = bridge;
  window.chrome = chrome;
  window.__yovoiceSyntheticSpeakerFixture = {
    calls,
    configure(next) {
      Object.assign(controls, next);
      if (Array.isArray(next.cancelResults)) controls.cancelResults = [...next.cancelResults];
    },
    controls,
    profiles,
    state,
  };

  function responseFor(result) {
    return { result };
  }

  function errorFor(code) {
    return { error: { code, params: {} } };
  }

  function delayedResponse(response, delayMs) {
    if (!delayMs) return response;
    return new Promise(resolve => setTimeout(() => resolve(response), delayMs));
  }

  function profileFor(input) {
    const id = `${nextProfileNumber++}`.padStart(32, '0');
    return {
      id,
      name: input.name ?? `Speaker ${nextProfileNumber - 1}`,
      createdAt: '2026-10-12T00:00:00Z',
      ...(input.bindVoiceId ? { bindVoiceId: input.bindVoiceId } : {}),
      modelSha256: 'synthetic-model',
      featureVersion: 'synthetic-version',
      dimension: 256,
    };
  }

  function respond(method, data) {
    if (method === 'state.get') return responseFor({ state, catalog, engine: { version: '', minimum: '' }, desktop: true });
    if (method === 'speaker.runtime') {
      const runtime = controls.runtime ?? {
        configured: controls.runtimeConfigured,
        threshold: 0.6,
        margin: 0.05,
        timeoutSeconds: 120,
        modelSha256: 'synthetic-model',
        featureVersion: 'synthetic-version',
        dimension: 256,
      };
      return responseFor(runtime);
    }
    if (method === 'speaker.list') {
      const response = controls.listErrorCode
        ? errorFor(controls.listErrorCode)
        : responseFor([...profiles]);
      return delayedResponse(response, controls.listDelayMs);
    }
    if (method === 'speaker.enroll') {
      const profile = profileFor(data);
      profiles.push(profile);
      if (controls.enrollPostAcceptErrorCode) {
        return delayedResponse(errorFor(controls.enrollPostAcceptErrorCode), controls.enrollDelayMs);
      }
      return delayedResponse(responseFor(profile), controls.enrollDelayMs);
    }
    if (method === 'speaker.identify') {
      if (controls.identification) {
        return delayedResponse(responseFor(JSON.parse(JSON.stringify(controls.identification))), controls.identifyDelayMs);
      }
      const profile = profiles[0];
      const status = controls.identifyStatus ?? (profile ? 'matched' : 'unknown');
      const candidates = status === 'ambiguous'
        ? profiles.slice(0, 2).map((candidate, index) => ({ profileId: candidate.id, name: candidate.name, score: 0.8 - index * 0.01 }))
        : profile ? [{ profileId: profile.id, name: profile.name, score: 0.92 }] : [];
      const result = {
        status,
        reason: 'synthetic fixture',
        ...(status === 'matched' && profile ? { profileId: profile.id, score: 0.92 } : {}),
        candidates,
        threshold: 0.6,
        margin: 0.05,
        modelSha256: 'synthetic-model',
        featureVersion: 'synthetic-version',
      };
      return delayedResponse(responseFor(result), controls.identifyDelayMs);
    }
    if (method === 'speaker.delete') {
      const index = profiles.findIndex(profile => profile.id === data.id);
      if (index >= 0) profiles.splice(index, 1);
      return responseFor({ id: data.id, deleted: index >= 0 });
    }
    if (method === 'speaker.cancel') {
      const next = controls.cancelResults.shift();
      if (next === 'none') return new Promise(() => {});
      return responseFor({ cancelRequested: next === undefined ? false : next });
    }
    if (method === 'speaker.generate') {
      if (controls.generateErrorCode) return delayedResponse(errorFor(controls.generateErrorCode), controls.generateDelayMs);
      return delayedResponse(responseFor({ id: 'generation-synthetic', title: 'Synthetic speaker generation', fileName: 'synthetic.wav', createdAt: '2026-10-12T00:00:00Z', duration: 1, settings: data.draft }), controls.generateDelayMs);
    }
    return responseFor(true);
  }
})();
