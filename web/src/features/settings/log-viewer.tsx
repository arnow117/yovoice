import { useCallback, useEffect, useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { Table, pixel, proportional, type TableColumn } from '@astryxdesign/core/Table';
import { Pagination } from '@astryxdesign/core/Pagination';
import { StatusDot } from '@astryxdesign/core/StatusDot';
import { TextInput } from '@astryxdesign/core/TextInput';
import { useLocale, useTranslator, type TranslatorFn } from '@astryxdesign/core/i18n';
import { ArrowUpRight, Download, RefreshCw } from 'lucide-react';
import { call, isDesktop } from '../../shared/lib/client';
import { parseCallError } from '../../shared/lib/call-error';
import { formatCallError } from '../../shared/i18n/format';
import { AppDialog } from '../../shared/ui/app-dialog';
import { Selector } from '../../shared/selector';
import type { LogEntry, LogLevel, LogPage } from '../../shared/workbench';
import { useAppUpdate } from '../update/app-update';

const pageSize = 50;
const levelVariant: Record<LogLevel, 'neutral' | 'warning' | 'error'> = { INFO: 'neutral', WARN: 'warning', ERROR: 'error' };
const ranges: Record<string, number> = { hour: 3600e3, day: 86400e3, week: 7 * 86400e3 };

interface Row extends Record<string, unknown> { key: string; entry: LogEntry }

// 一行摘要：错误翻译成界面文案放在最前，其余字段写成 key=value。
function summary(t: TranslatorFn, entry: LogEntry) {
  const { error, ...rest } = entry.fields ?? {};
  const parts = Object.entries(rest).map(([key, value]) => `${key}=${typeof value === 'string' ? value : JSON.stringify(value)}`);
  const text = parts.join(' · ');
  const message = error ? formatCallError(t, parseCallError(error)) : '';
  return [message, text.length > 200 ? `${text.slice(0, 200)}…` : text].filter(Boolean).join(' — ');
}

// 设置 › 日志：按级别、模块、时间和关键词查看服务日志，点一行看完整字段；可导出诊断包用于反馈问题。
export function LogViewer({ run }: { run: (task: () => Promise<unknown>) => void }) {
  const t = useTranslator();
  const locale = useLocale();
  const version = useAppUpdate()?.current ?? '';
  const [level, setLevel] = useState('');
  const [module, setModule] = useState('');
  const [range, setRange] = useState('');
  const [text, setText] = useState('');
  const [query, setQuery] = useState('');
  const [page, setPage] = useState(1);
  const [result, setResult] = useState<LogPage>({ entries: [], total: 0, modules: [] });
  const [loading, setLoading] = useState(false);
  const [detail, setDetail] = useState<LogEntry | null>(null);
  // 关键词输入停顿后再查询，避免每个按键都读一遍日志文件。
  useEffect(() => { const timer = setTimeout(() => { setQuery(text.trim()); setPage(1); }, 300); return () => clearTimeout(timer); }, [text]);
  const load = useCallback(() => run(async () => {
    setLoading(true);
    try {
      const since = ranges[range] ? new Date(Date.now() - ranges[range]).toISOString() : undefined;
      setResult(await call<LogPage>('logs.query', { level, module, text: query, since, offset: (page - 1) * pageSize, limit: pageSize }));
    } finally { setLoading(false); }
  }), [run, level, module, range, query, page]);
  useEffect(() => { load(); }, [load]);
  const filter = (set: (value: string) => void) => (value: string) => { set(value); setPage(1); };
  const time = (value: string) => new Date(value).toLocaleString(locale, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' });
  const levelLabel = (value: LogLevel) => t(`@yovoice.logs.level.${value.toLowerCase()}`);
  const columns: TableColumn<Row>[] = [
    { key: 'time', header: t('@yovoice.logs.time'), width: pixel(150), renderCell: row => <small>{time(row.entry.time)}</small> },
    { key: 'level', header: t('@yovoice.logs.levelColumn'), width: pixel(96), renderCell: row => <HStack gap={2} vAlign="center"><StatusDot variant={levelVariant[row.entry.level]} label={levelLabel(row.entry.level)} /><small>{levelLabel(row.entry.level)}</small></HStack> },
    { key: 'event', header: t('@yovoice.logs.event'), width: proportional(1), renderCell: row => <small><code>{row.entry.event}</code></small> },
    { key: 'summary', header: t('@yovoice.logs.summary'), width: proportional(2), renderCell: row => <small>{summary(t, row.entry)}</small> },
    { key: 'detail', header: '', width: pixel(72), renderCell: row => <Button size="sm" variant="ghost" label={t('@yovoice.logs.detail')} onClick={() => setDetail(row.entry)} /> },
  ];
  const rows: Row[] = result.entries.map((entry, index) => ({ key: `${entry.time}-${index}`, entry }));
  return <VStack className="settings-panel" gap={4} id="logs-panel" role="tabpanel" aria-label={t('@yovoice.settings.tabLogs')}>
    <HStack hAlign="between" vAlign="end" gap={3} wrap="wrap">
      <HStack gap={2} vAlign="end" wrap="wrap">
        <Selector size="sm" label={t('@yovoice.logs.levelColumn')} value={level} onChange={filter(setLevel)} width="calc(var(--spacing-10) * 3.5)"
          options={[{ value: '', label: t('@yovoice.logs.allLevels') }, { value: 'warn', label: t('@yovoice.logs.warnAndAbove') }, { value: 'error', label: t('@yovoice.logs.errorsOnly') }]} />
        <Selector size="sm" label={t('@yovoice.logs.module')} value={module} onChange={filter(setModule)} width="calc(var(--spacing-10) * 3.5)"
          options={[{ value: '', label: t('@yovoice.logs.allModules') }, ...result.modules.map(value => ({ value, label: value }))]} />
        <Selector size="sm" label={t('@yovoice.logs.range')} value={range} onChange={filter(setRange)} width="calc(var(--spacing-10) * 3.5)"
          options={[{ value: '', label: t('@yovoice.logs.allTime') }, { value: 'hour', label: t('@yovoice.logs.lastHour') }, { value: 'day', label: t('@yovoice.logs.lastDay') }, { value: 'week', label: t('@yovoice.logs.lastWeek') }]} />
        <TextInput size="sm" label={t('@yovoice.logs.search')} value={text} onChange={setText} placeholder={t('@yovoice.logs.searchPlaceholder')} width="calc(var(--spacing-10) * 5)" />
      </HStack>
      <HStack gap={2} wrap="wrap">
        <Button size="sm" variant="ghost" label={t('@yovoice.logs.refresh')} icon={<RefreshCw size={15} />} isLoading={loading} onClick={load} />
        {isDesktop ? <Button size="sm" label={t('@yovoice.settings.openLogs')} icon={<ArrowUpRight size={15} />} onClick={() => run(() => call('logs.open'))} /> : null}
        <Button size="sm" label={t('@yovoice.logs.export')} icon={<Download size={15} />} onClick={() => run(() => call('logs.export', { version }))} />
      </HStack>
    </HStack>
    <small>{t('@yovoice.logs.hint')}</small>
    <Table<Row> data={rows} columns={columns} idKey="key" density="compact" textOverflow="wrap" verticalAlign="top" hasHover />
    {result.total === 0 && !loading ? <small>{t('@yovoice.logs.empty')}</small> : null}
    {result.total > pageSize ? <Pagination size="sm" variant="count" page={page} pageSize={pageSize} totalItems={result.total} onChange={setPage} label={t('@yovoice.logs.pages')} /> : null}
    {detail ? <AppDialog title={detail.event} subtitle={`${time(detail.time)} · ${levelLabel(detail.level)}`} width={640} onClose={() => setDetail(null)}
      actions={<><Button label={t('@yovoice.logs.copy')} onClick={() => run(() => navigator.clipboard.writeText(JSON.stringify(detail, null, 2)))} /><Button label={t('@yovoice.action.close')} variant="primary" onClick={() => setDetail(null)} /></>}>
      {detail.fields?.error ? <p className="dialog-error">{formatCallError(t, parseCallError(detail.fields.error))}</p> : null}
      <pre className="license-text">{JSON.stringify(detail.fields ?? {}, null, 2)}</pre>
    </AppDialog> : null}
  </VStack>;
}
