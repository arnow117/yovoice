import { useRef, useState, type ReactNode } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { DropdownMenu } from '@astryxdesign/core/DropdownMenu';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { SegmentedControl, SegmentedControlItem } from '@astryxdesign/core/SegmentedControl';
import { TextArea } from '@astryxdesign/core/TextArea';
import { Token } from '@astryxdesign/core/Token';
import { useTranslator } from '@astryxdesign/core/i18n';
import { ArrowRight, Lightbulb } from 'lucide-react';
import { isUntitled, isYuE, type Draft, type State, type UiLocale } from '../../shared/workbench';
import { createMusicDraft, lyricSections, splitTags } from './music-draft';
import { MelodyScore } from './melody-score';

// 音乐创作区：从上到下是“风格 → 歌词”，生成的版本在“版本记录”里查看；让 AI 写风格和歌词走创作助手。
// 纯音乐与填写歌词是并列的两种模式。YuE2 按“风格与歌词 → 旋律谱”两步切换，每步占满编辑区，
// 步骤条显示各步的进度；已有歌曲或谱时可以直接到旋律谱一步提取或导入。
export function MusicEditor({ draft, state, change, locale }: {
  draft: Draft; state: State; change: (patch: Partial<Draft>) => void; locale: UiLocale;
}) {
  const t = useTranslator();
  // YuE2 的纯音乐先出谱，再把人声旋律交给乐器演奏；歌词可留空，或只写段落标记安排结构。
  const yue = isYuE(draft.modelId);
  const lyrics = useRef<HTMLTextAreaElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const [inputVal, setInputVal] = useState('');
  const tags = splitTags(draft.text);

  const toggleTag = (tag: string) => {
    const next = tags.includes(tag) ? tags.filter(item => item !== tag) : [...tags, tag];
    change({ text: next.join(', ').slice(0, 512) });
  };
  const addTag = (raw: string) => {
    const trimmed = raw.trim();
    if (!trimmed) return;
    const newItems = splitTags(trimmed);
    const combined = Array.from(new Set([...tags, ...newItems]));
    change({ text: combined.join(', ').slice(0, 512) });
    setInputVal('');
  };
  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',' || e.key === '，') {
      e.preventDefault();
      addTag(inputVal);
    } else if (e.key === 'Backspace' && !inputVal && tags.length > 0) {
      change({ text: tags.slice(0, -1).join(', ') });
    }
  };
  const handleBlur = () => {
    if (inputVal.trim()) {
      addTag(inputVal);
    }
  };
  const handlePaste = (e: React.ClipboardEvent<HTMLInputElement>) => {
    const text = e.clipboardData.getData('text');
    if (text.includes(',') || text.includes('，') || text.includes('\n')) {
      e.preventDefault();
      addTag(text);
    }
  };
  // 在光标处插入段落标记；标记单独占一行，便于 ACE-Step 识别结构。
  const insertSection = (section: string) => {
    const element = lyrics.current; const value = draft.lyrics ?? '';
    const start = element?.selectionStart ?? value.length, end = element?.selectionEnd ?? value.length;
    const before = value.slice(0, start);
    const tag = `${before && !before.endsWith('\n\n') ? (before.endsWith('\n') ? '\n' : '\n\n') : ''}[${section}]\n`;
    change({ lyrics: (before + tag + value.slice(end)).slice(0, 4000) });
    requestAnimationFrame(() => { element?.focus(); element?.setSelectionRange(start + tag.length, start + tag.length); });
  };
  const empty = !draft.text.trim() && !draft.lyrics?.trim();
  const abc = !!draft.abc?.trim();
  const [step, setStep] = useState<'write' | 'score'>(yue && abc ? 'score' : 'write');
  // 从第一步点“写旋律谱”时进入旋律谱并直接开始出谱。
  const [planRequest, setPlanRequest] = useState(0);
  const activity = state.activity;
  const planning = activity?.status === 'running' && activity.projectId === draft.id && activity.task === 'melody';
  const free = !draft.instrumental && draft.modelOptions?.yue2?.cot === 'off';
  const lines = (draft.lyrics ?? '').split('\n').filter(line => line.trim() && !/^\[[^\]]*\]$/.test(line.trim())).length;
  const writeStatus = empty ? t('@yovoice.music.stepWriteEmpty')
    : draft.instrumental ? t('@yovoice.music.stepWriteInstrumental', { tags: tags.length }) : t('@yovoice.music.stepWriteStatus', { tags: tags.length, lines });
  // 助手或后台写好的谱在打开旋律谱一步时才填入，这里先提示。
  const pending = state.melody?.projectId === draft.id;
  const scoreStatus = planning ? t('@yovoice.music.stepScoreWorking') : pending ? t('@yovoice.music.stepScorePending') : free ? t('@yovoice.music.stepScoreFree') : t(abc ? '@yovoice.music.stepScoreReady' : '@yovoice.music.stepScoreEmpty');
  const stepButton = (value: 'write' | 'score', index: number, title: string, status: string, progress: 'done' | 'todo' | 'busy') =>
    <button type="button" role="tab" className="music-step" aria-selected={step === value} data-state={progress} onClick={() => setStep(value)}>
      <span className="music-step-index" aria-hidden="true">{index}</span>
      <span className="music-step-text"><b>{title}</b><small>{status}</small></span>
    </button>;
  const steps: ReactNode = yue ? <HStack className="music-steps" role="tablist" aria-label={t('@yovoice.music.steps')} gap={0} vAlign="center">
    {stepButton('write', 1, t('@yovoice.music.stepWrite'), writeStatus, empty ? 'todo' : 'done')}
    <span className="music-step-link" data-done={abc || undefined} aria-hidden="true" />
    {stepButton('score', 2, t('@yovoice.melody.title'), scoreStatus, planning ? 'busy' : abc ? 'done' : 'todo')}
  </HStack> : null;
  const canPlan = !planning && !!draft.text.trim() && (draft.instrumental || !!draft.lyrics?.trim());
  const next = () => { setStep('score'); if (!abc && !free && canPlan) setPlanRequest(performance.now()); };
  if (yue && step === 'score') return <VStack className="music-editor" gap={5}>
    {steps}
    <MelodyScore draft={draft} state={state} change={change} planRequest={planRequest} />
  </VStack>;
  return <VStack className="music-editor" gap={5}>
    {steps}
    <VStack className="music-block" gap={3}>
      <HStack className="music-style-header" hAlign="between" vAlign="center" gap={2}>
        <h2>{t('@yovoice.music.style')}</h2>
        {empty ? <Button size="sm" variant="secondary" icon={<Lightbulb />} label={t('@yovoice.music.example')} onClick={() => { const example = createMusicDraft(locale); change({ text: example.text, lyrics: example.lyrics, synthesisLanguage: example.synthesisLanguage, ...(isUntitled(draft.title) ? { title: example.title } : {}) }); }} /> : null}
      </HStack>

      {/* 风格标签输入区：展示已选Token胶囊，并在行内支持直接打字、回车/逗号追加、退格删除 */}
      <HStack
        className="music-style-box"
        gap={1}
        vAlign="center"
        wrap="wrap"
        onClick={() => inputRef.current?.focus()}
      >
        <input
          className="music-style-field"
          aria-label={t('@yovoice.music.style')}
          value={draft.text}
          readOnly
          tabIndex={-1}
        />
        {tags.map(tag => (
          <Token key={tag} size="sm" label={tag} onRemove={() => toggleTag(tag)} />
        ))}
        <input
          ref={inputRef}
          className="music-inline-input grow"
          aria-label={t('@yovoice.music.addTag')}
          placeholder={tags.length === 0 ? t('@yovoice.music.stylePlaceholder') : t('@yovoice.music.addTagPlaceholder')}
          value={inputVal}
          onChange={e => setInputVal(e.target.value)}
          onKeyDown={handleKeyDown}
          onBlur={handleBlur}
          onPaste={handlePaste}
        />
      </HStack>
    </VStack>
    <VStack className="music-block" gap={3}>
      <HStack vAlign="center" gap={2} wrap="wrap">
        <h2 className="grow">{t('@yovoice.music.lyrics')}</h2>
        {!draft.instrumental || yue ? <DropdownMenu hasChevron alignment="end" button={{ size: 'sm', variant: 'ghost', label: t('@yovoice.music.insertSection') }}
          items={lyricSections.map(section => ({ label: `[${section}]`, onClick: () => insertSection(section) }))} /> : null}
        <SegmentedControl label={t('@yovoice.music.lyricsMode')} size="sm" value={draft.instrumental ? 'instrumental' : 'vocal'} onChange={value => change({ instrumental: value === 'instrumental' })}>
          <SegmentedControlItem value="vocal" label={t('@yovoice.music.vocal')} />
          <SegmentedControlItem value="instrumental" label={t('@yovoice.music.instrumental')} />
        </SegmentedControl>
      </HStack>
      {draft.instrumental ? <p className="music-instrumental">{t(yue ? '@yovoice.music.yueInstrumentalHint' : '@yovoice.music.instrumentalHint')}</p> : null}
      {draft.instrumental && !yue ? null
        : <TextArea ref={lyrics} className="music-lyrics" label={t('@yovoice.music.lyrics')} isLabelHidden value={draft.lyrics ?? ''} maxLength={4000} rows={draft.instrumental ? 6 : 14} hasSpellCheck={false} placeholder={t(draft.instrumental ? '@yovoice.music.structurePlaceholder' : '@yovoice.music.lyricsPlaceholder')} onChange={value => change({ lyrics: value.slice(0, 4000) })} />}
    </VStack>
    {yue ? <HStack className="music-next" gap={3} vAlign="center" wrap="wrap">
      <small className="grow">{t(free ? '@yovoice.music.nextFreeHint' : abc ? '@yovoice.music.nextScoreHint' : '@yovoice.music.nextPlanHint')}</small>
      <Button variant={abc || free ? 'secondary' : 'primary'} icon={<ArrowRight />} label={t(abc || free ? '@yovoice.music.nextOpenScore' : '@yovoice.music.nextPlan')}
        isDisabled={!abc && !free && !canPlan} tooltip={!abc && !free && !canPlan ? t('@yovoice.melody.planNeeds') : undefined} onClick={next} />
    </HStack> : null}
  </VStack>;
}
