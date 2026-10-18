import catalog from './catalog.json';
import aiPresets from './ai-presets.json';
import { performanceSettings, createDraft, emptyState, type AudioAsset, type Draft, type State, type Voice, type Preferences, type Character, type SynthesisSettings, type AIProvider, type AITask, type AssistantLive, type AssistantSession, type AssistantToolCard, type AssistantToolCardState, type LocalAPIStatus, type LogFilter, type MelodyInspection, type ScoreNote, aiTimeoutDefault } from '../workbench';
import { encodeWav } from './sound';
import { CallError } from './call-error';
import { isUiLocale } from '../i18n/locale';
import { auditedMethods, encodeError, previewExportLogs, previewLog, previewQueryLogs } from './preview-logs';

// 浏览器预览的本地后端：只保存编辑和音频数据，不模拟桌面推理或模型安装。
// 作品状态存于 localStorage（主副本加恢复备份），音频存于 IndexedDB。
const primaryKey = 'voice-workbench-v1';
const backupKey = 'voice-workbench-backup';
const maxUploadBytes = 20 * 1024 * 1024;

// 预览不监听端口：本机接口只记录开关与端口，用于演示设置页。
let localApi: LocalAPIStatus = { enabled: false, port: 8080, token: 'preview'.padEnd(64, '0'), running: false };
let mockPendingPath = '';
// 预览助手的轮次序号，用于让停止后的旧定时器失效；运行中再发的消息与真实后端一样排队。
let assistantTurnSeq = 0;
let assistantQueue: unknown[] = [];

const listeners = new Set<(state: State) => void>();
let stored: State | undefined;

function load(): State {
  let state = emptyState();
  for (const key of [primaryKey, backupKey]) {
    try {
      const value = JSON.parse(localStorage.getItem(key) ?? 'null');
      if (!value || !Array.isArray(value.drafts) || !Array.isArray(value.history) || !Array.isArray(value.voices)) continue;
      state = value; break;
    } catch { /* 主副本损坏时继续读取恢复备份。 */ }
  }
  state.characters ??= []; state.previews = []; delete state.aiTask; delete state.melody; state.assistantLive = undefined;
  // 浏览器预览默认跳过首次启动的开场画面；需要查看时在状态里把 introSeen 设为 false。
  state.preferences = { ...state.preferences, introSeen: state.preferences?.introSeen ?? true };
  if (!isUiLocale(state.preferences?.uiLocale)) {
    state = { ...state, preferences: { ...state.preferences, uiLocale: 'zh-CN' } };
  }
  return state;
}

// 首次使用时才读取本地存储，桌面端不会触碰预览数据。
const preview = () => stored ??= load();

function publish() {
  const previous = localStorage.getItem(primaryKey);
  if (previous) { try { JSON.parse(previous); localStorage.setItem(backupKey, previous); } catch { /* 损坏的主副本不覆盖恢复备份。 */ } }
  localStorage.setItem(primaryKey, JSON.stringify(preview()));
  listeners.forEach(fn => fn(structuredClone(preview())));
}

export function subscribePreview(listener: (state: State) => void) { listeners.add(listener); return () => { listeners.delete(listener); }; }


// 预览的助手会话：与真实后端一致，当前会话由 currentId 指向，消息写入当前会话。
function currentSession(state: State): AssistantSession | undefined {
  return state.assistantSessions?.sessions.find(session => session.id === state.assistantSessions?.currentId);
}

function touchSession(state: State): AssistantSession {
  const sessions = state.assistantSessions ?? { currentId: '', sessions: [] };
  state.assistantSessions = sessions;
  let session = currentSession(state);
  if (!session) {
    session = { id: crypto.randomUUID().replaceAll('-', ''), title: '', messages: [], updatedAt: new Date().toISOString() };
    sessions.sessions.unshift(session);
    sessions.currentId = session.id;
  }
  return session;
}

