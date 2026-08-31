import { Loader2 } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import type { CaseStatus, TaskStatus } from '@/lib/api'
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

// 得分阈值配色（≥95 绿 / ≥80 黄绿 / ≥60 琥珀 / <60 红）：
// 任务列表得分栏、详情页评分板、总览卡片共用同一视觉语言
export function scoreColor(score: number): string {
  if (score >= 95) return 'text-green-600 dark:text-green-400'
  if (score >= 80) return 'text-lime-600 dark:text-lime-400'
  if (score >= 60) return 'text-amber-600 dark:text-amber-400'
  return 'text-red-600 dark:text-red-400'
}

// ScoreText 得分文本：按阈值着色；无分（非终态/无采样）显示 —
export function ScoreText({ score, className }: { score?: number; className?: string }) {
  if (score == null) return <span className="text-muted-foreground">—</span>
  return (
    <span className={`tabular-nums ${scoreColor(score)} ${className ?? ''}`}>
      {score.toFixed(1)}
    </span>
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
