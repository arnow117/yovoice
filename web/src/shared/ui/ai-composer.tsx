import { useState, type ReactNode } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { StackItem } from '@astryxdesign/core/Stack';
import { TextArea } from '@astryxdesign/core/TextArea';
import { useTranslator } from '@astryxdesign/core/i18n';
import { CircleAlert, PenLine, Send, Settings2 } from 'lucide-react';
import { call } from '../lib/client';
import { Selector } from '../selector';
import { formatCallError } from '../i18n/format';
import type { State } from '../workbench';

// AI 写作栏：一句话描述发送进助手对话，面板自动打开，进度与继续追问都在面板里。
export function AIComposer({ kind, state, placeholder, submitLabel, extra, send, configure }: {
  kind: 'score' | 'lyrics'; state: State; placeholder: string; submitLabel: string; extra?: ReactNode;
  send: (brief: string) => void; configure: () => void;
}) {
  const t = useTranslator();
  const [brief, setBrief] = useState('');
  const [error, setError] = useState('');
  const provider = state.aiProviders?.find(p => p.id === state.aiProviderID);
  const runtimeMissing = !!state.assistantMissing;
  // 与常见 AI 应用一致：写作栏直接在已配置的服务和模型之间切换，按服务分组；选项值为“服务 ID + 模型”。
  const ready = (state.aiProviders ?? []).filter(p => p.model || p.models?.length);
  const choice = (id: string, model: string) => `${id}\n${model}`;
  const modelOptions = ready.map(p => ({ type: 'section' as const, title: p.name, options: [...new Set([p.model, ...(p.models ?? [])].filter(Boolean))].map(model => ({ value: choice(p.id, model), label: model })) }));
  const pick = (value: string) => { const [id, model] = value.split('\n'); void call('ai.provider.use', { id, model }).catch(e => setError(formatCallError(t, e))); };
  const submit = () => {
    const text = brief.trim();
    if (!text || runtimeMissing) return;
    if (!provider?.model) { setError(t('@yovoice.ai.notConfigured')); return; }
    send(text);
    setBrief('');
  };
  if (!provider?.model) return <HStack className="ai-composer" data-status="setup" gap={3} vAlign="center" wrap="wrap">
    <PenLine className="ai-composer-mark" size={16} aria-hidden />
    <small className="grow">{t(`@yovoice.ai.setup.${kind}`)}</small>
    <Button size="sm" variant="secondary" icon={<Settings2 />} label={t('@yovoice.ai.configure')} onClick={configure} />
  </HStack>;
  return <VStack className="ai-composer" data-status="idle" gap={0}>
    <VStack className="ai-composer-body" gap={2}>
      <TextArea className="ai-composer-input" label={t(`@yovoice.ai.brief.${kind}`)} isLabelHidden value={brief} rows={2} maxLength={2000}
        placeholder={placeholder} hasSpellCheck={false} onChange={value => setBrief(value.slice(0, 2000))}
        onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); submit(); } }} />
    </VStack>
    <HStack className="ai-composer-footer" gap={2} vAlign="center" wrap="wrap">
      {runtimeMissing ? <small className="grow ai-composer-via" role="alert"><CircleAlert className="ai-composer-mark" size={14} aria-hidden /> {t('@yovoice.ai.assistantMissing')}</small>
        : <HStack className="grow ai-composer-via" gap={0}><StackItem size="fill"><Selector label={t('@yovoice.ai.model')} isLabelHidden size="sm" variant="ghost" className="ai-composer-model" value={choice(provider.id, provider.model)} options={modelOptions} hasSearch={modelOptions.reduce((n, s) => n + s.options.length, 0) > 12} searchPlaceholder={t('@yovoice.ai.searchModels')}
          renderValue={option => <HStack gap={2} vAlign="center"><PenLine size={14} aria-hidden />{provider.name} · {option.label}</HStack>} onChange={pick} /></StackItem></HStack>}
      {extra}
      <Button size="sm" variant="primary" icon={<Send />} label={submitLabel} isDisabled={!brief.trim() || runtimeMissing} onClick={submit} />
    </HStack>
    {error ? <small className="ai-composer-error" role="alert">{error}</small> : null}
  </VStack>;
}