// 关键操作与桌面端一样记入日志；日志页的查询、导出和界面事件在这里直接处理。
export async function previewCall<T>(method: string, data: unknown): Promise<T> {
  if (method === 'logs.query') return previewQueryLogs((data ?? {}) as LogFilter) as T;
  if (method === 'diag.client') {
    const { event, method: source, detail } = data as { event: string; method?: string; detail?: string };
    previewLog(`client.${event.slice(0, 40)}`, { method: source, detail: detail?.slice(0, 2000) });
    return true as T;
  }
  if (method === 'logs.export') {
    const url = URL.createObjectURL(new Blob([previewExportLogs()], { type: 'application/x-ndjson' }));
    const link = document.createElement('a'); link.href = url; link.download = 'yovoice-preview-logs.jsonl'; link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  if (!auditedMethods.has(method)) return dispatch<T>(method, data);
  const started = performance.now(), input = (data ?? {}) as { id?: unknown; kind?: unknown; model?: unknown };
  const fields = { id: input.id, kind: input.kind, model: input.model };
  try {
    const result = await dispatch<T>(method, data);
    previewLog(method, { elapsed_ms: Math.round(performance.now() - started), ...fields });
    return result;
  } catch (error) {
    previewLog(method, { elapsed_ms: Math.round(performance.now() - started), error: encodeError(error), ...fields });
    throw error;
  }
}

async function dispatch<T>(method: string, data: unknown): Promise<T> {
  const state = preview();
  // 预览默认带上助手运行时；测试可用 yovoice-preview-no-ocho 模拟安装包缺失的情形。
  const assistant = { available: !localStorage.getItem('yovoice-preview-no-ocho') };
  if (method === 'logs.export') return true as T;
  // 预览不能运行本机程序：Codex 显示为未安装，登录需要桌面端。
  if (method === 'ai.codex.status') return {} as T;
  if (method === 'ai.codex.login') throw new CallError('@yovoice.error.previewDesktopOnly');
  if (method === 'state.get') return { state, catalog, engine: { version: '', minimum: '' }, desktop: false, assistant } as T;
  if (method === 'character.save') {
    const character = structuredClone(data as Character);
    if (!character.name.trim() || character.name.length > 120 || character.demoText.length > 2000) throw new CallError('@yovoice.error.characterInvalid');
    if ([character.settings.voiceId, character.settings.emotionVoiceId].some(id => id && !state.voices.some(v => v.id === id))) throw new CallError('@yovoice.error.voiceRequired');
    if ((character.performances?.length ?? 0) > 32 || character.performances?.some((p, i, all) => !/^[a-f0-9]{32}$/i.test(p.id) || !p.name.trim() || p.name.trim().length > 120 || all.some((v, j) => j !== i && (p.id === v.id || p.name.trim().toLowerCase() === v.name.trim().toLowerCase())))) throw new CallError('@yovoice.error.characterInvalid');
    character.performances = character.performances?.map(p => ({ ...p, name: p.name.trim(), settings: performanceSettings(character.settings, p) }));
    if (character.performances?.some(p => [p.settings.voiceId, p.settings.emotionVoiceId].some(id => id && !state.voices.some(v => v.id === id)))) throw new CallError('@yovoice.error.voiceRequired');
    const index = state.characters.findIndex(c => c.id === character.id);
    character.name = character.name.trim(); character.createdAt = index < 0 ? new Date().toISOString() : state.characters[index].createdAt;
    character.updatedAt = new Date().toISOString();
    if (index < 0) state.characters.push(character); else state.characters[index] = character;
    publish(); return character as T;
  }
  if (method === 'character.delete') { state.characters = state.characters.filter(c => c.id !== (data as { id: string }).id); publish(); return true as T; }
  if (method === 'character.discardPreview') return true as T;
  if (method === 'voice.update' || method === 'voice.fromGeneration') {
    const input = data as { id: string; name: string; referenceText: string };
    if (!input.name.trim() || input.name.length > 100 || input.referenceText.length > 2000) throw new CallError('@yovoice.error.characterInvalid');
    if (method === 'voice.update') {
      const voice = state.voices.find(v => v.id === input.id);
      if (!voice) throw new CallError('@yovoice.error.audioMissing');
      voice.name = input.name.trim(); voice.referenceText = input.referenceText;
      publish(); return voice as T;
    }
    const generation = state.history.find(g => g.id === input.id);
    if (!generation) throw new CallError('@yovoice.error.audioMissing');
    const blob = await blobStore(generation.fileName);
    if (!blob) throw new CallError('@yovoice.error.audioBlobMissing');
    const voice = await previewImportVoice(new File([blob], input.name.trim() + '.wav'));
    Object.assign(voice, { referenceText: input.referenceText, source: 'generation', sourceGenerationId: input.id });
    publish(); return voice as T;
  }
  if (method === 'draft.save') {
    const draft = structuredClone(data as Draft); const index = state.drafts.findIndex(d => d.id === draft.id);
    draft.createdAt = index < 0 ? new Date().toISOString() : state.drafts[index]?.createdAt;
    draft.updatedAt = state.drafts[index]?.updatedAt;
    if (index < 0 || JSON.stringify(draft) !== JSON.stringify(state.drafts[index])) draft.updatedAt = new Date().toISOString();
    if (index < 0) state.drafts.unshift(draft); else state.drafts[index] = draft;
    publish(); return true as T;
  }
  if (method === 'generation.lyricTimes') {
    const { id, times } = data as { id: string; times: number[] };
    const item = state.history.find(v => v.id === id);
    if (!item) throw new CallError('@yovoice.error.audioMissing');
    if (times.length > 2000 || times.some((t, i) => !(t >= 0) || t > item.duration + 1 || (i > 0 && t < times[i - 1]))) throw new CallError('@yovoice.error.requestInvalid');
    state.history = state.history.map(v => v.id === id ? { ...v, lyricTimes: times.length ? [...times] : undefined } : v);
    publish(); return true as T;
  }
  if (method === 'media.rename' || method === 'media.delete') {
    const { kind, id, name } = data as { kind: string; id: string; name: string };
    if (!['voices', 'outputs'].includes(kind)) throw new CallError('@yovoice.error.audioKindInvalid');
    if (method === 'media.rename') {
      if (!name?.trim() || name.trim().length > 120) throw new CallError('@yovoice.error.nameLength');
      if (kind === 'voices') state.voices = state.voices.map(v => v.id === id ? { ...v, name: name.trim() } : v);
      else state.history = state.history.map(v => v.id === id ? { ...v, title: name.trim() } : v);
    } else {
      const item = kind === 'voices' ? state.voices.find(v => v.id === id) : state.history.find(v => v.id === id);
      if (kind === 'outputs' && state.drafts.some(d => d.timeline?.tracks.some(t => t.clips.some(c => c.generationId === id)))) throw new CallError('@yovoice.timeline.inUse');
      if (kind === 'voices') {
        const uses = (s: SynthesisSettings) => s.voiceId === id || s.emotionVoiceId === id;
        // 与服务端一致：所有说话人都有独立设置时，作品级设置与演绎快照不可见，不阻止删除，删除时一并清理。
        const baseHidden = (d: Draft) => !!d.subtitles?.speakers.length && d.subtitles.speakers.every(s => s.settings);
        const draftUses = (d: Draft) => (!baseHidden(d) && (uses(d) || (d.performance && uses(d.performance.settings)))) || d.subtitles?.speakers.some(s => s.settings && uses(s.settings)) || d.subtitles?.cues.some(c => c.performance && uses(c.performance.settings));
        const names = [...state.characters.filter(c => uses(c.settings) || c.performances?.some(p => uses(p.settings)) || (c.preview && uses(c.preview.settings))).map(c => c.name), ...state.drafts.filter(draftUses).map(d => d.title)];
        if (names.length) throw new CallError('@yovoice.error.voiceReferenced', { names: names.join(', ') });
        state.voices = state.voices.filter(v => v.id !== id);
        const clear = <T extends SynthesisSettings>(s: T): T => ({ ...s, voiceId: s.voiceId === id ? null : s.voiceId, emotionVoiceId: s.emotionVoiceId === id ? null : s.emotionVoiceId });
        state.drafts = state.drafts.map(d => ({ ...clear(d), performance: d.performance && { ...d.performance, settings: clear(d.performance.settings) } }));
        state.history = state.history.map(g => ({ ...g, settings: { ...g.settings, voiceId: g.settings.voiceId === id ? null : g.settings.voiceId, emotionVoiceId: g.settings.emotionVoiceId === id ? null : g.settings.emotionVoiceId } }));
      } else state.history = state.history.filter(v => v.id !== id);
      if (item && ![...state.voices, ...state.history].some(v => v.fileName === item.fileName)) await blobDelete(item.fileName);
    }
    publish(); return true as T;
  }
  if (method === 'draft.delete') {
    const id = (data as { id: string }).id;
    if (state.activity?.status === 'running' && state.activity.projectId === id) throw new CallError('@yovoice.error.busy');
    const otherDrafts = state.drafts.filter(d => d.id !== id);
    const referencedGenIds = new Set<string>();
    const referencedAssetIds = new Set<string>();
    for (const d of otherDrafts) {
      for (const track of d.timeline?.tracks ?? []) {
        for (const clip of track.clips) {
          if (clip.generationId) referencedGenIds.add(clip.generationId);
          if (clip.assetId) referencedAssetIds.add(clip.assetId);
        }
      }
    }
    const targetDraft = state.drafts.find(d => d.id === id);
    for (const asset of targetDraft?.timeline?.assets ?? []) {
      if (!referencedAssetIds.has(asset.id)) {
        await blobDelete(asset.fileName);
      }
    }
    const removedGens = state.history.filter(g => g.settings.id === id && !referencedGenIds.has(g.id));
    for (const gen of removedGens) {
      if (![...state.voices, ...state.history.filter(g => !removedGens.includes(g))].some(v => v.fileName === gen.fileName)) {
        await blobDelete(gen.fileName);
      }
    }
    state.history = state.history.filter(g => !removedGens.includes(g));
    state.drafts = otherDrafts;
    publish();
    return true as T;
  }
  if (method === 'storage.info') {
    return {
      root: mockPendingPath || '/Users/preview/.yovoice',
      modelsBytes: 1024 * 1024 * 350,
      outputsBytes: 1024 * 1024 * 15,
      voicesBytes: 1024 * 1024 * 5,
      downloadsBytes: 1024 * 1024 * 80,
      totalBytes: 1024 * 1024 * 450,
      pendingPath: mockPendingPath || undefined,
    } as T;
  }
  if (method === 'storage.directory') {
    mockPendingPath = '/Volumes/Data/yovoice-data';
    return true as T;
  }
  if (method === 'storage.directory.open') return true as T;
  if (method === 'storage.relocate') {
    mockPendingPath = (data as { path: string }).path;
    return true as T;
  }
  if (method === 'storage.cleanup') {
    return { cleanedCount: 0, freedBytes: 0 } as T;
  }
  if (method === 'localApi.get' || method === 'localApi.save' || method === 'localApi.resetToken') {
    if (method === 'localApi.save') {
      const { enabled, port } = data as { enabled: boolean; port: number };
      if (!Number.isInteger(port) || port < 1024 || port > 65535) throw new CallError('@yovoice.error.localApiPort');
      localApi = { ...localApi, enabled, port };
    }
    if (method === 'localApi.resetToken') localApi = { ...localApi, token: crypto.randomUUID().replaceAll('-', '').repeat(2) };
    localApi = { ...localApi, running: localApi.enabled, url: localApi.enabled ? `http://127.0.0.1:${localApi.port}` : undefined };
    return structuredClone(localApi) as T;
  }
  if (method === 'preferences.save') { state.preferences = data as Preferences; publish(); return true as T; }
  if (method === 'voice.record') {
    const recording = data as { base64: string; name: string };
    const bytes = Uint8Array.from(atob(recording.base64), c => c.charCodeAt(0));
    return await previewImportVoice(new File([bytes], `${recording.name}.wav`, { type: 'audio/wav' })) as T;
  }
  // AI 服务配置可在预览中编辑（不保存密钥），实际调用模型只在桌面端进行。
  if (method === 'ai.provider.save') {
    const input = data as AIProvider & { key?: string | null };
    const preset = aiPresets.find(p => p.key === input.preset);
    if (!preset) throw new CallError('@yovoice.error.aiProviderInvalid');
    const editable = preset.endpoint !== 'fixed';
    const next: AIProvider = { id: input.id || crypto.randomUUID().replaceAll('-', ''), preset: preset.key, name: input.name?.trim() || preset.name, format: preset.endpoint === 'custom' ? input.format : preset.format, baseURL: editable ? input.baseURL.trim() : preset.baseURL, modelsPath: preset.endpoint === 'custom' ? input.modelsPath : preset.modelsPath, model: input.model?.trim() ?? '' };
    if (!/^https?:\/\/[^/]+/.test(next.baseURL)) throw new CallError('@yovoice.error.aiProviderInvalid');
    const providers = state.aiProviders ?? [];
    const old = providers.find(p => p.id === next.id);
    next.keyMask = input.key == null ? old?.keyMask : input.key.trim() ? '••••••••' : undefined;
    next.models = old?.models;
    state.aiProviders = old ? providers.map(p => p.id === next.id ? next : p) : [...providers, next];
    state.aiProviderID ||= next.id;
    publish(); return next as T;
  }
  if (method === 'ai.provider.delete' || method === 'ai.provider.use') {
    const id = (data as { id: string }).id;
    if (method === 'ai.provider.delete') {
      state.aiProviders = (state.aiProviders ?? []).filter(p => p.id !== id);
      if (state.aiProviderID === id) state.aiProviderID = state.aiProviders[0]?.id ?? '';
    } else {
      // 写作栏切换模型时同时改服务的当前模型。
      const model = (data as { model?: string }).model?.trim();
      state.aiProviderID = id;
      if (model) state.aiProviders = (state.aiProviders ?? []).map(p => p.id === id ? { ...p, model } : p);
    }
    publish(); return true as T;
  }
  // 预览模拟 OrcaRouter 一键授权：不打开外部页面，稍后直接添加使用免费模型的服务并设为当前服务。
  if (method === 'ai.connect') {
    setTimeout(() => {
      const providers = state.aiProviders ?? [];
      const old = providers.find(p => p.preset === 'orcarouter');
      const next: AIProvider = { id: old?.id ?? crypto.randomUUID().replaceAll('-', ''), preset: 'orcarouter', name: old?.name ?? 'OrcaRouter', format: 'chat_completions', baseURL: 'https://api.orcarouter.ai/v1', modelsPath: '/models', model: old?.model || 'orcarouter/free', models: old?.models, keyMask: '••••••••' };
      state.aiProviders = old ? providers.map(p => p.id === next.id ? next : p) : [...providers, next];
      state.aiProviderID = next.id; publish();
    }, 300);
    return { url: '' } as T;
  }
  // 预览模拟 AI 后台任务的生命周期：开始后短暂运行，再以“仅桌面可用”结束，便于检查界面闭环。
  // 旋律谱的完整校验在 Go 中；预览只读出谱头与人声旋律，便于查看导入的谱。
  if (method === 'melody.inspect') return previewInspectMelody((data as { abc: string }).abc) as T;
  if (method === 'melody.transform') {
    const { abc, op, tempo } = data as { abc: string; op: string; tempo: number };
    if (op === 'stripChords') return abc.split('\n').map(line => /^[A-Za-z]:|^%/.test(line) ? line : line.replace(/"[^"]*"/g, '')).join('\n') as T;
    if (op === 'tempo' && tempo >= 30 && tempo <= 300) return abc.replace(/^Q:.*$/m, `Q:1/4=${tempo}`) as T;
    throw new CallError('@yovoice.error.requestInvalid');
  }
  if (method === 'melody.dismiss') {
    if (state.melody?.id === (data as { id: string }).id) { delete state.melody; publish(); }
    return true as T;
  }
  if (method === 'ai.cast' || method === 'ai.melody') {
    const input = method === 'ai.cast' ? { projectId: '', brief: (data as { title: string }).title } : data as { projectId: string; brief: string };
    const provider = state.aiProviders?.find(p => p.id === state.aiProviderID);
    if (!provider?.model) throw new CallError('@yovoice.error.aiNotConfigured');
    if (state.aiTask?.status === 'running') throw new CallError('@yovoice.error.busy');
    const task: AITask = { id: crypto.randomUUID().replaceAll('-', ''), kind: method === 'ai.cast' ? 'cast' : 'melody', projectId: input.projectId, brief: input.brief, status: 'running', stage: 'writing', provider: provider.name, model: provider.model, timeout: state.preferences.aiTimeout || aiTimeoutDefault, startedAt: new Date().toISOString() };
    state.aiTask = task; publish();
    setTimeout(() => {
      if (state.aiTask?.id !== task.id || state.aiTask.status !== 'running') return;
      state.aiTask = { ...state.aiTask, status: 'failed', finishedAt: new Date().toISOString(), errorCode: '@yovoice.error.previewDesktopOnly' }; publish();
    }, 1500);
    return task as T;
  }
  if (method === 'ai.cancel') {
    if (state.aiTask?.status === 'running') { state.aiTask = { ...state.aiTask, status: 'cancelled', finishedAt: new Date().toISOString() }; publish(); }
    return true as T;
  }
  if (method === 'ai.dismiss') {
    if (state.aiTask?.id === (data as { id: string }).id && state.aiTask.status !== 'running') { delete state.aiTask; publish(); }
    return true as T;
  }
  // 预览模拟助手对话：流式文本、工具卡片与界面事件都按真实路径写进状态，编辑器内容与页面随之变化。
  if (method === 'assistant.send') {
    const input = data as { text: string; context?: { page?: string; workId?: string } };
    const provider = state.aiProviders?.find(p => p.id === state.aiProviderID);
    if (!assistant.available) throw new CallError('@yovoice.error.assistantMissing');
    if (!provider?.model) throw new CallError('@yovoice.error.aiNotConfigured');
    if (state.assistantLive?.turn) {
      assistantQueue.push(data);
      state.assistantLive.queued = assistantQueue.length;
      publish();
      return true as T;
    }
    const chat = touchSession(state);
    chat.messages.push({ id: crypto.randomUUID().replaceAll('-', ''), role: 'user', text: input.text, createdAt: new Date().toISOString() });
    if (!chat.title) chat.title = input.text.split('\n')[0].slice(0, 24);
    chat.updatedAt = new Date().toISOString();
    const tools: AssistantToolCard[] = [];
    const live: AssistantLive = state.assistantLive ?? {};
    state.assistantLive = live;
    // 每轮一个序号：停止或新一轮开始后，旧一轮尚未触发的定时器全部失效，不会写进下一轮。
    const seq = ++assistantTurnSeq;
    const alive = () => Boolean(live.turn) && seq === assistantTurnSeq;
    // 模拟流式增量：分两段写入。
    const stream = (at: number, chunk: string) => setTimeout(() => { if (alive() && live.turn) { live.turn = { ...live.turn, text: (live.turn.text ?? '') + chunk }; publish(); } }, at);
    const card = (at: number, tool: string, summary: string, cardState: AssistantToolCardState) => setTimeout(() => {
      if (!alive()) return;
      const existing = tools.findIndex(item => item.tool === tool);
      const next = { tool, summary, state: cardState };
      if (existing >= 0) tools[existing] = next; else tools.push(next);
      publish();
    }, at);
    const timer = (at: number, run: () => void) => setTimeout(() => { if (alive()) run(); }, at);
    live.turn = { text: '', tools, model: provider.model, startedAt: new Date().toISOString() };
    publish();
    const wantsScore = /钢琴|旋律|编曲|配乐|score|piano/i.test(input.text);
    const wantsSong = /写词|写歌|歌词|歌|lyric|song/i.test(input.text);
    let draftID = '';
    if (wantsScore || wantsSong) {
      card(300, 'create_work', wantsScore ? '新建了《示例小步舞曲》' : '新建了《夏夜兜风》', 'running');
      timer(700, () => {
        draftID = crypto.randomUUID().replaceAll('-', '');
        const draft: Draft = wantsScore
          ? { ...createDraft(false, state.preferences.uiLocale, 'musescore-general-sf2'), id: draftID, kind: 'score', title: '示例小步舞曲', score: { tempo: 100, timeSignature: [4, 4], sections: [], tracks: [] } }
          : { ...createDraft(false, state.preferences.uiLocale, 'ace-step-1.5-turbo-bf16'), id: draftID, kind: 'music', title: '夏夜兜风', text: 'city pop, warm synths', lyrics: '[verse]\n晚风吹过海岸线\n[chorus]\n夏夜一路向前' };
        state.drafts = [draft, ...state.drafts];
        live.update = { id: crypto.randomUUID().replaceAll('-', ''), workId: draftID };
        live.nav = { id: crypto.randomUUID().replaceAll('-', ''), workId: draftID };
        publish();
      });
      card(800, 'create_work', wantsScore ? '新建了《示例小步舞曲》' : '新建了《夏夜兜风》', 'done');
      if (wantsScore) {
        card(1100, 'update_work', '正在修改《示例小步舞曲》的乐谱…', 'running');
        timer(1600, () => {
          const target = state.drafts.find(d => d.id === draftID);
          if (!target?.score) return;
          target.score = { tempo: 100, timeSignature: [4, 4], sections: [{ name: 'A', start: 1, end: 4 }], tracks: [{ id: 'p', name: '钢琴', role: 'piano', program: 0, notes: [{ bar: 1, beat: 1, pitch: 60, length: 1, velocity: 80 }, { bar: 1, beat: 3, pitch: 64, length: 1, velocity: 76 }, { bar: 2, beat: 1, pitch: 67, length: 2, velocity: 78 }] }] };
          live.update = { id: crypto.randomUUID().replaceAll('-', ''), workId: draftID };
          publish();
        });
        card(1900, 'update_work', '修改了《示例小步舞曲》的乐谱', 'done');
      }
    } else {
      card(300, 'list_works', `查看了作品列表（${state.drafts.length} 个）`, 'done');
    }
    stream(2200, wantsScore ? '写好了：' : '好的，');
    stream(2600, wantsScore ? '四小节钢琴旋律已经写进《示例小步舞曲》，可以直接渲染试听。' : '我先看了作品列表。想让我做什么？');
    setTimeout(() => {
      if (!alive()) return;
      chat.messages.push({ id: crypto.randomUUID().replaceAll('-', ''), role: 'assistant', text: live.turn?.text ?? '', tools: [...tools], status: 'completed', createdAt: new Date().toISOString() });
      chat.updatedAt = new Date().toISOString();
      live.turn = undefined;
      const next = assistantQueue.shift();
      live.queued = assistantQueue.length;
      publish();
      if (next) void previewCall('assistant.send', next);
    }, 3200);
    return true as T;
  }
  if (method === 'assistant.cancel') {
    const live = state.assistantLive;
    assistantQueue = [];
    if (live?.turn) {
      assistantTurnSeq++;
      const chat = currentSession(state);
      chat?.messages.push({ id: crypto.randomUUID().replaceAll('-', ''), role: 'assistant', text: live.turn.text ?? '', tools: [...(live.turn.tools ?? [])], status: 'cancelled', createdAt: new Date().toISOString() });
      live.turn = undefined;
      live.queued = 0;
      publish();
    }
    return true as T;
  }
  if (method === 'assistant.sessions') {
    return [...(state.assistantSessions?.sessions ?? [])].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)) as T;
  }
  if (method === 'assistant.new') {
    const sessions = state.assistantSessions ?? { currentId: '', sessions: [] };
    const session: AssistantSession = { id: crypto.randomUUID().replaceAll('-', ''), title: '', messages: [], updatedAt: new Date().toISOString() };
    sessions.sessions = [session, ...sessions.sessions];
    sessions.currentId = session.id;
    state.assistantSessions = sessions;
    if (state.assistantLive?.turn) { state.assistantLive.turn = undefined; state.assistantLive.queued = 0; }
    publish();
    return structuredClone(session) as T;
  }
  if (method === 'assistant.open') {
    const id = (data as { id: string }).id;
    if (!state.assistantSessions?.sessions.some(session => session.id === id)) throw new CallError('@yovoice.error.requestInvalid');
    state.assistantSessions.currentId = id;
    publish();
    return true as T;
  }
  if (method === 'assistant.delete') {
    const id = (data as { id: string }).id;
    const sessions = (state.assistantSessions?.sessions ?? []).filter(session => session.id !== id);
    const currentId = state.assistantSessions?.currentId === id ? sessions[0]?.id ?? '' : state.assistantSessions?.currentId ?? '';
    state.assistantSessions = { currentId, sessions };
    publish();
    return true as T;
  }
  throw new CallError('@yovoice.error.previewDesktopOnly');
}

