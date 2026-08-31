import { Fragment, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, ChevronDown, FileCode, FileJson, Loader2, RotateCcw, X } from 'lucide-react'
import { useNavigate, useParams } from 'react-router'
import { errText } from '@/components/channel-form-dialog'
import { CaseStatusBadge, gradeColor, TaskStatusBadge } from '@/components/quality-badges'
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
  type QualityReport,
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
    // 运行中 3s 轮询刷新统计卡（含待评估计数）；终结后靠 SSE 终态帧失效重拉
    refetchInterval: (q) =>
      q.state.data != null && !isTerminalStatus(q.state.data.status) ? 3000 : false,
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

  // 评分板仅终态任务可用（运行中评分会随采集漂移）；SSE 终态帧翻转 enabled 后自动拉取
  const terminal = status != null && isTerminalStatus(status)
  const report = useQuery({
    queryKey: ['quality-task-report', taskId],
    queryFn: () => qualityApi.getReport(taskId),
    enabled: task != null && terminal,
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
  // 重跑 = 用快照里的对象与参数重新走一遍创建流程：快照全新生成，
  // 渠道/模型已删或未定价等由服务端创建校验链拦截并透出文案
  const rerun = useMutation({
    mutationFn: qualityApi.createTask,
    onSuccess: (created) => {
      queryClient.invalidateQueries({ queryKey: ['quality-tasks'] })
      navigate(`/quality/${created.id}`)
    },
  })

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
  const canRerun = task.target.channelId != null && task.target.modelEntryId != null

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
        {terminal && (
          // 老数据快照可能缺 channelId/modelEntryId，此时禁用；Button 禁用态 pointer-events-none，
          // 提示挂在外层 span 上才能显示
          <span
            className="ml-auto"
            title={canRerun ? undefined : t('quality.rerunUnavailable')}
          >
            <Button
              variant="outline"
              disabled={!canRerun || rerun.isPending}
              onClick={() =>
                rerun.mutate({
                  // canRerun 已保证两 id 非空；第二批三协议上线后此处透传 target.protocol
                  channelId: task.target.channelId!,
                  modelEntryId: task.target.modelEntryId!,
                  probes: task.probes,
                  params: task.params,
                })
              }
            >
              {rerun.isPending ? <Loader2 className="animate-spin" /> : <RotateCcw />}
              {t('quality.rerun')}
            </Button>
          </span>
        )}
      </div>
      {rerun.isError && <p className="text-sm text-destructive">{errText(rerun.error)}</p>}

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

      {terminal && report.data != null && (
        <ScoreboardCard taskId={taskId} report={report.data} />
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

// ScoreboardCard 评分板：检查点 + 权重模型的展示层，原始判定以用例结果表为准
function ScoreboardCard({ taskId, report }: { taskId: string; report: QualityReport }) {
  const { t } = useI18n()
  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="grid gap-1.5">
            <CardTitle>{t('quality.scoreboard')}</CardTitle>
            <CardDescription>{t('quality.scoreboardDesc')}</CardDescription>
          </div>
          <div className="flex gap-2">
            <Button asChild variant="outline" size="sm">
              <a href={qualityApi.exportUrl(taskId, 'json')} download>
                <FileJson />
                {t('quality.exportJson')}
              </a>
            </Button>
            <Button asChild variant="outline" size="sm">
              <a href={qualityApi.exportUrl(taskId, 'junit')} download>
                <FileCode />
                {t('quality.exportJunit')}
              </a>
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className="grid gap-6">
        {report.incomplete && (
          <p className="text-sm text-amber-600 dark:text-amber-400">
            {t('quality.incompleteNote')}
          </p>
        )}
        {report.score == null ? (
          <p className="text-sm text-muted-foreground">{t('quality.noScore')}</p>
        ) : (
          <div className="flex items-baseline gap-3">
            <span className="text-sm text-muted-foreground">{t('quality.overallScore')}</span>
            <span
              className={`text-4xl font-semibold tabular-nums ${gradeColor[report.grade ?? ''] ?? ''}`}
            >
              {report.score.toFixed(1)}
            </span>
            {report.grade && (
              <span className={`text-2xl font-semibold ${gradeColor[report.grade] ?? ''}`}>
                {report.grade}
              </span>
            )}
          </div>
        )}
        <div className="grid gap-6 lg:grid-cols-2">
          {report.probes.map((p) => (
            <div key={p.probeId} className="grid content-start gap-2">
              <div className="flex items-baseline justify-between gap-3">
                <p className="text-sm font-medium">{p.probeName}</p>
                <span
                  className={`text-lg font-semibold tabular-nums ${p.score != null ? '' : 'text-muted-foreground'}`}
                >
                  {p.score != null ? p.score.toFixed(1) : '—'}
                </span>
              </div>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('quality.checkpoint')}</TableHead>
                    <TableHead className="text-right">{t('quality.weight')}</TableHead>
                    <TableHead className="text-right">{t('quality.passRatio')}</TableHead>
                    <TableHead className="w-40 text-right">{t('quality.score')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {p.checkpoints.map((c) => (
                    <TableRow key={c.id}>
                      <TableCell>{c.name}</TableCell>
                      <TableCell className="text-right tabular-nums">{c.weight}</TableCell>
                      <TableCell className="text-right tabular-nums">
                        {c.total > 0 ? `${c.passed}/${c.total}` : '—'}
                      </TableCell>
                      <TableCell>
                        {c.score != null ? (
                          <div className="flex items-center justify-end gap-2">
                            <Progress value={c.score} className="h-1.5 w-16" />
                            <span className="w-10 text-right tabular-nums">
                              {c.score.toFixed(1)}
                            </span>
                          </div>
                        ) : (
                          <p className="text-right text-muted-foreground">
                            {t('quality.unsampled')}
                          </p>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
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
  // 运行中才会出现待评估行，任务结束后该卡片与分桶列自动消失
  const showCollected = stats.collected > 0
  const top: [string, number, string][] = [
    [t('quality.total'), stats.total, ''],
    [t('case.passed'), stats.passed, 'text-green-600 dark:text-green-400'],
    [t('case.rejected'), stats.rejected, 'text-amber-600 dark:text-amber-400'],
    [t('case.violated'), stats.violated, 'text-red-600 dark:text-red-400'],
  ]
  if (showCollected) {
    top.push([t('case.collected'), stats.collected, 'text-muted-foreground'])
  }
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('quality.statsTitle')}</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-6">
        <div className={`grid grid-cols-2 gap-4 ${showCollected ? 'sm:grid-cols-5' : 'sm:grid-cols-4'}`}>
          {top.map(([label, value, cls]) => (
            <div key={label} className="rounded-lg border p-4">
              <p className="text-sm text-muted-foreground">{label}</p>
              <p className={`mt-1 text-2xl font-semibold tabular-nums ${cls}`}>{value}</p>
            </div>
          ))}
        </div>
        <div className="grid gap-6 lg:grid-cols-2">
          <BucketTable title={t('quality.byMode')} buckets={stats.byMode} translateName showCollected={showCollected} />
          <BucketTable title={t('quality.byReason')} buckets={stats.byReason} showCollected={showCollected} />
        </div>
      </CardContent>
    </Card>
  )
}

function BucketTable({
  title,
  buckets,
  translateName = false,
  showCollected = false,
}: {
  title: string
  buckets: TaskStatBucket[]
  translateName?: boolean
  showCollected?: boolean
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
            {showCollected && <TableHead className="text-right">{t('case.collected')}</TableHead>}
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
              {showCollected && <TableCell className="text-right tabular-nums">{b.collected}</TableCell>}
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
              <SelectItem value="collected">{t('case.collected')}</SelectItem>
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
