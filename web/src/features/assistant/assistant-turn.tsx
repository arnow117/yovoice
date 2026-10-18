import { useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { Collapsible } from '@astryxdesign/core/Collapsible';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { Markdown } from '@astryxdesign/core/Markdown';
import { StatusDot } from '@astryxdesign/core/StatusDot';
import { useTranslator } from '@astryxdesign/core/i18n';
import { useStreamingText } from '@astryxdesign/core/hooks';
import { Check, Copy, Pause, Play } from 'lucide-react';
import { formatCallError } from '../../shared/i18n/format';
import { formatTime, type AssistantBlock, type AssistantMessage, type Generation } from '../../shared/workbench';
import { splitTurn, turnActivity, turnPhase } from './assistant-model';
import { AssistantOrb } from './assistant-avatar';

// 助手的一轮回复：没有气泡与头像，内容即界面。过程（思考、工具、中间说明）收在一行状态里，
// 默认收起、点开看步骤；回答逐字流式渲染为 Markdown；生成结果可直接试听。

export function AssistantTurn({ blocks, live, message, generation, playing, onPlay, openWork }: {
  blocks: AssistantBlock[]; live: boolean; message?: AssistantMessage; generation: Generation | null; playing: string | null;
  onPlay: (item: Generation) => void; openWork: (workId: string) => void;
}) {
  const t = useTranslator();
  const { process, answer } = splitTurn(blocks);
  const text = useStreamingText(answer, live, { speed: 'fast' });
  const working = live && !answer;
  return <VStack className="assistant-turn" gap={2} data-live={live || undefined}>
    {process.length || working ? <ProcessTrace blocks={process} working={working} /> : null}
    {text ? <Markdown className="assistant-answer" density="compact" contentWidth="100%" isStreaming={live}>{text}</Markdown> : null}
    {generation ? <HStack className="assistant-result" gap={2} vAlign="center">
      <Button size="sm" variant="secondary" isIconOnly icon={playing === generation.id ? <Pause size={14} /> : <Play size={14} />}
        label={playing === generation.id ? t('@yovoice.assistant.pause') : t('@yovoice.assistant.play')} onClick={() => onPlay(generation)} />
      <small className="grow">{generation.title} · {formatTime(generation.duration)}</small>
      <Button size="sm" variant="ghost" label={t('@yovoice.assistant.openWork')} onClick={() => openWork(generation.settings.id)} />
    </HStack> : null}
    {message?.status === 'cancelled' ? <small className="assistant-note">{t('@yovoice.assistant.cancelled')}</small> : null}
    {message?.status === 'failed' && message.errorCode ? <small className="assistant-note" data-tone="danger" role="alert">{formatCallError(t, message)}</small> : null}
    {!live && message?.text ? <CopyButton text={message.text} /> : null}
  </VStack>;
}

// 过程：一行状态（运行中带点阵动画），点开是按顺序的步骤；默认收起，运行中也只显示当前这一步。
function ProcessTrace({ blocks, working }: { blocks: AssistantBlock[]; working: boolean }) {
  const t = useTranslator();
  const [open, setOpen] = useState<boolean | null>(null);
  const activity = turnActivity(blocks);
  const phase = turnPhase(activity);
  const tools = blocks.filter(block => block.kind === 'tool').length;
  const failed = blocks.some(block => block.card?.state === 'error');
  const label = working
    ? t(`@yovoice.assistant.phase.${phase}`)
    : tools ? t('@yovoice.assistant.steps', { count: tools }) : t('@yovoice.assistant.thought');
  // 运行中：点阵 + 带流光的阶段文字，工具调用时后面跟一句具体在做什么。
  const detail = working && activity.kind === 'tool' ? activity.card.summary : '';
  const status = <HStack className="assistant-status" gap={2} vAlign="center" role={working ? 'status' : undefined} data-phase={working ? phase : undefined} data-tone={failed && !working ? 'danger' : undefined}>
    {working ? <AssistantOrb phase={phase} size={32} /> : null}
    <span className="assistant-status-label">{label}</span>
    {detail ? <span className="assistant-status-detail">{detail}</span> : null}
  </HStack>;
  if (!blocks.length) return status;
  return <Collapsible className="assistant-process" trigger={status} isOpen={open ?? false} onOpenChange={setOpen} chevronPosition="end">
    <VStack className="assistant-steps" gap={1}>
      {blocks.map((block, index) => block.kind === 'tool' && block.card
        ? <HStack key={index} className="assistant-step" gap={2} vAlign="center">
          <StatusDot variant={block.card.state === 'error' ? 'error' : block.card.state === 'running' ? 'accent' : 'success'} isPulsing={block.card.state === 'running'}
            label={block.card.state === 'error' ? t('@yovoice.assistant.toolFailed') : block.card.state === 'running' ? t('@yovoice.assistant.working') : t('@yovoice.assistant.toolDone')} />
          <small className="grow">{block.card.summary || t('@yovoice.assistant.working')}</small>
        </HStack>
        : <small key={index} className="assistant-step assistant-thought" data-kind={block.kind}>{block.text?.trim()}</small>)}
    </VStack>
  </Collapsible>;
}

// 复制回答：无剪贴板权限的环境（如内嵌页面）退回 execCommand，保证总有效果。
function CopyButton({ text }: { text: string }) {
  const t = useTranslator();
  const [copied, setCopied] = useState(false);
  const copy = async () => {
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
  return <HStack className="assistant-actions" gap={1}>
    <Button size="sm" variant="ghost" isIconOnly icon={copied ? <Check size={13} /> : <Copy size={13} />}
      label={copied ? t('@yovoice.assistant.copied') : t('@yovoice.assistant.copy')} onClick={copy} />
  </HStack>;
}