function previewInspectMelody(abc: string): MelodyInspection {
  const header = (field: string) => new RegExp(`^${field}:\\s*(.+)$`, 'm').exec(abc)?.[1].trim() ?? '';
  const tempo = Number(/=(\d+)/.exec(header('Q'))?.[1]), meter = header('M').split('/').map(Number), unit = Number(header('L').split('/')[1]);
  if (!(tempo > 0) || meter.length !== 2 || !(unit > 0)) return { issue: { code: 'header', detail: '' } };
  const lines = abc.split('\n'), notes: ScoreNote[] = [];
  const steps: Record<string, number> = { C: 0, D: 2, E: 4, F: 5, G: 7, A: 9, B: 11 }, beatUnits = unit / meter[1];
  let bars = 0, chords = false, position = 0;
  for (let i = 0; i < lines.length; i++) {
    if (!/^V:\s*Vocal\s*$/.test(lines[i]) || !lines[i + 1]) continue;
    const music = lines[i + 1];
    chords ||= music.includes('"');
    for (const m of music.replace(/"[^"]*"/g, '').matchAll(/(\|)|([_^=]*)([A-Ga-gzZ])([,']*)(\d*)/g)) {
      if (m[1]) { bars++; position = bars * meter[0]; continue; }
      const length = (Number(m[5]) || 1) / beatUnits;
      if (m[3] === 'Z') { bars += (Number(m[5]) || 1) - 1; continue; }
      if (m[3] !== 'z') {
        const accidental = m[2].includes('^') ? 1 : m[2].includes('_') ? -1 : 0;
        const pitch = 60 + steps[m[3].toUpperCase()] + accidental + (m[3] === m[3].toLowerCase() ? 12 : 0) + [...m[4]].reduce((n, c) => n + (c === ',' ? -12 : 12), 0);
        notes.push({ bar: Math.floor(position / meter[0]) + 1, beat: position % meter[0] + 1, length, pitch, velocity: 90 });
      }
      position += length;
    }
  }
  if (!bars) return { issue: { code: 'group', detail: '' } };
  const score = { tempo, timeSignature: [meter[0], meter[1]] as [number, number], tracks: [{ id: 'vocal', name: 'Vocal', role: 'melody' as const, program: 53, notes }] };
  return { tempo, meter: [meter[0], meter[1]], key: header('K'), bars, seconds: bars * meter[0] * 60 / tempo, chords, score };
}

