import { useEffect, useRef, useState } from 'react';
import { Icon } from '@astryxdesign/core/Icon';
import { Button } from '@astryxdesign/core/Button';
import { ChatComposer, ChatMessage, ChatMessageBubble, ChatMessageList, ChatSystemMessage, ChatToolCalls } from '@astryxdesign/core/Chat';
import { CodeBlock } from '@astryxdesign/core/CodeBlock';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { useTranslator } from '@astryxdesign/core/i18n';
import { useStreamingText } from '@astryxdesign/core/hooks';
import { CircleAlert, Maximize2, Minimize2, Play, Plus, Sparkles, Square, X } from 'lucide-react';
import { call, mediaUrl } from '../../shared/lib/client';
import { formatCallError } from '../../shared/i18n/format';
import { formatTime, type AssistantContext, type AssistantMessage, type AssistantToolCard, type State, type Generation } from '../../shared/workbench';

// 助手面板：浮在工作区右下角的对话卡片，可展开为右侧全高面板。
// 对话消息与实时状态来自后端广播；发送、停止、新对话经 assistant.* RPC。

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
  const messages = state.assistantChat?.messages ?? [];
  const provider = state.aiProviders?.find(p => p.id === state.aiProviderID);
  const configured = !!provider?.model;
  const runtimeMissing = !!state.assistantMissing;
  const [draft, setDraft] = useState('');
  const [chipsDismissed, setChipsDismissed] = useState(false);
  const audioRef = useRef<{ element: HTMLAudioElement; id: string } | null>(null);
  const [playing, setPlaying] = useState<string | null>(null);
  useEffect(() => () => { audioRef.current?.element.pause(); audioRef.current = null; }, []);

  const send = (text: string) => {
    const trimmed = text.trim();
    if (!trimmed || !configured || runtimeMissing) return;
    setDraft('');
    void call('assistant.send', { text: trimmed, context }).catch(() => undefined);
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

  // 生成完成的音频卡片：generate_work 完成且该作品已有版本时，可直接播放或跳回作品。
  const generationCard = (cards: AssistantToolCard[]): Generation | null => {
    const started = cards.some(card => card.tool === 'generate_work' && card.state === 'done');
    if (!started) return null;
    const workID = cards.filter(card => card.tool === 'generate_work').map(card => workIDFromArgs(card.args)).find(Boolean);
    return workID ? state.history.find(item => item.settings.id === workID) ?? null : null;
  };

  const empty = messages.length === 0;
  return <VStack className={`assistant-panel${expanded ? ' expanded' : ''}`} gap={0} data-testid="assistant-panel" data-open="true">
    <HStack className="assistant-header" vAlign="center" gap={2}>
      <Icon icon={Sparkles} size="sm" className="assistant-mark" />
      <b className="grow">{t('@yovoice.assistant.title')}</b>
      {live?.queued ? <small className="assistant-queued">{t('@yovoice.assistant.queued', { count: live.queued })}</small> : null}
      <Button size="sm" variant="ghost" isIconOnly icon={<Plus size={16} />} label={t('@yovoice.assistant.newChat')} isDisabled={running}
        onClick={() => void call('assistant.reset').catch(() => undefined)} />
      <Button size="sm" variant="ghost" isIconOnly icon={expanded ? <Minimize2 size={16} /> : <Maximize2 size={16} />} label={expanded ? t('@yovoice.assistant.collapse') : t('@yovoice.assistant.expand')} onClick={onToggleExpand} />
      <Button size="sm" variant="ghost" isIconOnly icon={<X size={16} />} label={t('@yovoice.assistant.close')} onClick={onClose} />
    </HStack>
    <VStack className="assistant-body" gap={0}>
      {empty && !running ? <VStack gap={3} hAlign="center" className="assistant-empty">
        <p>{t('@yovoice.assistant.empty')}</p>
        <HStack gap={2} wrap="wrap" hAlign="center">{quickActions.map(action => <Button key={action.label} size="sm" variant="secondary" label={action.label} onClick={() => send(action.prompt)} />)}</HStack>
      </VStack> : <ChatMessageList density="compact" gap={1} isStreaming={running}>
        {messages.map(message => message.role === 'divider'
          ? <ChatSystemMessage key={message.id} variant="divider">{t('@yovoice.assistant.newChatDivider')}</ChatSystemMessage>
          : <ChatMessage key={message.id} sender={message.role === 'user' ? 'user' : 'assistant'}
              avatar={message.role === 'user' ? undefined : <Icon icon={Sparkles} size="sm" className="assistant-mark" />}>
            {message.role === 'user'
              ? <ChatMessageBubble>{message.text}</ChatMessageBubble>
              : <AssistantBody message={message} running={false} generation={generationCard(message.tools ?? [])} playing={playing} onPlay={play} openWork={actions.openWork} />}
          </ChatMessage>)}
        {running && live?.turn ? <ChatMessage sender="assistant" avatar={<Icon icon={Sparkles} size="sm" className="assistant-mark" />}>
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
      onSubmit={send} />}
  </VStack>;
}

// workIDFromArgs 从工具卡片的参数摘要里取出作品 ID（generate_work 的 args 形如 {"id":"…"}）。
function workIDFromArgs(args?: string): string {
  if (!args) return '';
  try {
    const id = (JSON.parse(args) as { id?: string }).id;
    return typeof id === 'string' ? id : '';
  } catch { return ''; }
}

// 助手消息正文：流式或定稿的文本 + 工具卡片 + 可播放的生成结果。
function AssistantBody({ message, running, generation, playing, onPlay, openWork }: {
  message: AssistantMessage; running: boolean; generation: Generation | null; playing: string | null;
  onPlay: (item: Generation) => void; openWork: (workId: string) => void;
}) {
  const t = useTranslator();
  const text = useStreamingText(message.text ?? '', running, { speed: 'fast' });
  const cards = message.tools ?? [];
  return <VStack gap={1} className="assistant-reply">
    {text ? <ChatMessageBubble variant="ghost" width="100%">{text}</ChatMessageBubble> : null}
    {cards.length ? <ChatMessageBubble variant="ghost" width="100%"><ChatToolCalls calls={cards.map((card, index) => ({
      name: card.tool,
      key: `${card.tool}-${index}`,
      status: card.state === 'running' ? 'running' : card.state === 'error' ? 'error' : 'complete',
      target: card.summary,
      errorMessage: card.state === 'error' ? (card.summary || card.tool) : undefined,
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
  </VStack>;
}
