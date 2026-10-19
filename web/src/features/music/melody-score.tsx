import { useEffect, useEffectEvent, useRef, useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { DropdownMenu } from '@astryxdesign/core/DropdownMenu';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { SegmentedControl, SegmentedControlItem } from '@astryxdesign/core/SegmentedControl';
import { TextArea } from '@astryxdesign/core/TextArea';
import { useTranslator } from '@astryxdesign/core/i18n';
import { CircleAlert, FileMusic, ListMusic, LoaderCircle, Mic2, Play } from 'lucide-react';
import { call, CallError, coverMelodyFile } from '../../shared/lib/client';
import { formatActivity, formatCallError, formatMelodyIssue } from '../../shared/i18n/format';
import { formatTime, type Draft, type MelodyInspection, type MelodyResult, type State, type Track } from '../../shared/workbench';
import { ScoreArrangement } from '../score/score-arrangement';
import { MelodyStaff } from './melody-staff';

const maxLength = 64 << 10;

type Notice = { kind: 'truncated' | 'cover' };

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

// 旋律谱：YuE2 先按歌词写出旋律与和弦（或从一首歌提取旋律做翻唱），在这里看轨道、改谱面或让创作助手改，
// 然后按谱生成整首歌。参照 YuE 官方的 ABC 方言：人声行带和弦、伴奏行写配器动机，两行成组。
export function MelodyScore({ draft, state, change, play, onError }: {
  draft: Draft; state: State; change: (patch: Partial<Draft>) => void; play: (track: Track) => void; onError: (message: string) => void;
}) {
  const t = useTranslator();
  const abc = draft.abc ?? '';
  const inspection = useInspection(abc);
  const [view, setView] = useState<'staff' | 'tracks' | 'source'>('staff');
  const [notice, setNotice] = useState<Notice | null>(null);
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

  // 操作失败走全局的错误提示，不在谱面区域插入提示条。
  const fail = (e: unknown) => onError(formatCallError(t, e));
  const attempt = async (job: () => Promise<unknown>) => { try { await job(); } catch (e) { fail(e); } };
  const apply = (melody: MelodyResult) => {
    change({ abc: melody.abc });
    setNotice(melody.truncated ? { kind: 'truncated' } : melody.source === 'cover' ? { kind: 'cover' } : null);
    setView('tracks');
    void call('melody.dismiss', { id: melody.id });
  };
  // 作品里还没有谱时，写好的谱直接填入；已有谱时等用户决定是否替换。
  const settle = useEffectEvent(() => { if (pending && !abc.trim()) apply(pending); });
  useEffect(() => settle(), [pending?.id]);

  const transform = (op: 'stripChords' | 'tempo' | 'instrumental', value = 0) => attempt(async () => change({ abc: await call<string>('melody.transform', { abc, op, tempo: value }) }));
  const commitTempo = () => { const value = Number(tempo); if (inspection?.tempo && value && value !== inspection.tempo) void transform('tempo', value); else setTempo(inspection?.tempo ? String(inspection.tempo) : ''); };
  const plan = () => attempt(() => call('melody.plan', draft));
  // 试听：用已装的音色库把谱渲染成音频，放进底部播放器，不经过推理。
  const [listening, setListening] = useState(false);
  const listen = async () => {
    setListening(true);
    await attempt(async () => {
      const { fileName } = await call<{ fileName: string }>('melody.listen', { abc });
      play({ id: fileName, name: t('@yovoice.melody.listenName'), fileName, kind: 'outputs', subtitle: draft.title });
    });
    setListening(false);
  };
  const canPlan = !busy && !!draft.text.trim() && (draft.instrumental || !!draft.lyrics?.trim());
  // 谱里还有人声旋律时，菜单里可以把它转给乐器。
  const sung = !!inspection?.score?.tracks.some(track => track.id === 'vocal');
  const summary = inspection && !inspection.issue ? [inspection.key, `${inspection.tempo} BPM`, inspection.meter?.join('/'), t('@yovoice.score.barCount', { count: inspection.bars ?? 0 }), formatTime(inspection.seconds ?? 0), t(inspection.chords ? '@yovoice.melody.withChords' : '@yovoice.melody.melodyOnly')].filter(Boolean).join(' · ') : '';
  const score = inspection?.score ? { ...inspection.score, tracks: inspection.score.tracks.map(track => ({ ...track, name: t(`@yovoice.melody.track.${track.id}`) })) } : undefined;

  const pickers = <>
    <input ref={audio} hidden type="file" accept="audio/*,.mp3,.wav,.flac,.m4a,.aac,.ogg,.opus" aria-label={t('@yovoice.melody.coverFile')} onChange={event => {
      const picked = event.target.files?.[0]; event.target.value = '';
      if (picked) void attempt(() => coverMelodyFile(draft.id, picked));
    }} />
    <input ref={file} hidden type="file" accept=".abc,.txt,text/plain" aria-label={t('@yovoice.melody.abcFile')} onChange={async event => {
      const picked = event.target.files?.[0]; event.target.value = '';
      if (!picked) return;
      if (picked.size > maxLength) { fail(new CallError('@yovoice.error.melodyInvalid', { code: 'length' })); return; }
      change({ abc: (await picked.text()).replace(/\r\n?/g, '\n') }); setNotice(null);
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
      <VStack className="melody-heading" gap={0}>
        <h2>{t('@yovoice.melody.title')}</h2>
        {abc.trim() ? <small className="score-summary" title={summary}>{summary || ' '}</small> : null}
      </VStack>
      {abc.trim() ? <>
        <Button size="sm" variant="secondary" icon={listening ? <LoaderCircle className="generation-spinner" /> : <Play />} label={t(listening ? '@yovoice.melody.listening' : '@yovoice.melody.listen')} isDisabled={listening || !!inspection?.issue} onClick={() => void listen()} />
        <label className="number-field compact" title={t('@yovoice.melody.tempo')}><input type="number" aria-label={t('@yovoice.melody.tempo')} min={30} max={300} step={1} value={tempo} disabled={!inspection?.tempo}
          onChange={event => setTempo(event.target.value)} onBlur={commitTempo} onKeyDown={event => { if (event.key === 'Enter') commitTempo(); }} /><small aria-hidden="true">BPM</small></label>
        <SegmentedControl label={t('@yovoice.melody.view')} size="sm" value={view} onChange={value => setView(value as 'staff' | 'tracks' | 'source')}>
          <SegmentedControlItem value="staff" label={t('@yovoice.melody.viewStaff')} />
          <SegmentedControlItem value="tracks" label={t('@yovoice.melody.viewTracks')} />
          <SegmentedControlItem value="source" label={t('@yovoice.melody.viewSource')} />
        </SegmentedControl>
        <DropdownMenu hasChevron alignment="end" button={{ size: 'sm', variant: 'ghost', label: t('@yovoice.melody.more') }} items={[
          { id: 'plan', label: t('@yovoice.melody.replan'), isDisabled: !canPlan, onClick: () => void plan() },
          { id: 'cover', label: t('@yovoice.melody.cover'), isDisabled: busy, onClick: () => audio.current?.click() },
          { id: 'strip', label: t('@yovoice.melody.stripChords'), isDisabled: !inspection?.chords, onClick: () => void transform('stripChords') },
          { id: 'instrumental', label: t('@yovoice.melody.toInstrumental'), isDisabled: !sung, onClick: () => void transform('instrumental') },
          { id: 'import', label: t('@yovoice.melody.import'), onClick: () => file.current?.click() },
          { id: 'copy', label: t('@yovoice.melody.copy'), onClick: () => void attempt(() => navigator.clipboard.writeText(abc)) },
          { id: 'clear', label: t('@yovoice.melody.clear'), onClick: () => { change({ abc: '' }); setNotice(null); } },
        ]} />
      </> : null}
    </HStack>

    {pending && abc.trim() ? <HStack className="melody-notice" data-tone="info" gap={3} vAlign="center" wrap="wrap" role="status">
      <VStack className="grow" gap={0}><b>{t(pending.source === 'cover' ? '@yovoice.melody.readyCover' : '@yovoice.melody.readyPlan')}</b><small>{t('@yovoice.melody.readyReplace')}</small></VStack>
      <Button size="sm" variant="ghost" label={t('@yovoice.ai.discard')} onClick={() => void call('melody.dismiss', { id: pending.id })} />
      <Button size="sm" variant="primary" label={t('@yovoice.melody.replace')} onClick={() => apply(pending)} />
    </HStack> : null}

    {progress}

    {/* 还没有谱时只留三个来源按钮，不再放说明文字。 */}
    {!abc.trim() ? working ? null : <HStack className="melody-empty" gap={2} wrap="wrap" vAlign="center">
      <Button variant="primary" icon={<ListMusic />} label={t('@yovoice.melody.plan')} isDisabled={!canPlan} tooltip={canPlan ? undefined : t('@yovoice.melody.planNeeds')} onClick={() => void plan()} />
      <Button variant="secondary" icon={<Mic2 />} label={t('@yovoice.melody.cover')} isDisabled={busy} tooltip={t('@yovoice.melody.coverHint')} onClick={() => audio.current?.click()} />
      <Button variant="ghost" icon={<FileMusic />} label={t('@yovoice.melody.import')} onClick={() => file.current?.click()} />
    </HStack> : <>
      {notice ? <HStack className="melody-notice" data-tone={notice.kind === 'truncated' ? 'warning' : 'info'} gap={2} vAlign="start" role="status">
        <VStack className="grow" gap={0}>
          <small>{t(`@yovoice.melody.notice.${notice.kind}`)}</small>
        </VStack>
        <Button size="sm" variant="ghost" label={t('@yovoice.action.closeNotice')} onClick={() => setNotice(null)} />
      </HStack> : null}
      {inspection?.issue ? <HStack className="melody-notice" data-tone="danger" gap={2} vAlign="start" role="alert">
        <CircleAlert size={16} aria-hidden="true" />
        <VStack className="grow" gap={0}><b>{t('@yovoice.melody.invalid')}</b><small>{formatMelodyIssue(t, inspection.issue)}</small></VStack>
      </HStack> : null}
      {view === 'staff' && !inspection?.issue ? <MelodyStaff abc={abc} label={t('@yovoice.melody.viewStaff')} />
        : view === 'tracks' && score ? <ScoreArrangement readOnly score={score} selected={selected} select={setSelected} updateTrack={() => {}} seek={() => {}}
        label={t('@yovoice.melody.tracksLabel', { bars: inspection?.bars ?? 0 })} />
        : <TextArea className="melody-source" label={t('@yovoice.melody.source')} isLabelHidden value={abc} rows={16} hasSpellCheck={false}
          onChange={value => change({ abc: value.slice(0, maxLength) })} />}
    </>}
  </VStack>;
}