export function assertUploadSize(file: Blob) {
  if (file.size > maxUploadBytes) throw new CallError('@yovoice.error.audioTooLarge');
}

export async function previewImportTimeline(file: File, name: string): Promise<AudioAsset> {
  assertUploadSize(file);
  const context = new AudioContext();
  try {
    const buffer = await context.decodeAudioData(await file.arrayBuffer());
    if (buffer.duration < 0.01 || buffer.duration > 3600) throw new CallError('@yovoice.timeline.importDuration');
    const id = crypto.randomUUID().replaceAll('-', '');
    const asset = { id, name, fileName: `import-${id}.wav`, duration: buffer.duration };
    await blobStore(asset.fileName, encodeWav(buffer));
    return asset;
  } finally { await context.close(); }
}

export async function previewImportVoice(file: File): Promise<Voice> {
  assertUploadSize(file);
  const context = new AudioContext();
  try {
    const decoded = await context.decodeAudioData(await file.arrayBuffer());
    if (decoded.duration < 1 || decoded.duration > 60) throw new CallError('@yovoice.error.audioDuration');
    const wav = encodeWav(decoded);
    const id = crypto.randomUUID().replaceAll('-', '');
    const voice = { id, name: file.name.replace(/\.[^.]+$/, ''), fileName: id + '.wav', duration: decoded.duration };
    await blobStore(voice.fileName, wav); preview().voices.push(voice); publish(); return voice;
  } finally { await context.close(); }
}

