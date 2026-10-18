import { useEffect, useRef, useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { ChatComposer, ChatMessage, ChatMessageBubble, ChatMessageList, ChatToolCalls } from '@astryxdesign/core/Chat';
import { CodeBlock } from '@astryxdesign/core/CodeBlock';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { useTranslator } from '@astryxdesign/core/i18n';
import { useStreamingText } from '@astryxdesign/core/hooks';
import { Check, CircleAlert, Copy, History, Maximize2, Minimize2, Play, Plus, Sparkles, Square, Trash2, X } from 'lucide-react';
import { call, mediaUrl } from '../../shared/lib/client';
import { formatCallError } from '../../shared/i18n/format';
import { formatTime, type AssistantContext, type AssistantMessage, type AssistantToolCard, type State, type Generation } from '../../shared/workbench';

// 助手面板：浮在工作区右下角的对话卡片，可展开为右侧全高面板。
// 对话按会话组织（历史抽屉可切换），发送、停止、新建、删除经 assistant.* RPC。

/** 助手要求打开某作品时由应用层执行（面板自己不掌握页面状态）。 */
export interface AssistantPanelActions {
  openWork: (workId: string) => void;
  openSettings: () => void;
}

export interface QuickAction {
  label: string;
  prompt: string;
}

export function AssistantPanel({ state, context, expanded, quickActions, onToggleExpand, onClose, actions }: {
  state: State; context: AssistantContext; expanded: boolean; quickActions: QuickAction[];
  onToggleExpand: () => void; onClose: () => void; actions: AssistantPanelActions;
}) {
  const t = useTranslator();
  const live = state.assistantLive;
  const running = !!live?.turn;
  const sessions = state.assistantSessions?.sessions ?? [];
  const current = sessions.find(session => session.id === state.assistantSessions?.currentId);
  const messages = current?.messages ?? [];
  const provider = state.aiProviders?.find(p => p.id === state.aiProviderID);
  const configured = !!provider?.model;
  const runtimeMissing = !!state.assistantMissing;
  const [draft, setDraft] = useState('');
  const [historyOpen, setHistoryOpen] = useState(false);
  const [deleting, setDeleting] = useState<string | null>(null);
  const [chipsDismissed, setChipsDismissed] = useState(false);
  const [withContext, setWithContext] = useState(true);
  const audioRef = useRef<{ element: HTMLAudioElement; id: string } | null>(null);
  const [playing, setPlaying] = useState<string | null>(null);
  useEffect(() => () => { audioRef.current?.element.pause(); audioRef.current = null; }, []);
  useEffect(() => { if (running) setHistoryOpen(false); }, [running]);

  const send = (text: string) => {
    const trimmed = text.trim();
    if (!trimmed || !configured || runtimeMissing) return;
    setDraft('');
    setHistoryOpen(false);
    void call('assistant.send', { text: trimmed, context: withContext ? context : {} }).catch(() => undefined);
  };

  const newSession = () => {
    if (running) return;
    setHistoryOpen(false);
    void call('assistant.new').catch(() => undefined);
  };
  const openSession = (id: string) => {
    setHistoryOpen(false);
    setDeleting(null);
    void call('assistant.open', { id }).catch(() => undefined);
  };
  const deleteSession = (id: string) => {
    setDeleting(null);
    void call('assistant.delete', { id }).catch(() => undefined);
  };

  const play = async (item: Generation) => {
    const current = audioRef.current;
    if (current && current.id === item.id) {
      current.element.pause();
      audioRef.current = null;
      setPlaying(null);
      return;
    }
    current?.element.pause();
    const url = await mediaUrl('outputs', item.fileName);
    const element = new Audio(url);
    audioRef.current = { element, id: item.id };
    setPlaying(item.id);
    element.onended = () => { if (audioRef.current?.id === item.id) { audioRef.current = null; setPlaying(null); } if (url.startsWith('blob:')) URL.revokeObjectURL(url); };
    await element.play().catch(() => setPlaying(null));
  };

  const kindLabel: Record<string, string> = { text: t('@yovoice.project.text'), story: t('@yovoice.project.story'), music: t('@yovoice.project.music'), score: t('@yovoice.project.score') };
  const contextLabel = [kindLabel[context.workKind ?? ''] ?? '', context.workTitle].filter(Boolean).join(' · ');
  const empty = messages.length === 0;
  return <VStack className={`assistant-panel${expanded ? ' expanded' : ''}`} gap={0} data-testid="assistant-panel" data-open="true">
    <HStack className="assistant-header" vAlign="center" gap={2}>
      <Button data-testid="assistant-history-toggle" size="sm" variant="ghost" isIconOnly icon={<History size={16} />} label={t('@yovoice.assistant.history')} aria-expanded={historyOpen} onClick={() => { setHistoryOpen(open => !open); setDeleting(null); }} />
      <b className="grow assistant-title">{historyOpen ? t('@yovoice.assistant.history') : current?.title || t('@yovoice.assistant.title')}</b>
      {live?.queued ? <small className="assistant-queued">{t('@yovoice.assistant.queued', { count: live.queued })}</small> : null}
      <Button size="sm" variant="ghost" isIconOnly icon={<Plus size={16} />} label={t('@yovoice.assistant.newChat')} isDisabled={running} onClick={newSession} />
      <Button size="sm" variant="ghost" isIconOnly icon={expanded ? <Minimize2 size={16} /> : <Maximize2 size={16} />} label={expanded ? t('@yovoice.assistant.collapse') : t('@yovoice.assistant.expand')} onClick={onToggleExpand} />
      <Button size="sm" variant="ghost" isIconOnly icon={<X size={16} />} label={t('@yovoice.assistant.close')} onClick={onClose} />
    </HStack>
    {historyOpen ? <VStack className="assistant-history" gap={1} data-testid="assistant-history">
      <HStack vAlign="center" gap={2} className="assistant-history-bar">
        <Button size="sm" variant="secondary" icon={<Plus size={14} />} label={t('@yovoice.assistant.newChat')} isDisabled={running} onClick={newSession} />
        <small className="grow">{t('@yovoice.assistant.historyCount', { count: sessions.length })}</small>
        <Button size="sm" variant="ghost" isIconOnly icon={<X size={14} />} label={t('@yovoice.assistant.back')} onClick={() => { setHistoryOpen(false); setDeleting(null); }} />
      </HStack>
      <VStack className="assistant-history-list" gap={1}>
        {sessions.length === 0 ? <small className="assistant-history-empty">{t('@yovoice.assistant.historyEmpty')}</small> : null}
        {sessions.map(session => <HStack key={session.id} className={`assistant-history-item${session.id === current?.id ? ' current' : ''}`} vAlign="center" gap={2}>
          <Button size="sm" variant="ghost" className="grow assistant-history-link" label={session.title || t('@yovoice.assistant.newChat')} onClick={() => openSession(session.id)} />
          <small>{t('@yovoice.assistant.historyMeta', { count: session.messages.length, time: relativeTime(t, session.updatedAt) })}</small>
          {deleting === session.id
            ? <Button size="sm" variant="primary" icon={<Trash2 size={14} />} label={t('@yovoice.assistant.deleteConfirm')} onClick={() => deleteSession(session.id)} />
            : <Button size="sm" variant="ghost" isIconOnly className="assistant-history-delete" icon={<Trash2 size={14} />} label={t('@yovoice.assistant.deleteChat', { title: session.title || t('@yovoice.assistant.newChat') })} onClick={() => setDeleting(session.id)} />}
        </HStack>)}
      </VStack>
    </VStack> : null}
    <VStack className="assistant-body" gap={0} style={historyOpen ? { display: 'none' } : undefined}>
      {empty && !running ? <VStack gap={3} hAlign="center" className="assistant-empty">
        <p>{t('@yovoice.assistant.empty')}</p>
        <HStack gap={2} wrap="wrap" hAlign="center">{quickActions.map(action => <Button key={action.label} size="sm" variant="secondary" label={action.label} onClick={() => send(action.prompt)} />)}</HStack>
      </VStack> : <ChatMessageList density="compact" gap={1} isStreaming={running}>
        {messages.map(message => message.role === 'user'
          ? <ChatMessage key={message.id} sender="user">
            <ChatMessageBubble>{message.text}</ChatMessageBubble>
          </ChatMessage>
          : <ChatMessage key={message.id} sender="assistant" avatar={<span className="assistant-mark" aria-hidden><Sparkles size={15} /></span>}>
            <AssistantBody message={message} running={false} generation={generationCard(message.tools ?? [], state)} playing={playing} onPlay={play} openWork={actions.openWork} />
          </ChatMessage>)}
        {running && live?.turn ? <ChatMessage sender="assistant" avatar={<span className="assistant-mark" aria-hidden><Sparkles size={15} /></span>}>
          <AssistantBody message={{ id: 'live', role: 'assistant', text: live.turn.text ?? '', tools: live.turn.tools, createdAt: live.turn.startedAt }} running generation={null} playing={null} onPlay={() => undefined} openWork={() => undefined} />
        </ChatMessage> : null}
      </ChatMessageList>}
      {!empty && quickActions.length && !chipsDismissed ? <HStack className="assistant-chips" gap={2} vAlign="center" wrap="wrap">
        {quickActions.map(action => <Button key={action.label} size="sm" variant="secondary" label={action.label} isDisabled={running || !configured || runtimeMissing} onClick={() => send(action.prompt)} />)}
        <Button size="sm" variant="ghost" isIconOnly icon={<X size={14} />} label={t('@yovoice.assistant.hideChips')} onClick={() => setChipsDismissed(true)} />
      </HStack> : null}
    </VStack>
    {runtimeMissing ? <HStack className="assistant-notice" role="alert" gap={2} vAlign="center">
      <CircleAlert size={15} aria-hidden /><span className="grow">{t('@yovoice.ai.assistantMissing')}</span>
    </HStack> : !configured ? <HStack className="assistant-notice" gap={2} vAlign="center">
      <CircleAlert size={15} aria-hidden /><span className="grow">{t('@yovoice.assistant.setup')}</span>
      <Button size="sm" variant="secondary" label={t('@yovoice.ai.configure')} onClick={actions.openSettings} />
    </HStack> : <ChatComposer className="assistant-composer" density="compact" value={draft} onChange={setDraft}
      placeholder={t('@yovoice.assistant.placeholder')} isDisabled={runtimeMissing}
      isStopShown={running} onStop={() => void call('assistant.cancel').catch(() => undefined)}
      onSubmit={send}
      footerActions={contextLabel ? <Button size="sm" variant="ghost" className={`assistant-context${withContext ? '' : ' off'}`} label={contextLabel}
        icon={<Sparkles size={13} aria-hidden />} aria-pressed={withContext}
        tooltip={withContext ? t('@yovoice.assistant.contextOn') : t('@yovoice.assistant.contextOff')}
        onClick={() => setWithContext(on => !on)} /> : undefined} />}
  </VStack>;
}

// generationCard 找到 generate_work 已完成且该作品已有版本的结果，供音频卡片播放。
function generationCard(cards: AssistantToolCard[], state: State): Generation | null {
  const started = cards.some(card => card.tool === 'generate_work' && card.state === 'done');
  if (!started) return null;
  const workID = cards.filter(card => card.tool === 'generate_work').map(card => workIDFromArgs(card.args)).find(Boolean);
  return workID ? state.history.find(item => item.settings.id === workID) ?? null : null;
}

// workIDFromArgs 从工具卡片的参数摘要里取出作品 ID（generate_work 的 args 形如 {"id":"…"}）。
function workIDFromArgs(args?: string): string {
  if (!args) return '';
  try {
    const id = (JSON.parse(args) as { id?: string }).id;
    return typeof id === 'string' ? id : '';
  } catch { return ''; }
}

// relativeTime 显示会话的相对更新时间。
function relativeTime(t: ReturnType<typeof useTranslator>, iso: string): string {
  const minutes = Math.max(0, Math.round((Date.now() - Date.parse(iso)) / 60000));
  if (minutes < 1) return t('@yovoice.assistant.justNow');
  if (minutes < 60) return t('@yovoice.assistant.minutesAgo', { count: minutes });
  const hours = Math.round(minutes / 60);
  if (hours < 24) return t('@yovoice.assistant.hoursAgo', { count: hours });
  return new Date(iso).toLocaleDateString();
}

// 助手消息正文：流式或定稿的文本 + 工具卡片 + 复制 + 可播放的生成结果。
function AssistantBody({ message, running, generation, playing, onPlay, openWork }: {
  message: AssistantMessage; running: boolean; generation: Generation | null; playing: string | null;
  onPlay: (item: Generation) => void; openWork: (workId: string) => void;
}) {
  const t = useTranslator();
  const text = useStreamingText(message.text ?? '', running, { speed: 'fast' });
  const [copied, setCopied] = useState(false);
  const cards = message.tools ?? [];
  // 无剪贴板权限的环境（如内嵌页面）退回 execCommand，保证“复制”总有效果。
  const copy = async () => {
    const text = message.text ?? '';
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      const area = document.createElement('textarea');
      area.value = text;
      document.body.appendChild(area);
      area.select();
      document.execCommand('copy');
      area.remove();
    }
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1500);
  };
  return <VStack gap={1} className="assistant-reply">
    {text ? <ChatMessageBubble variant="ghost" width="100%">{text}</ChatMessageBubble> : null}
    {/* 工具卡片只给用户看友好文案，内部工具名不出现；参数留在展开详情里。 */}
    {cards.length ? <ChatMessageBubble variant="ghost" width="100%" className="assistant-tools"><ChatToolCalls calls={cards.map((card, index) => ({
      name: card.summary || t('@yovoice.assistant.working'),
      key: `${card.tool}-${index}`,
      status: card.state === 'running' ? 'running' : card.state === 'error' ? 'error' : 'complete',
      errorMessage: card.state === 'error' ? (card.summary || t('@yovoice.assistant.toolFailed')) : undefined,
      resultDetail: card.args ? <CodeBlock size="sm" language="json" hasLanguageLabel={false} width="100%" code={card.args} /> : undefined,
    }))} /></ChatMessageBubble> : null}
    {generation ? <ChatMessageBubble variant="ghost" width="100%"><HStack gap={2} vAlign="center" wrap="wrap">
      <Button size="sm" variant="secondary" icon={playing === generation.id ? <Square size={14} /> : <Play size={14} />}
        label={playing === generation.id ? t('@yovoice.assistant.pause') : t('@yovoice.assistant.play')} onClick={() => onPlay(generation)} />
      <small className="grow">{generation.title} · {formatTime(generation.duration)}</small>
      <Button size="sm" variant="ghost" label={t('@yovoice.assistant.openWork')} onClick={() => openWork(generation.settings.id)} />
    </HStack></ChatMessageBubble> : null}
    {message.status === 'cancelled' ? <ChatMessageBubble variant="ghost" width="100%"><small>{t('@yovoice.assistant.cancelled')}</small></ChatMessageBubble> : null}
    {message.status === 'failed' && message.errorCode ? <ChatMessageBubble variant="ghost" width="100%"><small role="alert">{formatCallError(t, message)}</small></ChatMessageBubble> : null}
    {!running && message.text ? <HStack className="assistant-actions" gap={1}>
      <Button size="sm" variant="ghost" isIconOnly icon={copied ? <Check size={13} /> : <Copy size={13} />}
        label={copied ? t('@yovoice.assistant.copied') : t('@yovoice.assistant.copy')} onClick={copy} />
    </HStack> : null}
  </VStack>;
}
