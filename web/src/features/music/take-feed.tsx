import { useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { DropdownMenu } from '@astryxdesign/core/DropdownMenu';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { useLocale, useTranslator } from '@astryxdesign/core/i18n';
import { AudioLines, Disc3, Download, LoaderCircle, MoreHorizontal, Pause, Play, Sparkles } from 'lucide-react';
import { call } from '../../shared/lib/client';
import { noticeMessage } from '../../shared/lib/call-error';
import { formatActivity, formatCallError } from '../../shared/i18n/format';
import { ConfirmDelete } from '../../shared/ui/app-dialog';
import { formatTime, isYuE, type Draft, type Generation, type State } from '../../shared/workbench';
import { splitTags } from './music-draft';
import { SongCover } from './song-cover';

// 版本流：仿 Suno 的歌曲列表，当前作品生成的每一版是一张卡片（封面、标题、风格标签、时长与标记），
// 点封面在底部播放器试听，正在生成的一版以占位卡片显示进度；更多操作里可载入这一版的谱、复用风格歌词、导出或删除。
export function TakeFeed({ draft, state, takes, current, playing, play, pause, open, reuse, loadScore, exportItem, cancel, onError }: {
  draft: Draft; state: State; takes: Generation[]; current?: string; playing: boolean;
  play: (item: Generation) => void; pause: () => void; open: (item: Generation) => void; reuse: (item: Generation) => void;
  loadScore: (item: Generation) => void; exportItem: (item: Generation) => void; cancel: () => void; onError: (message: string) => void;
}) {
  const t = useTranslator();
  const locale = useLocale();
  const [removing, setRemoving] = useState<Generation | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const activity = state.activity;
  const generating = activity?.status === 'running' && activity.kind === 'generate' && activity.projectId === draft.id ? activity : null;
  // 版本号按生成先后从 1 开始，列表最新的在上。
  const ordered = [...takes].sort((a, b) => b.createdAt.localeCompare(a.createdAt));
  const number = (item: Generation) => takes.length - ordered.indexOf(item);
  const remove = async () => {
    if (!removing || busy) return;
    setBusy(true); setError('');
    try { if (current === removing.id) pause(); await call('media.delete', { kind: 'outputs', id: removing.id }); setRemoving(null); }
    catch (e) { setError(formatCallError(t, e)); } finally { setBusy(false); }
  };
  return <VStack className="take-feed" gap={0} role="region" aria-label={t('@yovoice.music.takes')}>
    <HStack className="take-feed-header" gap={2} vAlign="center">
      <h2 className="grow">{t('@yovoice.music.takes')}</h2>
      {takes.length ? <small className="take-count">{takes.length}</small> : null}
    </HStack>
    <VStack className="take-list" gap={2} role="list">
      {generating ? <HStack className="take take-pending" gap={3} vAlign="center" role="listitem" aria-busy="true">
        <VStack className="take-cover" gap={0} hAlign="center" vAlign="center"><LoaderCircle className="generation-spinner" aria-hidden="true" /></VStack>
        <VStack className="take-body grow" gap={1}>
          <b className="take-title">{t('@yovoice.music.takeGenerating', { n: takes.length + 1 })}</b>
          <small className="take-status" role="status">{formatActivity(t, generating)}</small>
          <span className="take-shimmer" aria-hidden="true" />
        </VStack>
        <Button size="sm" variant="ghost" label={t('@yovoice.action.cancel')} onClick={cancel} />
      </HStack> : null}
      {ordered.map(item => {
        const active = current === item.id, sounding = active && playing;
        const tags = item.settings.instrumental ? [t('@yovoice.music.instrumental'), ...splitTags(item.settings.text)] : splitTags(item.settings.text);
        const scored = isYuE(item.settings.modelId) && !!item.settings.abc?.trim();
        const label = t('@yovoice.app.historyVersion', { n: number(item) });
        return <HStack key={item.id} className="take" data-active={active || undefined} data-playing={sounding || undefined} gap={3} vAlign="center" role="listitem">
          <button type="button" className="take-cover" aria-label={t(sounding ? '@yovoice.music.pauseTake' : '@yovoice.music.playTake', { name: label })} onClick={() => sounding ? pause() : play(item)}>
            <SongCover seed={item.id} animated={sounding} size={112} />
            <span className="take-cover-icon" aria-hidden="true">{sounding ? <Pause fill="currentColor" /> : <Play fill="currentColor" />}</span>
          </button>
          <VStack className="take-body grow" gap={1}>
            <HStack gap={2} vAlign="center">
              <b className="take-title grow" title={item.settings.text}>{label}</b>
              <small className="take-duration">{formatTime(item.duration)}</small>
            </HStack>
            <HStack className="take-tags" gap={1} vAlign="center">
              <span className="take-badge" data-model={isYuE(item.settings.modelId) ? 'yue' : 'ace'}>{isYuE(item.settings.modelId) ? 'YuE2' : 'ACE'}</span>
              {scored ? <span className="take-badge" data-kind="score">{t('@yovoice.music.takeScored')}</span> : null}
              {tags.slice(0, 3).map(tag => <span key={tag} className="take-tag">{tag}</span>)}
            </HStack>
            <small className="take-time"><time dateTime={item.createdAt}>{new Date(item.createdAt).toLocaleString(locale, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })}</time></small>
          </VStack>
          <HStack className="take-actions" gap={0} vAlign="center">
            <Button size="sm" variant="ghost" isIconOnly icon={<Disc3 />} label={t('@yovoice.music.openNowPlaying', { name: label })} tooltip={t('@yovoice.music.nowPlaying')} onClick={() => open(item)} />
            <DropdownMenu presentation="popover" alignment="end" hasChevron={false} menuWidth="max-content"
              button={{ size: 'sm', variant: 'ghost', isIconOnly: true, icon: <MoreHorizontal />, label: t('@yovoice.music.takeActions', { name: label }) }}
              items={[
                ...(scored ? [{ id: 'score', label: t('@yovoice.music.loadTakeScore'), icon: <AudioLines />, onClick: () => loadScore(item) }] : []),
                { id: 'reuse', label: t('@yovoice.music.reuseTake'), icon: <Sparkles />, onClick: () => reuse(item) },
                { id: 'export', label: t('@yovoice.app.exportVersion'), icon: <Download />, onClick: () => exportItem(item) },
                { id: 'reveal', label: t('@yovoice.player.reveal'), onClick: () => { void call('media.reveal', { kind: 'outputs', id: item.id }).catch(e => onError(noticeMessage(e))); } },
                { type: 'divider' as const },
                { id: 'delete', label: t('@yovoice.media.delete', { noun: t('@yovoice.media.nounHistory'), name: label }), variant: 'destructive' as const, onClick: () => { setError(''); setRemoving(item); } },
              ]} />
          </HStack>
        </HStack>;
      })}
      {!takes.length && !generating ? <VStack className="take-empty" gap={2} hAlign="center" vAlign="center">
        <Disc3 aria-hidden="true" />
        <p>{t('@yovoice.music.takesEmpty')}</p>
        <small>{t('@yovoice.music.takesEmptyHint')}</small>
      </VStack> : null}
    </VStack>
    {removing ? <ConfirmDelete title={t('@yovoice.media.deleteTitle', { noun: t('@yovoice.media.nounHistory') })}
      description={t('@yovoice.media.deleteBody', { name: t('@yovoice.app.historyVersion', { n: number(removing) }) }) + t('@yovoice.media.deleteHistoryExtra')}
      confirmLabel={t('@yovoice.media.deleteConfirm', { noun: t('@yovoice.media.nounHistory') })} busy={busy} error={error} onClose={() => setRemoving(null)} onConfirm={() => void remove()} /> : null}
  </VStack>;
}