export async function previewMediaUrl(file: string): Promise<string> {
  const blob = await blobStore(file); if (!blob) throw new CallError('@yovoice.error.audioBlobMissing'); return URL.createObjectURL(blob);
}

function database(): Promise<IDBDatabase> { return new Promise((resolve, reject) => {
  const request = indexedDB.open('voice-workbench-audio', 1);
  request.onupgradeneeded = () => request.result.createObjectStore('audio');
  request.onsuccess = () => resolve(request.result); request.onerror = () => reject(request.error);
}); }

async function blobStore(key: string, value?: Blob): Promise<Blob | undefined> {
  const db = await database();
  try { return await new Promise((resolve, reject) => {
    const transaction = db.transaction('audio', value ? 'readwrite' : 'readonly');
    const request = value ? transaction.objectStore('audio').put(value, key) : transaction.objectStore('audio').get(key);
    transaction.oncomplete = () => resolve(value ?? request.result); transaction.onerror = () => reject(transaction.error);
  }); } finally { db.close(); }
}

async function blobDelete(key: string): Promise<void> {
  const db = await database();
  try { await new Promise<void>((resolve, reject) => { const tx = db.transaction('audio', 'readwrite'); tx.objectStore('audio').delete(key); tx.oncomplete = () => resolve(); tx.onerror = () => reject(tx.error); }); } finally { db.close(); }
}
