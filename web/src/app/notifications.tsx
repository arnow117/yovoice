import { useState, type ReactNode } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { useTranslator } from '@astryxdesign/core/i18n';
import { CircleAlert, X } from 'lucide-react';
import { formatSize, stableJSON, type Activity } from '../shared/workbench';
import { formatActivity, formatActivityError, formatNotice } from '../shared/i18n/format';

function NoticeText({ error, activity }: { error: string; activity: Activity | null | undefined }) {
  const t = useTranslator();
  if (error) return <>{formatNotice(t, error)}</>;
  return <>{formatActivityError(t, activity) ?? t('@yovoice.error.unknown')}</>;
}

function ActivityLabel({ activity }: { activity: Activity }) {
  const t = useTranslator();
  return <strong data-testid="activity-label">{formatActivity(t, activity)}</strong>;
}


// 全局错误提示与后台任务进度；关闭的失败任务按内容记忆，不再重复提示。
export function Notifications({ error, clearError, activity, modelActivity, draftId, page, cancel, cancelModel, children }: { children?: ReactNode; error: string; clearError: () => void; activity: Activity | null; modelActivity?: Activity | null; draftId: string; page: string; cancel: () => void; cancelModel: () => void }) {
  const t = useTranslator();
  const [dismissedActivity, setDismissedActivity] = useState(() => localStorage.getItem('yovoice-dismissed-task') ?? '');
  // 模型失败与生成失败分开记忆：共用一个键时关闭其中一个，另一个已关闭的提示会重新出现。
  const [dismissedModel, setDismissedModel] = useState(() => localStorage.getItem('yovoice-dismissed-model-task') ?? '');
  const activityKey = activity ? stableJSON(activity) : '';
  const busy = activity?.status === 'running';
  // 模型下载与导入与生成并行：导入进度在这里展示，下载进度在模型列表中展示；失败在任何页面都提示一次。
  const modelKey = modelActivity ? stableJSON(modelActivity) : '';
  const modelFailed = modelActivity?.status === 'failed' && modelKey !== dismissedModel;
  const dismissModel = (key: string) => { setDismissedModel(key); localStorage.setItem('yovoice-dismissed-model-task', key); };
  return <VStack className="notifications" gap={2}>
      {modelFailed && !error ? <HStack className="notice" role="alert" gap={3} hAlign="between" vAlign="center"><CircleAlert className="notice-icon" aria-hidden /><p className="grow"><NoticeText error="" activity={modelActivity} /></p><Button label={t('@yovoice.action.closeNotice')} size="sm" variant="ghost" isIconOnly icon={<X size={16} />} onClick={() => dismissModel(modelKey)} /></HStack> : null}
      {error || (activity?.status === 'failed' && !activity.characterId && (!activity.projectId || activity.projectId === draftId) && page === 'create' && activityKey !== dismissedActivity) ? <HStack className="notice" role="alert" gap={3} hAlign="between" vAlign="center"><CircleAlert className="notice-icon" aria-hidden /><p className="grow"><NoticeText error={error} activity={activity} /></p><Button label={t('@yovoice.action.closeNotice')} size="sm" variant="ghost" isIconOnly icon={<X size={16} />} onClick={() => { clearError(); if (activity?.status === 'failed') { setDismissedActivity(activityKey); localStorage.setItem('yovoice-dismissed-task', activityKey); } }} /></HStack> : null}
      {busy && activity && !['download', 'generate'].includes(activity.kind) ? <HStack className="activity" role="status" gap={3} vAlign="center"><VStack className="grow" gap={2}><HStack hAlign="between"><ActivityLabel activity={activity} />{activity.total > 0 ? <small>{formatSize(activity.received)} / {formatSize(activity.total)}</small> : null}</HStack><progress value={activity.total > 0 ? activity.received : undefined} max={activity.total || 1} /></VStack><Button label={activity.kind === 'download' ? t('@yovoice.action.pause') : t('@yovoice.action.cancel')} size="sm" onClick={cancel} /></HStack> : null}
      {modelActivity?.status === 'running' && modelActivity.kind === 'import' ? <HStack className="activity" role="status" gap={3} vAlign="center"><VStack className="grow" gap={2}><ActivityLabel activity={modelActivity} /><progress max={1} /></VStack><Button label={t('@yovoice.action.cancel')} size="sm" onClick={cancelModel} /></HStack> : null}
      {children}
  </VStack>;
}
