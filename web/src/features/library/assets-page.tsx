import { useCallback, useEffect, useMemo, useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { EmptyState } from '@astryxdesign/core/EmptyState';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { Pagination } from '@astryxdesign/core/Pagination';
import { StatusDot } from '@astryxdesign/core/StatusDot';
import { Table, pixel, proportional, useTableSelection, useTableSortable, type TableColumn } from '@astryxdesign/core/Table';
import { TextInput } from '@astryxdesign/core/TextInput';
import { Token } from '@astryxdesign/core/Token';
import { useLocale, useTranslator } from '@astryxdesign/core/i18n';
import { ArrowUpRight, FolderOpen, Image as ImageIcon, Play, RefreshCw, Trash2 } from 'lucide-react';
import { call, isDesktop } from '../../shared/lib/client';
import { noticeMessage } from '../../shared/lib/call-error';
import { formatCallError } from '../../shared/i18n/format';
import { AppDialog } from '../../shared/ui/app-dialog';
import { Selector } from '../../shared/selector';
import { formatBytes, formatTime, type Asset, type AssetKind, type AssetResult, type State, type Track } from '../../shared/workbench';
import { Player } from '../media/player';

const pageSize = 50;
const kinds: AssetKind[] = ['generation', 'segment', 'voice', 'material', 'image'];
type SortKey = 'createdAt' | 'bytes' | 'duration' | 'name';
interface Row extends Record<string, unknown> { key: string; asset: Asset }

const rowKey = (a: Asset) => `${a.kind}:${a.id}`;
const isUsed = (a: Asset) => (a.users?.length ?? 0) > 0;
// 时间轴素材属于作品草稿，只能在作品的时间轴里移除；正在被引用的也不能勾选。
const canDelete = (a: Asset) => a.kind !== 'material' && !isUsed(a);

/**
 * 资源页：汇总所有生成音频、故事配音片段、参考音频、时间轴素材和 AI 图片，
 * 按类型、作品、使用情况和关键词筛选，可排序、试听或预览，并批量删除未被使用的项目。
 */
export function AssetsPage({ state, active, run, onError, openProject, openStorage }: {
  state: State; active: boolean; run: (task: () => Promise<unknown>) => void; onError: (error: string) => void;
  openProject: (id: string) => void; openStorage: () => void;
}) {
  const t = useTranslator();
  const locale = useLocale();
  const [assets, setAssets] = useState<Asset[]>([]);
  const [loading, setLoading] = useState(false);
  const [kind, setKind] = useState('');
  const [project, setProject] = useState('');
  const [usage, setUsage] = useState('');
  const [text, setText] = useState('');
  const [sort, setSort] = useState<{ sortKey: SortKey; direction: 'ascending' | 'descending' }[]>([{ sortKey: 'createdAt', direction: 'descending' }]);
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [confirming, setConfirming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [outcome, setOutcome] = useState<{ deleted: number; freed: number; failed: { asset: Asset; result: AssetResult }[] } | null>(null);
  const [showFailures, setShowFailures] = useState(false);
  const [track, setTrack] = useState<Track | null>(null);
  const [image, setImage] = useState<{ name: string; src: string } | null>(null);

  const load = useCallback(() => run(async () => {
    setLoading(true);
    try { setAssets(await call<Asset[]>('assets.list') ?? []); } finally { setLoading(false); }
  }), [run]);
  // 进入页面时读取；生成、删除或导入改变了状态时重新汇总（文件大小只有后端知道）。
  useEffect(() => { if (active) load(); }, [active, load, state.history, state.voices, state.drafts]);
  // 列表刷新后去掉已不存在的勾选项。
  useEffect(() => setSelected(current => new Set([...current].filter(key => assets.some(a => rowKey(a) === key && canDelete(a))))), [assets]);

  const totals = useMemo(() => {
    const byKind = new Map<AssetKind, { count: number; bytes: number }>();
    for (const a of assets) {
      const entry = byKind.get(a.kind) ?? { count: 0, bytes: 0 };
      entry.count++; entry.bytes += Math.max(a.bytes, 0);
      byKind.set(a.kind, entry);
    }
    return { byKind, bytes: [...byKind.values()].reduce((sum, v) => sum + v.bytes, 0) };
  }, [assets]);
  const projects = useMemo(() => [...new Map(assets.filter(a => a.projectId).map(a => [a.projectId!, a.projectTitle ?? ''])).entries()], [assets]);

  const filtered = useMemo(() => {
    const query = text.trim().toLocaleLowerCase();
    const list = assets.filter(a => (!kind || a.kind === kind)
      && (!project || (project === '-' ? a.projectId && !a.projectTitle : a.projectId === project))
      && (!usage || (usage === 'used') === isUsed(a))
      && (!query || [a.name, a.projectTitle ?? '', ...(a.users ?? [])].some(v => v.toLocaleLowerCase().includes(query))));
    const [{ sortKey, direction } = { sortKey: 'createdAt' as SortKey, direction: 'descending' as const }] = sort;
    const value = (a: Asset) => sortKey === 'name' ? a.name : sortKey === 'createdAt' ? a.createdAt : sortKey === 'bytes' ? a.bytes : a.duration ?? 0;
    return [...list].sort((a, b) => {
      const x = value(a), y = value(b);
      const order = typeof x === 'string' ? x.localeCompare(String(y), locale) : x - Number(y);
      return direction === 'ascending' ? order : -order;
    });
  }, [assets, kind, project, usage, text, sort, locale]);
  const pageRows: Row[] = filtered.slice((page - 1) * pageSize, page * pageSize).map(asset => ({ key: rowKey(asset), asset }));
  const deletable = filtered.filter(canDelete);
  const picked = assets.filter(a => selected.has(rowKey(a)));
  const pickedBytes = picked.reduce((sum, a) => sum + Math.max(a.bytes, 0), 0);
  const filter = (set: (value: string) => void) => (value: string) => { set(value); setPage(1); };

  const selection = useTableSelection<Row>({
    getIsItemSelected: row => selected.has(row.key),
    onSelectItem: ({ item, isSelected }) => setSelected(current => { const next = new Set(current); if (isSelected) next.add(item.key); else next.delete(item.key); return next; }),
    onSelectAll: ({ isAllSelected }) => setSelected(isAllSelected ? new Set(deletable.map(rowKey)) : new Set()),
    getIsAllSelected: () => deletable.length > 0 && deletable.every(a => selected.has(rowKey(a))),
    getIsIndeterminate: () => selected.size > 0 && !deletable.every(a => selected.has(rowKey(a))),
    getIsItemEnabled: row => canDelete(row.asset),
    getRowLabel: row => row.asset.name,
  });
  const sortable = useTableSortable<Row, SortKey>({ sort, onSortChange: next => { setSort(next as typeof sort); setPage(1); } });

  const kindLabel = (value: AssetKind) => t(`@yovoice.assets.kind.${value}`);
  const time = (value: string) => value && !value.startsWith('0001') ? new Date(value).toLocaleString(locale, { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) : '—';
  const listen = (a: Asset) => setTrack({ id: a.id, name: a.name, fileName: a.fileName, kind: a.media === 'voices' ? 'voices' : 'outputs', subtitle: kindLabel(a.kind), playRequest: performance.now() });
  const preview = (a: Asset) => run(async () => setImage({ name: a.name, src: await call<string>('assets.image', { id: a.id }) }));
  const columns: TableColumn<Row>[] = [
    { key: 'name', header: t('@yovoice.assets.col.name'), width: proportional(2), sortable: true, renderCell: ({ asset: a }) => <HStack gap={2} vAlign="center">
      {a.kind === 'image'
        ? <Button size="sm" variant="ghost" isIconOnly icon={<ImageIcon size={15} />} label={t('@yovoice.assets.preview', { name: a.name })} isDisabled={a.bytes < 0} onClick={() => preview(a)} />
        : <Button size="sm" variant="ghost" isIconOnly icon={<Play size={15} />} label={t('@yovoice.assets.play', { name: a.name })} isDisabled={a.bytes < 0} onClick={() => listen(a)} />}
      <small className="asset-name" title={a.name}>{a.name}</small>
    </HStack> },
    { key: 'kind', header: t('@yovoice.assets.col.kind'), width: pixel(104), renderCell: ({ asset: a }) => <Token size="sm" color={a.kind === 'voice' ? 'teal' : a.kind === 'image' ? 'purple' : a.kind === 'material' ? 'orange' : 'default'} label={kindLabel(a.kind)} /> },
    { key: 'project', header: t('@yovoice.assets.col.project'), width: proportional(1), renderCell: ({ asset: a }) => a.projectId
      ? a.projectTitle ? <Button size="sm" variant="ghost" label={a.projectTitle} onClick={() => openProject(a.projectId!)} /> : <small className="asset-muted">{t('@yovoice.assets.sourceMissing')}</small>
      : <small className="asset-muted">—</small> },
    { key: 'duration', header: t('@yovoice.assets.col.duration'), width: pixel(76), align: 'end', sortable: true, renderCell: ({ asset: a }) => <small>{a.duration ? formatTime(a.duration) : '—'}</small> },
    { key: 'bytes', header: t('@yovoice.assets.col.size'), width: pixel(92), align: 'end', sortable: true, renderCell: ({ asset: a }) => <small>{a.bytes < 0 ? '—' : formatBytes(a.bytes)}</small> },
    { key: 'createdAt', header: t('@yovoice.assets.col.created'), width: pixel(136), sortable: true, renderCell: ({ asset: a }) => <small>{time(a.createdAt)}</small> },
    { key: 'status', header: t('@yovoice.assets.col.status'), width: proportional(1), renderCell: ({ asset: a }) => a.bytes < 0
      ? <HStack gap={2} vAlign="center"><StatusDot variant="error" label={t('@yovoice.assets.status.missing')} /><small>{t('@yovoice.assets.status.missing')}</small></HStack>
      : isUsed(a)
        ? <HStack gap={2} vAlign="center"><StatusDot variant="warning" label={t('@yovoice.assets.status.used')} /><small className="asset-name" title={t('@yovoice.assets.status.usedBy', { names: a.users!.join('、') })}>{t('@yovoice.assets.status.usedBy', { names: a.users!.join('、') })}</small></HStack>
        : <HStack gap={2} vAlign="center"><StatusDot variant="neutral" label={t('@yovoice.assets.status.unused')} /><small>{t('@yovoice.assets.status.unused')}</small></HStack> },
    { key: 'actions', header: '', width: pixel(76), renderCell: ({ asset: a }) => <HStack gap={1}>
      {isDesktop && a.media !== 'images' && a.bytes >= 0 ? <Button size="sm" variant="ghost" isIconOnly icon={<FolderOpen size={15} />} label={t('@yovoice.media.reveal', { name: a.name })}
        onClick={() => { void call('media.reveal', { kind: a.media, id: a.id }).catch(e => onError(noticeMessage(e))); }} /> : null}
      {canDelete(a) ? <Button size="sm" variant="ghost" isIconOnly icon={<Trash2 size={15} />} label={t('@yovoice.assets.deleteOne', { name: a.name })}
        onClick={() => { setSelected(new Set([rowKey(a)])); setConfirming(true); }} /> : null}
    </HStack> },
  ];

  const remove = () => run(async () => {
    setDeleting(true);
    try {
      const items = picked.map(a => ({ kind: a.kind, id: a.id }));
      const results = await call<AssetResult[]>('assets.delete', { items });
      const byKey = new Map(picked.map(a => [rowKey(a), a]));
      const failed = results.filter(r => r.error).map(result => ({ asset: byKey.get(`${result.kind}:${result.id}`)!, result }));
      const done = results.filter(r => !r.error).map(r => byKey.get(`${r.kind}:${r.id}`)!);
      setOutcome({ deleted: done.length, freed: done.reduce((sum, a) => sum + Math.max(a.bytes, 0), 0), failed });
      setShowFailures(failed.length > 0);
      if (track && done.some(a => a.id === track.id)) setTrack(null);
      setSelected(new Set()); setConfirming(false);
      load();
    } finally { setDeleting(false); }
  });

  return <VStack className="library-page assets-page" gap={4} data-testid="assets-page">
    <HStack hAlign="between" vAlign="center" gap={3} wrap="wrap">
      <VStack gap={1}>
        <h1>{t('@yovoice.assets.title')}</h1>
        <small className="asset-muted">{t('@yovoice.assets.total', { count: assets.length, size: formatBytes(totals.bytes) })}</small>
      </VStack>
      <HStack gap={2} wrap="wrap">
        <Button size="sm" variant="ghost" label={t('@yovoice.logs.refresh')} icon={<RefreshCw size={15} />} isLoading={loading} onClick={load} />
        <Button size="sm" label={t('@yovoice.assets.storage')} icon={<ArrowUpRight size={15} />} onClick={openStorage} />
      </HStack>
    </HStack>
    <HStack className="asset-stats" gap={2} wrap="wrap" role="group" aria-label={t('@yovoice.assets.stats')}>
      {kinds.filter(k => totals.byKind.has(k)).map(k => <Button key={k} size="sm" aria-pressed={kind === k} variant={kind === k ? 'primary' : 'secondary'}
        label={t('@yovoice.assets.stat', { kind: kindLabel(k), count: totals.byKind.get(k)!.count, size: formatBytes(totals.byKind.get(k)!.bytes) })}
        onClick={() => filter(setKind)(kind === k ? '' : k)} />)}
    </HStack>
    <HStack gap={2} vAlign="end" wrap="wrap">
      <Selector size="sm" label={t('@yovoice.assets.col.kind')} value={kind} onChange={filter(setKind)} width="calc(var(--spacing-10) * 3.5)"
        options={[{ value: '', label: t('@yovoice.assets.allKinds') }, ...kinds.map(value => ({ value, label: kindLabel(value) }))]} />
      <Selector size="sm" label={t('@yovoice.assets.col.project')} value={project} onChange={filter(setProject)} width="calc(var(--spacing-10) * 4.5)"
        options={[{ value: '', label: t('@yovoice.assets.allProjects') }, ...projects.filter(([, title]) => title).map(([value, label]) => ({ value, label })), ...(projects.some(([, title]) => !title) ? [{ value: '-', label: t('@yovoice.assets.sourceMissing') }] : [])]} />
      <Selector size="sm" label={t('@yovoice.assets.usage')} value={usage} onChange={filter(setUsage)} width="calc(var(--spacing-10) * 3)"
        options={[{ value: '', label: t('@yovoice.assets.usage.all') }, { value: 'unused', label: t('@yovoice.assets.status.unused') }, { value: 'used', label: t('@yovoice.assets.status.used') }]} />
      <TextInput size="sm" label={t('@yovoice.assets.search')} value={text} onChange={filter(setText)} placeholder={t('@yovoice.assets.searchPlaceholder')} width="calc(var(--spacing-10) * 5)" />
    </HStack>
    {selected.size > 0 ? <HStack className="asset-selection" gap={3} vAlign="center" wrap="wrap" role="region" aria-label={t('@yovoice.assets.selection')}>
      <strong>{t('@yovoice.assets.selected', { count: selected.size, size: formatBytes(pickedBytes) })}</strong>
      <Button size="sm" variant="destructive" icon={<Trash2 size={15} />} label={t('@yovoice.assets.deleteSelected')} onClick={() => setConfirming(true)} />
      <Button size="sm" variant="ghost" label={t('@yovoice.assets.clearSelection')} onClick={() => setSelected(new Set())} />
    </HStack> : outcome ? <p className="asset-outcome" role="status">
      {t('@yovoice.assets.deleted', { count: outcome.deleted, size: formatBytes(outcome.freed) })}
      {outcome.failed.length ? <> {t('@yovoice.assets.skipped', { count: outcome.failed.length })} <Button size="sm" variant="ghost" label={t('@yovoice.assets.showSkipped')} onClick={() => setShowFailures(true)} /></> : null}
    </p> : <small className="asset-muted">{t('@yovoice.assets.hint')}</small>}
    <Table<Row> data={pageRows} columns={columns} idKey="key" density="compact" textOverflow="truncate" hasHover plugins={{ selection, sortable }}
      emptyState={loading ? false : <EmptyState title={t(assets.length ? '@yovoice.assets.noMatch' : '@yovoice.assets.empty')} />} />
    {filtered.length > pageSize ? <Pagination size="sm" variant="count" page={page} pageSize={pageSize} totalItems={filtered.length} onChange={setPage} label={t('@yovoice.assets.pages')} /> : null}
    {track ? <Player track={track} suspended={!active} onError={onError} reveal={false} /> : null}

    {confirming ? <AppDialog title={t('@yovoice.assets.deleteTitle', { count: picked.length })} busy={deleting} onClose={() => setConfirming(false)}
      actions={<><Button label={t('@yovoice.action.cancel')} onClick={() => setConfirming(false)} /><Button variant="destructive" label={t('@yovoice.assets.deleteConfirm')} isLoading={deleting} onClick={remove} /></>}>
      <p>{t('@yovoice.assets.deleteBody', { size: formatBytes(pickedBytes) })}</p>
    </AppDialog> : null}
    {showFailures && outcome ? <AppDialog title={t('@yovoice.assets.skippedTitle')} onClose={() => setShowFailures(false)}
      actions={<Button variant="primary" label={t('@yovoice.action.close')} onClick={() => setShowFailures(false)} />}>
      <VStack as="ul" className="asset-failures" gap={2}>{outcome.failed.map(({ asset, result }) => <li key={rowKey(asset)}><strong>{asset.name}</strong> — {formatCallError(t, { errorCode: result.error, errorParams: result.params })}</li>)}</VStack>
    </AppDialog> : null}
    {image ? <AppDialog title={image.name} width={720} onClose={() => setImage(null)} actions={<Button variant="primary" label={t('@yovoice.action.close')} onClick={() => setImage(null)} />}>
      <img className="asset-image" src={image.src} alt={image.name} />
    </AppDialog> : null}
  </VStack>;
}
