import { useEffect, useRef, useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { ChatComposer, ChatMessage, ChatMessageBubble, ChatMessageList } from '@astryxdesign/core/Chat';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { useTranslator } from '@astryxdesign/core/i18n';
import { ChevronDown, Maximize2, Minimize2, SquarePen, Trash2, X } from 'lucide-react';
import { call, mediaUrl } from '../../shared/lib/client';
import type { AssistantBlock, AssistantContext, State, Generation } from '../../shared/workbench';
import { messageBlocks, turnCards } from './assistant-model';
import { AssistantTurn } from './assistant-turn';

// 助手面板：浮在工作区右下角的对话卡片，放大后仍是浮层，只是更宽更高。
// 对话按会话组织（标题处切换历史），发送、停止、新建、删除经 assistant.* RPC；运行中的一轮按内容块实时渲染。

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
    const active = audioRef.current;
    if (active && active.id === item.id) {
      active.element.pause();
      audioRef.current = null;
      setPlaying(null);
      return;
    }
    active?.element.pause();
    const url = await mediaUrl('outputs', item.fileName);
    const element = new Audio(url);
    audioRef.current = { element, id: item.id };
    setPlaying(item.id);
    element.onended = () => { if (audioRef.current?.id === item.id) { audioRef.current = null; setPlaying(null); } if (url.startsWith('blob:')) URL.revokeObjectURL(url); };
    await element.play().catch(() => setPlaying(null));
  };

  const kindLabel: Record<string, string> = { text: t('@yovoice.project.text'), story: t('@yovoice.project.story'), music: t('@yovoice.project.music'), score: t('@yovoice.project.score') };
  const contextLabel = context.workTitle || kindLabel[context.workKind ?? ''] || '';
  const empty = messages.length === 0 && !running;
  const title = historyOpen ? t('@yovoice.assistant.history') : current?.title || t('@yovoice.assistant.title');
  return <VStack className={`assistant-panel${expanded ? ' expanded' : ''}`} gap={0} data-testid="assistant-panel" data-open="true" role="complementary" aria-label={t('@yovoice.assistant.title')}>
    <HStack className="assistant-header" vAlign="center" gap={1}>
      <Button data-testid="assistant-history-toggle" size="sm" variant="ghost" className="assistant-title" label={title} endContent={<ChevronDown size={14} aria-hidden />}
        aria-expanded={historyOpen} tooltip={t('@yovoice.assistant.history')} onClick={() => { setHistoryOpen(open => !open); setDeleting(null); }} />
      {live?.queued ? <small className="assistant-queued">{t('@yovoice.assistant.queued', { count: live.queued })}</small> : null}
      <Button size="sm" variant="ghost" isIconOnly icon={<SquarePen size={15} />} label={t('@yovoice.assistant.newChat')} isDisabled={running} onClick={newSession} />
      <Button size="sm" variant="ghost" isIconOnly icon={expanded ? <Minimize2 size={15} /> : <Maximize2 size={15} />} label={expanded ? t('@yovoice.assistant.collapse') : t('@yovoice.assistant.expand')} onClick={onToggleExpand} />
      <Button size="sm" variant="ghost" isIconOnly icon={<X size={15} />} label={t('@yovoice.assistant.close')} onClick={onClose} />
    </HStack>
    {historyOpen ? <VStack className="assistant-history" gap={0} data-testid="assistant-history">
      {sessions.length === 0 ? <small className="assistant-history-empty">{t('@yovoice.assistant.historyEmpty')}</small> : null}
      {sessions.map(session => <HStack key={session.id} className={`assistant-history-item${session.id === current?.id ? ' current' : ''}`} vAlign="center" gap={2}>
        <Button size="sm" variant="ghost" className="grow assistant-history-link" label={session.title || t('@yovoice.assistant.newChat')} onClick={() => openSession(session.id)} />
        <small>{relativeTime(t, session.updatedAt)}</small>
        {deleting === session.id
          ? <Button size="sm" variant="primary" label={t('@yovoice.assistant.deleteConfirm')} onClick={() => deleteSession(session.id)} />
          : <Button size="sm" variant="ghost" isIconOnly className="assistant-history-delete" icon={<Trash2 size={14} />} label={t('@yovoice.assistant.deleteChat', { title: session.title || t('@yovoice.assistant.newChat') })} onClick={() => setDeleting(session.id)} />}
      </HStack>)}
    </VStack> : null}
    <VStack className="assistant-body" gap={0} style={historyOpen ? { display: 'none' } : undefined}>
      {empty ? <VStack gap={3} className="assistant-empty">
        <p>{t('@yovoice.assistant.empty')}</p>
        <VStack gap={0} className="assistant-suggestions">{quickActions.map(action => <Button key={action.label} size="sm" variant="ghost" className="assistant-suggestion" label={action.label}
          isDisabled={!configured || runtimeMissing} onClick={() => send(action.prompt)} />)}</VStack>
      </VStack> : <ChatMessageList density="compact" gap={4} align="top" isStreaming={running}>
        {messages.map(message => message.role === 'user'
          ? <ChatMessage key={message.id} sender="user"><ChatMessageBubble>{message.text}</ChatMessageBubble></ChatMessage>
          : <ChatMessage key={message.id} sender="assistant">
            <AssistantTurn blocks={messageBlocks(message)} live={false} message={message} generation={generationCard(messageBlocks(message), state)} playing={playing} onPlay={play} openWork={actions.openWork} />
          </ChatMessage>)}
        {live?.turn ? <ChatMessage key="live" sender="assistant">
          <AssistantTurn blocks={live.turn.blocks ?? []} live generation={null} playing={null} onPlay={() => undefined} openWork={() => undefined} />
        </ChatMessage> : null}
      </ChatMessageList>}
    </VStack>
    {runtimeMissing ? <HStack className="assistant-notice" role="alert" gap={2} vAlign="center">
      <span className="grow">{t('@yovoice.ai.assistantMissing')}</span>
    </HStack> : !configured ? <HStack className="assistant-notice" gap={2} vAlign="center">
      <span className="grow">{t('@yovoice.assistant.setup')}</span>
      <Button size="sm" variant="secondary" label={t('@yovoice.ai.configure')} onClick={actions.openSettings} />
    </HStack> : <ChatComposer className="assistant-composer" density="compact" value={draft} onChange={setDraft}
      placeholder={t('@yovoice.assistant.placeholder')} isDisabled={runtimeMissing}
      isStopShown={running} onStop={() => void call('assistant.cancel').catch(() => undefined)}
      onSubmit={send}
      footerActions={contextLabel ? <Button size="sm" variant="ghost" className={`assistant-context${withContext ? '' : ' off'}`} label={contextLabel}
        aria-pressed={withContext} tooltip={withContext ? t('@yovoice.assistant.contextOn') : t('@yovoice.assistant.contextOff')}
        onClick={() => setWithContext(on => !on)} /> : undefined} />}
  </VStack>;
}

// generationCard 找到 generate_work 已完成且该作品已有版本的结果，供试听。
function generationCard(blocks: AssistantBlock[], state: State): Generation | null {
  const cards = turnCards(blocks).filter(card => card.tool === 'generate_work');
  if (!cards.some(card => card.state === 'done')) return null;
  const workID = cards.map(card => workIDFromArgs(card.args)).find(Boolean);
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
