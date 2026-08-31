import { Loader2 } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import type { CaseStatus, CostTier, TaskStatus } from '@/lib/api'
import { type DictKey, useI18n } from '@/lib/i18n'

// 任务/用例状态徽章：五态与三判定的配色统一，列表页与详情页共用

const taskStyles: Record<TaskStatus, string> = {
  queued: 'bg-muted text-muted-foreground',
  running: 'bg-blue-500/15 text-blue-600 dark:text-blue-400',
  succeeded: 'bg-green-500/15 text-green-600 dark:text-green-400',
  failed: 'bg-red-500/15 text-red-600 dark:text-red-400',
  canceled: 'bg-muted text-muted-foreground',
}

export function TaskStatusBadge({ status }: { status: TaskStatus }) {
  const { t } = useI18n()
  return (
    <Badge variant="outline" className={`border-transparent ${taskStyles[status]}`}>
      {status === 'running' && <Loader2 className="animate-spin" />}
      {t(`taskStatus.${status}` as DictKey)}
    </Badge>
  )
}

const caseStyles: Record<CaseStatus, string> = {
  passed: 'bg-green-500/15 text-green-600 dark:text-green-400',
  rejected: 'bg-amber-500/15 text-amber-600 dark:text-amber-400',
  violated: 'bg-red-500/15 text-red-600 dark:text-red-400',
  collected: 'bg-muted text-muted-foreground',
}

export function CaseStatusBadge({ status }: { status: CaseStatus }) {
  const { t } = useI18n()
  return (
    <Badge variant="outline" className={`border-transparent ${caseStyles[status]}`}>
      {t(`case.${status}` as DictKey)}
    </Badge>
  )
}

export function CostTierBadge({ tier }: { tier: CostTier }) {
  const { t } = useI18n()
  return <Badge variant="outline">{t(`tier.${tier}` as DictKey)}</Badge>
}
