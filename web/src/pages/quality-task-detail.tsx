import { Fragment, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, ChevronDown, Loader2, X } from 'lucide-react'
import { useNavigate, useParams } from 'react-router'
import { errText } from '@/components/channel-form-dialog'
import { CaseStatusBadge, TaskStatusBadge } from '@/components/quality-badges'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Progress } from '@/components/ui/progress'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { isTerminalStatus, useTaskEvents } from '@/hooks/use-task-events'
import {
  probesApi,
  qualityApi,
  type CaseStatus,
  type QualityCaseResult,
  type QualityTask,
  type TaskStatBucket,
} from '@/lib/api'
import { type DictKey, useI18n } from '@/lib/i18n'

function formatDuration(ms: number): string {
  const s = Math.round(ms / 1000)
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`
}

export default function QualityTaskDetailPage() {
  const { taskId = '' } = useParams()
  const { t } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const taskQ = useQuery({
    queryKey: ['quality-task', taskId],
    queryFn: () => qualityApi.getTask(taskId),
    retry: false,
  })
  const probes = useQuery({ queryKey: ['probes'], queryFn: probesApi.list })

  const task = taskQ.data
  const active = task != null && !isTerminalStatus(task.status)
  // SSE 实时帧优先，DB 快照兜底；断线重连由 EventSource 自理，重连后先收快照帧补平
  const live = useTaskEvents(taskId, active)
  const status = live?.status ?? task?.status
  const done = live?.done ?? task?.progressDone ?? 0
  const total = live?.total ?? task?.progressTotal ?? 0

  const [statusFilter, setStatusFilter] = useState<CaseStatus | 'all'>('all')
  const results = useQuery({
    queryKey: ['quality-task-results', taskId, statusFilter],
    queryFn: () =>
      qualityApi.listResults(taskId, statusFilter === 'all' ? undefined : statusFilter),
    enabled: task != null,
    // 运行中 3s 轮询让用例表实时填充；终结后靠 SSE 终态帧触发的失效拉最终数据
    refetchInterval: active ? 3000 : false,
  })

  const [cancelOpen, setCancelOpen] = useState(false)
  const cancel = useMutation({
    mutationFn: () => qualityApi.cancelTask(taskId),
    onSuccess: () => {
      setCancelOpen(false)
      queryClient.invalidateQueries({ queryKey: ['quality-task', taskId] })
      queryClient.invalidateQueries({ queryKey: ['quality-tasks'] })
    },
  })
  const closeCancelDialog = () => {
    setCancelOpen(false)
    cancel.reset()
  }

  if (taskQ.isPending) {
    return <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
  }
  if (taskQ.isError || task == null) {
    return (
      <div className="grid gap-4">
        <p className="text-sm text-destructive">{t('quality.notFound')}</p>
        <Button variant="outline" className="w-fit" onClick={() => navigate('/quality')}>
          <ArrowLeft />
          {t('common.back')}
        </Button>
      </div>
    )
  }

  const probeName = (id: string) => probes.data?.find((p) => p.id === id)?.name ?? id
  const pct = total > 0 ? (done / total) * 100 : 0

  return (
    <div className="grid gap-6">
      <div className="flex flex-wrap items-center gap-3">
        <Button
          variant="ghost"
          size="icon"
          className="size-8"
          title={t('common.back')}
          onClick={() => navigate('/quality')}
        >
          <ArrowLeft />
        </Button>
        <h1 className="text-2xl font-semibold">{t('quality.taskTitle')}</h1>
        {status && <TaskStatusBadge status={status} />}
        {(status === 'queued' || status === 'running') && (
          <Button variant="outline" className="ml-auto" onClick={() => setCancelOpen(true)}>
            <X />
            {t('quality.cancelTask')}
          </Button>
        )}
      </div>

      {status != null && !isTerminalStatus(status) && (
        <div className="flex items-center gap-3">
          <Progress value={pct} className="max-w-96" />
          <span className="text-sm tabular-nums text-muted-foreground">
            {done}/{total}
          </span>
        </div>
      )}
      {task.error && (
        <p className="text-sm text-destructive">
          {t('quality.taskError')}：{task.error}
        </p>
      )}

      <SnapshotCard task={task} probeName={probeName} />
      {task.stats && <StatsCard stats={task.stats} />}
      <ResultsCard
        results={results.data}
        pending={results.isPending}
        error={results.isError ? results.error : null}
        statusFilter={statusFilter}
        onFilterChange={setStatusFilter}
        active={active}
      />

      <Dialog open={cancelOpen} onOpenChange={(o) => { if (!o) closeCancelDialog() }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t('quality.cancelTask')}</DialogTitle>
            <DialogDescription>{t('quality.cancelConfirmDesc')}</DialogDescription>
          </DialogHeader>
          {cancel.isError && (
            <p className="text-sm text-destructive">{errText(cancel.error)}</p>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={closeCancelDialog}>
              {t('quality.keepRunning')}
            </Button>
            <Button
              variant="destructive"
              disabled={cancel.isPending}
              onClick={() => cancel.mutate()}
            >
              {cancel.isPending && <Loader2 className="animate-spin" />}
              {t('quality.cancelConfirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

// SnapshotCard 参数快照卡：检测对象与任务参数在创建时定格，这里展示的是历史事实
function SnapshotCard({
  task,
  probeName,
}: {
  task: QualityTask
  probeName: (id: string) => string
}) {
  const { t } = useI18n()
  const tg = task.target
  const symbol = tg.currency === 'CNY' ? '¥' : '$'
  const startedAt = task.startedAt ? new Date(task.startedAt) : null
  const finishedAt = task.finishedAt ? new Date(task.finishedAt) : null

  const rows: [string, React.ReactNode][] = [
    [t('quality.channel'), tg.channelName],
    [t('channels.baseUrl'), tg.baseUrl],
    [t('quality.model'), tg.model],
    [
      t('channels.protocols'),
      <div key="protos" className="flex flex-wrap gap-1">
        {tg.protocols.map((p) => (
          <Badge key={p} variant="outline">
            {t(`proto.${p}` as DictKey)}
          </Badge>
        ))}
      </div>,
    ],
    [
      t('quality.probes'),
      task.probes.map(probeName).join('、'),
    ],
    [
      t('quality.paramsLabel'),
      `${t('quality.concShort')} ${task.params.concurrency} · ${t('quality.rerunsShort')} ${task.params.reruns}` +
        (task.params.maxCases != null ? ` · ${t('quality.maxCases')} ${task.params.maxCases}` : ''),
    ],
  ]
  if (tg.inputPrice != null) {
    rows.push([
      t('quality.pricing'),
      `${t('models.inputPrice')} ${symbol}${tg.inputPrice} · ${t('models.outputPrice')} ${symbol}${tg.outputPrice}` +
        (tg.cachedInputPrice != null
          ? ` · ${t('models.cachedInputPrice')} ${symbol}${tg.cachedInputPrice}`
          : ''),
    ])
  }
  rows.push([
    t('quality.createdAt'),
    `${new Date(task.createdAt).toLocaleString()}${task.createdBy ? ` · ${task.createdBy}` : ''}`,
  ])
  if (startedAt) rows.push([t('quality.startedAt'), startedAt.toLocaleString()])
  if (finishedAt) {
    rows.push([
      t('quality.finishedAt'),
      `${finishedAt.toLocaleString()}${
        startedAt
          ? ` · ${t('quality.duration')} ${formatDuration(finishedAt.getTime() - startedAt.getTime())}`
          : ''
      }`,
    ])
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('quality.snapshotTitle')}</CardTitle>
        <CardDescription>{t('quality.snapshotDesc')}</CardDescription>
      </CardHeader>
      <CardContent>
        <dl className="grid gap-x-8 gap-y-2 text-sm sm:grid-cols-[auto_1fr]">
          {rows.map(([label, value]) => (
            <Fragment key={label}>
              <dt className="text-muted-foreground">{label}</dt>
              <dd className="break-all">{value}</dd>
            </Fragment>
          ))}
        </dl>
      </CardContent>
    </Card>
  )
}

function StatsCard({ stats }: { stats: NonNullable<QualityTask['stats']> }) {
  const { t } = useI18n()
  const top: [string, number, string][] = [
    [t('quality.total'), stats.total, ''],
    [t('case.passed'), stats.passed, 'text-green-600 dark:text-green-400'],
    [t('case.rejected'), stats.rejected, 'text-amber-600 dark:text-amber-400'],
    [t('case.violated'), stats.violated, 'text-red-600 dark:text-red-400'],
  ]
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('quality.statsTitle')}</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-6">
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
          {top.map(([label, value, cls]) => (
            <div key={label} className="rounded-lg border p-4">
              <p className="text-sm text-muted-foreground">{label}</p>
              <p className={`mt-1 text-2xl font-semibold tabular-nums ${cls}`}>{value}</p>
            </div>
          ))}
        </div>
        <div className="grid gap-6 lg:grid-cols-2">
          <BucketTable title={t('quality.byMode')} buckets={stats.byMode} translateName />
          <BucketTable title={t('quality.byReason')} buckets={stats.byReason} />
        </div>
      </CardContent>
    </Card>
  )
}

function BucketTable({
  title,
  buckets,
  translateName = false,
}: {
  title: string
  buckets: TaskStatBucket[]
  translateName?: boolean
}) {
  const { t } = useI18n()
  return (
    <div className="grid gap-2">
      <p className="text-sm font-medium">{title}</p>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('quality.bucket')}</TableHead>
            <TableHead className="text-right">{t('quality.total')}</TableHead>
            <TableHead className="text-right">{t('case.passed')}</TableHead>
            <TableHead className="text-right">{t('case.rejected')}</TableHead>
            <TableHead className="text-right">{t('case.violated')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {buckets.map((b) => (
            <TableRow key={b.name}>
              <TableCell>
                {translateName ? t(`mode.${b.name}` as DictKey) : b.name}
              </TableCell>
              <TableCell className="text-right tabular-nums">{b.total}</TableCell>
              <TableCell className="text-right tabular-nums">{b.passed}</TableCell>
              <TableCell className="text-right tabular-nums">{b.rejected}</TableCell>
              <TableCell className="text-right tabular-nums">{b.violated}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function ResultsCard({
  results,
  pending,
  error,
  statusFilter,
  onFilterChange,
  active,
}: {
  results: QualityCaseResult[] | undefined
  pending: boolean
  error: unknown
  statusFilter: CaseStatus | 'all'
  onFilterChange: (v: CaseStatus | 'all') => void
  active: boolean
}) {
  const { t } = useI18n()
  const [expanded, setExpanded] = useState<string | null>(null)

  const caseKey = (r: QualityCaseResult) => `${r.probe}/${r.suite}/${r.line}/${r.mode}`

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <CardTitle>{t('quality.results')}</CardTitle>
          <Select value={statusFilter} onValueChange={(v) => onFilterChange(v as CaseStatus | 'all')}>
            <SelectTrigger className="w-36">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{t('quality.filterAll')}</SelectItem>
              <SelectItem value="passed">{t('case.passed')}</SelectItem>
              <SelectItem value="rejected">{t('case.rejected')}</SelectItem>
              <SelectItem value="violated">{t('case.violated')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </CardHeader>
      <CardContent>
        {pending ? (
          <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
        ) : error != null ? (
          <p className="text-sm text-destructive">{errText(error)}</p>
        ) : results == null || results.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted-foreground">
            {active ? t('quality.resultsPending') : t('quality.resultsEmpty')}
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('quality.status')}</TableHead>
                <TableHead>{t('quality.suite')}</TableHead>
                <TableHead className="text-right">{t('quality.line')}</TableHead>
                <TableHead>{t('quality.mode')}</TableHead>
                <TableHead>{t('quality.reason')}</TableHead>
                <TableHead className="text-right">HTTP</TableHead>
                <TableHead className="text-right">{t('quality.latency')}</TableHead>
                <TableHead className="text-right">{t('quality.attempts')}</TableHead>
                <TableHead>{t('quality.message')}</TableHead>
                <TableHead className="w-8" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {results.map((r) => {
                const key = caseKey(r)
                const expandable = r.message !== '' || (r.arguments ?? '') !== ''
                const open = expanded === key
                return (
                  <Fragment key={key}>
                    <TableRow
                      className={expandable ? 'cursor-pointer' : undefined}
                      onClick={expandable ? () => setExpanded(open ? null : key) : undefined}
                    >
                      <TableCell>
                        <CaseStatusBadge status={r.status} />
                      </TableCell>
                      <TableCell>{r.suite}</TableCell>
                      <TableCell className="text-right tabular-nums">{r.line}</TableCell>
                      <TableCell>{t(`mode.${r.mode}` as DictKey)}</TableCell>
                      <TableCell className="text-muted-foreground">{r.selectionReason}</TableCell>
                      <TableCell className="text-right tabular-nums">
                        {r.httpStatus ?? '—'}
                      </TableCell>
                      <TableCell className="text-right tabular-nums">
                        {r.latencyMs != null ? `${r.latencyMs} ms` : '—'}
                      </TableCell>
                      <TableCell className="text-right tabular-nums">{r.attempts}</TableCell>
                      <TableCell className="max-w-64 truncate text-muted-foreground">
                        {r.message || '—'}
                      </TableCell>
                      <TableCell>
                        {expandable && (
                          <ChevronDown
                            className={`size-4 text-muted-foreground transition-transform ${open ? '' : '-rotate-90'}`}
                          />
                        )}
                      </TableCell>
                    </TableRow>
                    {open && (
                      <TableRow className="hover:bg-transparent">
                        <TableCell colSpan={10} className="bg-muted/30">
                          <div className="grid gap-3 py-2 text-sm">
                            {r.message !== '' && (
                              <div>
                                <p className="mb-1 text-xs text-muted-foreground">
                                  {t('quality.message')}
                                </p>
                                <p className="whitespace-pre-wrap break-all">{r.message}</p>
                              </div>
                            )}
                            {(r.arguments ?? '') !== '' && (
                              <div>
                                <p className="mb-1 text-xs text-muted-foreground">
                                  {t('quality.arguments')}
                                </p>
                                <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted p-3 font-mono text-xs">
                                  {r.arguments}
                                </pre>
                              </div>
                            )}
                          </div>
                        </TableCell>
                      </TableRow>
                    )}
                  </Fragment>
                )
              })}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}
