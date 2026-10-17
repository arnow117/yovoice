import { useEffect, useEffectEvent, useRef, useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { DropdownMenu } from '@astryxdesign/core/DropdownMenu';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { SegmentedControl, SegmentedControlItem } from '@astryxdesign/core/SegmentedControl';
import { TextArea } from '@astryxdesign/core/TextArea';
import { useTranslator } from '@astryxdesign/core/i18n';
import { CircleAlert, FileMusic, ListMusic, LoaderCircle, Mic2, Sparkles } from 'lucide-react';
import { call, CallError, coverMelodyFile } from '../../shared/lib/client';
import { formatActivity, formatCallError, formatMelodyIssue } from '../../shared/i18n/format';
import { AIComposer } from '../../shared/ui/ai-composer';
import { formatTime, type Draft, type MelodyChanges, type MelodyInspection, type MelodyResult, type State, type UiLocale } from '../../shared/workbench';
import { ScoreArrangement } from '../score/score-arrangement';

const maxLength = 64 << 10;

// AI 改谱的结果，与 Go 的 workbench.MelodyEdit 一致。
interface MelodyEdit { abc: string; style?: string; summary?: string; changes?: MelodyChanges }
type Notice = { kind: 'truncated' | 'cover' } | { kind: 'edit'; summary?: string; changes?: MelodyChanges };

/** 把小节号压成区间：3、4、5、9 → 3–5、9。 */
export function barRanges(bars: number[], separator: string) {
  const out: string[] = [];
  for (let i = 0; i < bars.length; i++) {
    let j = i;
    while (j + 1 < bars.length && bars[j + 1] === bars[j] + 1) j++;
    out.push(j > i ? `${bars[i]}–${bars[j]}` : String(bars[i]));
    i = j;
  }
  return out.join(separator);
}

// 谱面校验与换算只在 Go 里做一份：编辑稍停后请后端检查，返回概要、问题位置和换算出的轨道。
function useInspection(abc: string) {
  const [inspection, setInspection] = useState<MelodyInspection | null>(null);
  useEffect(() => {
    if (!abc.trim()) { setInspection(null); return; }
    let live = true;
    const timer = setTimeout(() => {
      void call<MelodyInspection>('melody.inspect', { abc }).then(value => { if (live) setInspection(value); }).catch(() => { if (live) setInspection(null); });
    }, 250);
    return () => { live = false; clearTimeout(timer); };
  }, [abc]);
  return inspection;
}

// 旋律谱：YuE2 先按歌词写出旋律与和弦（或从一首歌提取旋律做翻唱），在这里看轨道、改谱面或交给 AI 改，
// 然后按谱生成整首歌。参照 YuE 官方的 ABC 方言：人声行带和弦、伴奏行写配器动机，两行成组。
export function MelodyScore({ draft, state, change, locale, configureAI }: {
  draft: Draft; state: State; change: (patch: Partial<Draft>) => void; locale: UiLocale; configureAI: () => void;
}) {
  const t = useTranslator();
  const abc = draft.abc ?? '';
  const inspection = useInspection(abc);
  const [view, setView] = useState<'tracks' | 'source'>('tracks');
  const [notice, setNotice] = useState<Notice | null>(null);
  const [error, setError] = useState('');
  const [tempo, setTempo] = useState('');
  const [selected, setSelected] = useState<string>();
  const audio = useRef<HTMLInputElement>(null);
  const file = useRef<HTMLInputElement>(null);
  const activity = state.activity;
  const busy = activity?.status === 'running';
  // 出谱与扒谱占用生成通道，进度在这里显示；失败由全局通知说明。
  const working = busy && activity?.projectId === draft.id && activity.task === 'melody' ? activity : null;
  const pending = state.melody?.projectId === draft.id ? state.melody : undefined;
  useEffect(() => { setTempo(inspection?.tempo ? String(inspection.tempo) : ''); }, [inspection?.tempo]);

  const fail = (e: unknown) => setError(formatCallError(t, e));
  const attempt = async (job: () => Promise<unknown>) => { setError(''); try { await job(); } catch (e) { fail(e); } };
  const apply = (melody: MelodyResult) => {
    change({ abc: melody.abc });
    setNotice(melody.truncated ? { kind: 'truncated' } : melody.source === 'cover' ? { kind: 'cover' } : null);
    setView('tracks');
    void call('melody.dismiss', { id: melody.id });
  };
  // 作品里还没有谱时，写好的谱直接填入；已有谱时等用户决定是否替换。
  const settle = useEffectEvent(() => { if (pending && !abc.trim()) apply(pending); });
  useEffect(() => settle(), [pending?.id]);

  const transform = (op: 'stripChords' | 'tempo', value = 0) => attempt(async () => change({ abc: await call<string>('melody.transform', { abc, op, tempo: value }) }));
  const commitTempo = () => { const value = Number(tempo); if (inspection?.tempo && value && value !== inspection.tempo) void transform('tempo', value); else setTempo(inspection?.tempo ? String(inspection.tempo) : ''); };
  const plan = () => attempt(() => call('melody.plan', draft));
  const canPlan = !busy && !!draft.text.trim() && !!draft.lyrics?.trim();
  const applyEdit = (result: unknown) => {
    const edit = result as MelodyEdit;
    if (!edit?.abc) return;
    change({ abc: edit.abc, ...(edit.style ? { text: edit.style.slice(0, 512) } : {}) });
    setNotice({ kind: 'edit', summary: edit.summary, changes: edit.changes });
  };
  const summary = inspection && !inspection.issue ? [inspection.key, `${inspection.tempo} BPM`, inspection.meter?.join('/'), t('@yovoice.score.barCount', { count: inspection.bars ?? 0 }), formatTime(inspection.seconds ?? 0), t(inspection.chords ? '@yovoice.melody.withChords' : '@yovoice.melody.melodyOnly')].filter(Boolean).join(' · ') : '';
  const score = inspection?.score ? { ...inspection.score, tracks: inspection.score.tracks.map(track => ({ ...track, name: t(`@yovoice.melody.track.${track.id}`) })) } : undefined;
  const changes = notice?.kind === 'edit' ? notice.changes : undefined;
  const sep = t('@yovoice.melody.listSeparator');
  const changeText = changes ? [
    changes.vocal?.length ? t('@yovoice.melody.changedVocal', { bars: barRanges(changes.vocal, sep) }) : '',
    changes.chords?.length ? t('@yovoice.melody.changedChords', { bars: barRanges(changes.chords, sep) }) : '',
    changes.ins?.length ? t('@yovoice.melody.changedIns', { bars: barRanges(changes.ins, sep) }) : '',
    changes.tempo ? t('@yovoice.melody.changedTempo') : '', changes.key ? t('@yovoice.melody.changedKey') : '',
    changes.bars[0] !== changes.bars[1] ? t('@yovoice.melody.changedBars', { before: changes.bars[0], after: changes.bars[1] }) : '',
  ].filter(Boolean).join(sep) : '';

  const pickers = <>
    <input ref={audio} hidden type="file" accept="audio/*,.mp3,.wav,.flac,.m4a,.aac,.ogg,.opus" aria-label={t('@yovoice.melody.coverFile')} onChange={event => {
      const picked = event.target.files?.[0]; event.target.value = '';
      if (picked) void attempt(() => coverMelodyFile(draft.id, picked));
    }} />
    <input ref={file} hidden type="file" accept=".abc,.txt,text/plain" aria-label={t('@yovoice.melody.abcFile')} onChange={async event => {
      const picked = event.target.files?.[0]; event.target.value = '';
      if (!picked) return;
      if (picked.size > maxLength) { fail(new CallError('@yovoice.error.melodyInvalid', { code: 'length' })); return; }
      change({ abc: (await picked.text()).replace(/\r\n?/g, '\n') }); setNotice(null); setError('');
    }} />
  </>;
  const progress = working ? <HStack className="melody-progress" gap={2} vAlign="center" role="status">
    <LoaderCircle size={14} className="generation-spinner" aria-hidden="true" />
    <small className="grow">{formatActivity(t, working)}</small>
    <Button size="sm" label={t('@yovoice.action.cancel')} onClick={() => void attempt(() => call('operation.cancel'))} />
  </HStack> : null;

  return <VStack className="music-block melody-score" gap={3}>
    {pickers}
    <HStack vAlign="center" gap={2} wrap="wrap">
      <VStack className="grow" gap={0}>
        <h2>{t('@yovoice.melody.title')}</h2>
        <small className="score-summary">{abc.trim() ? summary || ' ' : t('@yovoice.melody.subtitle')}</small>
      </VStack>
      {abc.trim() ? <>
        <label className="number-field compact" title={t('@yovoice.melody.tempo')}><input type="number" aria-label={t('@yovoice.melody.tempo')} min={30} max={300} step={1} value={tempo} disabled={!inspection?.tempo}
          onChange={event => setTempo(event.target.value)} onBlur={commitTempo} onKeyDown={event => { if (event.key === 'Enter') commitTempo(); }} /><small aria-hidden="true">BPM</small></label>
        <SegmentedControl label={t('@yovoice.melody.view')} size="sm" value={view} onChange={value => setView(value as 'tracks' | 'source')}>
          <SegmentedControlItem value="tracks" label={t('@yovoice.melody.viewTracks')} />
          <SegmentedControlItem value="source" label={t('@yovoice.melody.viewSource')} />
        </SegmentedControl>
        <DropdownMenu hasChevron alignment="end" button={{ size: 'sm', variant: 'ghost', label: t('@yovoice.melody.more') }} items={[
          { id: 'plan', label: t('@yovoice.melody.replan'), isDisabled: !canPlan, onClick: () => void plan() },
          { id: 'cover', label: t('@yovoice.melody.cover'), isDisabled: busy, onClick: () => audio.current?.click() },
          { id: 'strip', label: t('@yovoice.melody.stripChords'), isDisabled: !inspection?.chords, onClick: () => void transform('stripChords') },
          { id: 'import', label: t('@yovoice.melody.import'), onClick: () => file.current?.click() },
          { id: 'copy', label: t('@yovoice.melody.copy'), onClick: () => void attempt(() => navigator.clipboard.writeText(abc)) },
          { id: 'clear', label: t('@yovoice.melody.clear'), onClick: () => { change({ abc: '' }); setNotice(null); } },
        ]} />
      </> : null}
    </HStack>

    {pending && abc.trim() ? <HStack className="melody-notice" data-tone="info" gap={3} vAlign="center" wrap="wrap" role="status">
      <Sparkles size={16} aria-hidden="true" />
      <VStack className="grow" gap={0}><b>{t(pending.source === 'cover' ? '@yovoice.melody.readyCover' : '@yovoice.melody.readyPlan')}</b><small>{t('@yovoice.melody.readyReplace')}</small></VStack>
      <Button size="sm" variant="ghost" label={t('@yovoice.ai.discard')} onClick={() => void call('melody.dismiss', { id: pending.id })} />
      <Button size="sm" variant="primary" label={t('@yovoice.melody.replace')} onClick={() => apply(pending)} />
    </HStack> : null}

    {abc.trim() ? <AIComposer kind="melody" projectId={draft.id} state={state} overwrite configure={configureAI} apply={applyEdit}
      placeholder={t('@yovoice.ai.melodyPlaceholder')} submitLabel={t('@yovoice.ai.melodySubmit')}
      start={brief => call('ai.melody', { projectId: draft.id, brief, abc, style: draft.text, lyrics: draft.lyrics ?? '', locale })} /> : null}

    {progress}
    {error ? <HStack className="melody-notice" data-tone="danger" gap={2} vAlign="start" role="alert"><CircleAlert size={16} aria-hidden="true" /><small className="grow">{error}</small></HStack> : null}

    {!abc.trim() ? working ? null : <VStack className="score-empty melody-empty" gap={3} hAlign="center">
      <p>{t('@yovoice.melody.empty')}</p>
      <HStack gap={2} wrap="wrap" hAlign="center">
        <Button variant="primary" icon={<ListMusic />} label={t('@yovoice.melody.plan')} isDisabled={!canPlan} tooltip={canPlan ? undefined : t('@yovoice.melody.planNeeds')} onClick={() => void plan()} />
        <Button variant="secondary" icon={<Mic2 />} label={t('@yovoice.melody.cover')} isDisabled={busy} onClick={() => audio.current?.click()} />
        <Button variant="ghost" icon={<FileMusic />} label={t('@yovoice.melody.import')} onClick={() => file.current?.click()} />
      </HStack>
      <small>{t('@yovoice.melody.coverHint')}</small>
    </VStack> : <>
      {notice ? <HStack className="melody-notice" data-tone={notice.kind === 'truncated' ? 'warning' : 'info'} gap={2} vAlign="start" role="status">
        <VStack className="grow" gap={0}>
          <small>{notice.kind === 'edit' ? notice.summary || t('@yovoice.melody.edited') : t(`@yovoice.melody.notice.${notice.kind}`)}</small>
          {changeText ? <small className="melody-changes">{t('@yovoice.melody.changed', { changes: changeText })}</small> : null}
        </VStack>
        <Button size="sm" variant="ghost" label={t('@yovoice.action.closeNotice')} onClick={() => setNotice(null)} />
      </HStack> : null}
      {inspection?.issue ? <HStack className="melody-notice" data-tone="danger" gap={2} vAlign="start" role="alert">
        <CircleAlert size={16} aria-hidden="true" />
        <VStack className="grow" gap={0}><b>{t('@yovoice.melody.invalid')}</b><small>{formatMelodyIssue(t, inspection.issue)}</small></VStack>
      </HStack> : null}
      {view === 'tracks' && score ? <ScoreArrangement readOnly score={score} selected={selected} select={setSelected} updateTrack={() => {}} seek={() => {}}
        label={t('@yovoice.melody.tracksLabel', { bars: inspection?.bars ?? 0 })} />
        : <TextArea className="melody-source" label={t('@yovoice.melody.source')} isLabelHidden value={abc} rows={16} hasSpellCheck={false}
          onChange={value => change({ abc: value.slice(0, maxLength) })} />}
    </>}
  </VStack>;
}
