import { useRef, useState, type ReactNode } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { SegmentedControl, SegmentedControlItem } from '@astryxdesign/core/SegmentedControl';
import { TextArea } from '@astryxdesign/core/TextArea';
import { Token } from '@astryxdesign/core/Token';
import { useTranslator } from '@astryxdesign/core/i18n';
import { Lightbulb, Palette, TextQuote } from 'lucide-react';
import { isUntitled, isYuE, type Draft, type State, type UiLocale } from '../../shared/workbench';
import { createMusicDraft, lyricSections, splitTags } from './music-draft';
import { MelodyScore } from './melody-score';

// 音乐创作区：让 AI 写风格和歌词走创作助手。
// 仿 Suno 的工作室布局：左侧是创作表单（风格、歌词、YuE2 的旋律谱各一张卡片），右侧是这首歌的版本流。
// 纯音乐与填写歌词是并列的两种模式。
export function MusicEditor({ draft, state, change, locale, feed }: {
  draft: Draft; state: State; change: (patch: Partial<Draft>) => void; locale: UiLocale; feed?: ReactNode;
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
  const lines = (draft.lyrics ?? '').split('\n').filter(line => line.trim() && !/^\[[^\]]*\]$/.test(line.trim())).length;
  const cardHeader = (icon: ReactNode, title: string, meta: string, actions?: ReactNode) => <HStack className="music-card-header" gap={2} vAlign="center" wrap="wrap">
    <span className="music-card-icon" aria-hidden="true">{icon}</span>
    <VStack className="grow" gap={0}><h2>{title}</h2>{meta ? <small>{meta}</small> : null}</VStack>
    {actions}
  </HStack>;
  return <HStack className="music-studio" gap={0} vAlign="stretch">
    <VStack className="music-editor music-form" gap={3}>
      <VStack className="music-card" gap={3}>
        {cardHeader(<Palette />, t('@yovoice.music.style'), tags.length ? t('@yovoice.music.styleCount', { count: tags.length }) : '',
          empty ? <Button size="sm" variant="secondary" icon={<Lightbulb />} label={t('@yovoice.music.example')} onClick={() => { const example = createMusicDraft(locale); change({ text: example.text, lyrics: example.lyrics, synthesisLanguage: example.synthesisLanguage, ...(isUntitled(draft.title) ? { title: example.title } : {}) }); }} /> : null)}
        {/* 风格标签输入区：展示已选Token胶囊，并在行内支持直接打字、回车/逗号追加、退格删除 */}
        <HStack className="music-style-box" gap={1} vAlign="center" wrap="wrap" onClick={() => inputRef.current?.focus()}>
          <input className="music-style-field" aria-label={t('@yovoice.music.style')} value={draft.text} readOnly tabIndex={-1} />
          {tags.map(tag => <Token key={tag} size="sm" label={tag} onRemove={() => toggleTag(tag)} />)}
          <input ref={inputRef} className="music-inline-input grow" aria-label={t('@yovoice.music.addTag')}
            placeholder={tags.length === 0 ? t('@yovoice.music.stylePlaceholder') : t('@yovoice.music.addTagPlaceholder')}
            value={inputVal} onChange={e => setInputVal(e.target.value)} onKeyDown={handleKeyDown} onBlur={handleBlur} onPaste={handlePaste} />
        </HStack>
      </VStack>
      <VStack className="music-card" gap={3}>
        {cardHeader(<TextQuote />, t('@yovoice.music.lyrics'), draft.instrumental ? t('@yovoice.music.instrumental') : lines ? t('@yovoice.music.lyricLines', { count: lines }) : '',
          <SegmentedControl label={t('@yovoice.music.lyricsMode')} size="sm" value={draft.instrumental ? 'instrumental' : 'vocal'} onChange={value => change({ instrumental: value === 'instrumental' })}>
            <SegmentedControlItem value="vocal" label={t('@yovoice.music.vocal')} />
            <SegmentedControlItem value="instrumental" label={t('@yovoice.music.instrumental')} />
          </SegmentedControl>)}
        {draft.instrumental ? <p className="music-instrumental">{t(yue ? '@yovoice.music.yueInstrumentalHint' : '@yovoice.music.instrumentalHint')}</p> : null}
        {!draft.instrumental || yue ? <HStack className="music-sections" gap={1} vAlign="center" wrap="wrap" role="group" aria-label={t('@yovoice.music.insertSection')}>
          {lyricSections.map(section => <button key={section} type="button" className="music-section-chip" onClick={() => insertSection(section)}>[{section}]</button>)}
        </HStack> : null}
        {draft.instrumental && !yue ? null
          : <TextArea ref={lyrics} className="music-lyrics" label={t('@yovoice.music.lyrics')} isLabelHidden value={draft.lyrics ?? ''} maxLength={4000} rows={draft.instrumental ? 5 : 12} hasSpellCheck={false} placeholder={t(draft.instrumental ? '@yovoice.music.structurePlaceholder' : '@yovoice.music.lyricsPlaceholder')} onChange={value => change({ lyrics: value.slice(0, 4000) })} />}
      </VStack>
      {/* YuE2 先写谱再演唱：谱卡片在歌词之后，可选旋律加和弦、只用旋律或不用谱。 */}
      {yue ? <MelodyScore draft={draft} state={state} change={change} /> : null}
    </VStack>
    {feed}
  </HStack>;
}
