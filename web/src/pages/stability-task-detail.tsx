import { Fragment, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, FileJson, Loader2, RotateCcw, X } from 'lucide-react'
import { useNavigate, useParams } from 'react-router'
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from 'recharts'
import { errText } from '@/components/channel-form-dialog'
import { TaskStatusBadge } from '@/components/quality-badges'
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
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from '@/components/ui/chart'
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { isTerminalStatus, useTaskEvents } from '@/hooks/use-task-events'
import {
  stabilityApi,
  stabilityProbesApi,
  type StabilityMetrics,
  type StabilityReport,
  type StabilityStageMetric,
  type StabilityTask,
} from '@/lib/api'
import { type DictKey, useI18n } from '@/lib/i18n'

const OVERALL_STAGE = '__overall__'

// 三分位数配色语义化：越高分位越告警（蓝→琥珀→红）；主题内置 --chart-* 为灰阶不便区分，故直给色值
const CHART_CONFIG: ChartConfig = {
  p50: { label: 'p50', color: '#2563eb' },
  p95: { label: 'p95', color: '#d97706' },
  p99: { label: 'p99', color: '#dc2626' },
}

function formatDuration(ms: number): string {
  const s = Math.round(ms / 1000)
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`
}

// 数值格式化：错误率是 0..1 分数 → 百分比；吞吐保留一位小数
function pct(v: number): string {
  return `${(v * 100).toFixed(1)}%`
}
function num(v: number | undefined): string {
  return v == null ? '—' : v.toFixed(1)
}
function ms(v: number | undefined): string {
  return v == null ? '—' : `${Math.round(v)}`
}

export default function StabilityTaskDetailPage() {
  const { taskId = '' } = useParams()
  const { t } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const taskQ = useQuery({
    queryKey: ['stability-task', taskId],
    queryFn: () => stabilityApi.getTask(taskId),
    retry: false,
    refetchInterval: (q) =>
      q.state.data != null && !isTerminalStatus(q.state.data.status) ? 3000 : false,
  })
  const probes = useQuery({ queryKey: ['stability-probes'], queryFn: stabilityProbesApi.list })

  const task = taskQ.data
  const active = task != null && !isTerminalStatus(task.status)
  // 稳定性 SSE 走独立端点与 query key，泛化 hook 传入 basePath/invalidateKeys
  const live = useTaskEvents(taskId, active, {
    basePath: '/api/stability/tasks',
    invalidateKeys: [
      ['stability-task', taskId],
      ['stability-tasks'],
    ],
  })
  const status = live?.status ?? task?.status
  const done = live?.done ?? task?.progressDone ?? 0
  const total = live?.total ?? task?.progressTotal ?? 0

  // 指标报告仅终态可用（运行中分位数会随采集漂移）；SSE 终态帧翻转 enabled 后自动拉取
  const terminal = status != null && isTerminalStatus(status)
  const report = useQuery({
    queryKey: ['stability-task-metrics', taskId],
    queryFn: () => stabilityApi.getMetrics(taskId),
    enabled: task != null && terminal,
  })

  const [cancelOpen, setCancelOpen] = useState(false)
  const cancel = useMutation({
    mutationFn: () => stabilityApi.cancelTask(taskId),
    onSuccess: () => {
      setCancelOpen(false)
      queryClient.invalidateQueries({ queryKey: ['stability-task', taskId] })
      queryClient.invalidateQueries({ queryKey: ['stability-tasks'] })
    },
  })
  const closeCancelDialog = () => {
    setCancelOpen(false)
    cancel.reset()
  }
  const rerun = useMutation({
    mutationFn: stabilityApi.createTask,
    onSuccess: (created) => {
      queryClient.invalidateQueries({ queryKey: ['stability-tasks'] })
      navigate(`/stability/${created.id}`)
    },
  })

  if (taskQ.isPending) {
    return <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
  }
  if (taskQ.isError || task == null) {
    return (
      <div className="grid gap-4">
        <p className="text-sm text-destructive">{t('quality.notFound')}</p>
        <Button variant="outline" className="w-fit" onClick={() => navigate('/stability')}>
          <ArrowLeft />
          {t('common.back')}
        </Button>
      </div>
    )
  }

  const probeName = (id: string) => probes.data?.find((p) => p.id === id)?.name ?? id
  const pctDone = total > 0 ? (done / total) * 100 : 0
  const canRerun = task.target.channelId != null && task.target.modelEntryId != null

  return (
    <div className="grid gap-6">
      <div className="flex flex-wrap items-center gap-3">
        <Button
          variant="ghost"
          size="icon"
          className="size-8"
          title={t('common.back')}
          onClick={() => navigate('/stability')}
        >
          <ArrowLeft />
        </Button>
        <h1 className="text-2xl font-semibold">{t('stab.taskTitle')}</h1>
        {status && <TaskStatusBadge status={status} />}
        {(status === 'queued' || status === 'running') && (
          <Button variant="outline" className="ml-auto" onClick={() => setCancelOpen(true)}>
            <X />
            {t('quality.cancelTask')}
          </Button>
        )}
        {terminal && (
          <span className="ml-auto" title={canRerun ? undefined : t('quality.rerunUnavailable')}>
            <Button
              variant="outline"
              disabled={!canRerun || rerun.isPending}
              onClick={() =>
                rerun.mutate({
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
          <Progress value={pctDone} className="max-w-96" />
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

      {terminal &&
        (report.isPending ? (
          <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
        ) : report.data != null ? (
          <MetricsCard taskId={taskId} report={report.data} probeName={probeName} />
        ) : null)}
      <SnapshotCard task={task} probeName={probeName} />

      <Dialog open={cancelOpen} onOpenChange={(o) => { if (!o) closeCancelDialog() }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t('quality.cancelTask')}</DialogTitle>
            <DialogDescription>{t('stab.cancelConfirmDesc')}</DialogDescription>
          </DialogHeader>
          {cancel.isError && <p className="text-sm text-destructive">{errText(cancel.error)}</p>}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={closeCancelDialog}>
              {t('quality.keepRunning')}
            </Button>
            <Button variant="destructive" disabled={cancel.isPending} onClick={() => cancel.mutate()}>
              {cancel.isPending && <Loader2 className="animate-spin" />}
              {t('quality.cancelConfirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

// 稳定性指标卡：逐 probe 分区展示——阶梯折线（TTFT 分位数）+ 分档指标表 + 错误分类 + 脚注
function MetricsCard({
  taskId,
  report,
  probeName,
}: {
  taskId: string
  report: StabilityReport
  probeName: (id: string) => string
}) {
  const { t } = useI18n()

  // 按出现顺序归拢 probe，保留注册序即展示序
  const probeIds: string[] = []
  for (const s of report.stages) if (!probeIds.includes(s.probe)) probeIds.push(s.probe)

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="grid gap-1.5">
            <CardTitle>{t('stab.metrics')}</CardTitle>
            <CardDescription>{t('stab.metricsDesc')}</CardDescription>
          </div>
          <Button asChild variant="outline" size="sm">
            <a href={stabilityApi.exportUrl(taskId)} download>
              <FileJson />
              {t('quality.exportJson')}
            </a>
          </Button>
        </div>
      </CardHeader>
      <CardContent className="grid gap-8">
        {report.incomplete && (
          <p className="text-sm text-amber-600 dark:text-amber-400">{t('stab.incompleteNote')}</p>
        )}
        {report.stages.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t('stab.noMetrics')}</p>
        ) : (
          probeIds.map((pid) => (
            <ProbeSection
              key={pid}
              name={probeName(pid)}
              stages={report.stages
                .filter((s) => s.probe === pid && s.stage !== OVERALL_STAGE)
                .sort((a, b) => a.stageIndex - b.stageIndex)}
              overall={report.stages.find((s) => s.probe === pid && s.stage === OVERALL_STAGE)}
            />
          ))
        )}
        {report.footnotes && report.footnotes.length > 0 && (
          <div className="grid gap-1.5">
            <p className="text-sm font-medium">{t('stab.footnotesTitle')}</p>
            <ul className="list-inside list-disc text-xs text-muted-foreground">
              {report.footnotes.map((f, i) => (
                <li key={i}>{f}</li>
              ))}
            </ul>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function stageLabel(s: StabilityStageMetric): string {
  return s.metrics.concurrency != null ? String(s.metrics.concurrency) : s.stage
}

function ProbeSection({
  name,
  stages,
  overall,
}: {
  name: string
  stages: StabilityStageMetric[]
  overall?: StabilityStageMetric
}) {
  const { t } = useI18n()

  // 折线数据：仅有 TTFT 分位数的档入图；x 轴用并发数（缺省回退档标识）
  const chartData = stages
    .filter((s) => s.metrics.ttftMs != null)
    .map((s) => ({
      label: stageLabel(s),
      p50: s.metrics.ttftMs!.p50,
      p95: s.metrics.ttftMs!.p95,
      p99: s.metrics.ttftMs!.p99,
    }))
  const byErrorClass = overall?.metrics.byErrorClass ?? {}
  const errorClasses = Object.entries(byErrorClass).filter(([, n]) => n > 0)

  return (
    <div className="grid gap-4">
      <p className="text-sm font-medium">{name}</p>

      {chartData.length > 0 && (
        <div className="grid gap-2">
          <p className="text-xs text-muted-foreground">{t('stab.ladderChart')}</p>
          <ChartContainer config={CHART_CONFIG} className="aspect-[3/1] w-full">
            <LineChart data={chartData} margin={{ top: 8, right: 12, bottom: 4, left: 4 }}>
              <CartesianGrid vertical={false} />
              <XAxis
                dataKey="label"
                tickLine={false}
                axisLine={false}
                tickMargin={8}
                label={{ value: t('stab.concurrency'), position: 'insideBottom', offset: -2, fontSize: 11 }}
              />
              <YAxis width={44} tickLine={false} axisLine={false} tickMargin={4} />
              <ChartTooltip
                content={<ChartTooltipContent valueFormatter={(v) => `${v} ms`} />}
              />
              {(['p50', 'p95', 'p99'] as const).map((k) => (
                <Line
                  key={k}
                  type="monotone"
                  dataKey={k}
                  stroke={`var(--color-${k})`}
                  strokeWidth={2}
                  dot={{ r: 3 }}
                  isAnimationActive={false}
                />
              ))}
            </LineChart>
          </ChartContainer>
        </div>
      )}

      <div className="grid gap-2">
        <p className="text-xs text-muted-foreground">{t('stab.stageTable')}</p>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('stab.concurrency')}</TableHead>
              <TableHead className="text-right">{t('stab.requests')}</TableHead>
              <TableHead className="text-right">{t('stab.errorRate')}</TableHead>
              <TableHead className="text-right">{t('stab.throughput')}</TableHead>
              <TableHead className="text-right">{t('stab.tokensPerSec')}</TableHead>
              <TableHead className="text-right">{t('stab.ttft')} p50</TableHead>
              <TableHead className="text-right">p95</TableHead>
              <TableHead className="text-right">p99</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {stages.map((s) => (
              <MetricRow key={s.stage} label={stageLabel(s)} m={s.metrics} />
            ))}
            {overall && (
              <MetricRow key="__overall__" label={t('stab.overall')} m={overall.metrics} bold />
            )}
          </TableBody>
        </Table>
      </div>

      {errorClasses.length > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs text-muted-foreground">{t('stab.byErrorClass')}：</span>
          {errorClasses.map(([cls, n]) => (
            <Badge key={cls} variant="outline" className="font-normal">
              {t(`errClass.${cls}` as DictKey)} · {n}
            </Badge>
          ))}
        </div>
      )}
    </div>
  )
}

function MetricRow({ label, m, bold }: { label: string; m: StabilityMetrics; bold?: boolean }) {
  return (
    <TableRow className={bold ? 'font-medium' : undefined}>
      <TableCell>{label}</TableCell>
      <TableCell className="text-right tabular-nums">{m.requests}</TableCell>
      <TableCell className="text-right tabular-nums">{pct(m.errorRate)}</TableCell>
      <TableCell className="text-right tabular-nums">{num(m.throughputRps)}</TableCell>
      <TableCell className="text-right tabular-nums">{num(m.tokensPerSec)}</TableCell>
      <TableCell className="text-right tabular-nums">{ms(m.ttftMs?.p50)}</TableCell>
      <TableCell className="text-right tabular-nums">{ms(m.ttftMs?.p95)}</TableCell>
      <TableCell className="text-right tabular-nums">{ms(m.ttftMs?.p99)}</TableCell>
    </TableRow>
  )
}

// SnapshotCard 参数快照卡：检测对象与任务参数在创建时定格，展示历史事实（含实测协议）
function SnapshotCard({
  task,
  probeName,
}: {
  task: StabilityTask
  probeName: (id: string) => string
}) {
  const { t } = useI18n()
  const tg = task.target
  const p = task.params
  const startedAt = task.startedAt ? new Date(task.startedAt) : null
  const finishedAt = task.finishedAt ? new Date(task.finishedAt) : null

  const rows: [string, React.ReactNode][] = [
    [t('quality.channel'), tg.channelName],
    [t('channels.baseUrl'), tg.baseUrl],
    [t('quality.model'), tg.model],
    [
      t('stab.protocol'),
      p.protocol ? (
        <Badge key="protocol" variant="outline">
          {t(`proto.${p.protocol}` as DictKey)}
        </Badge>
      ) : (
        '—'
      ),
    ],
    [t('quality.probes'), task.probes.map(probeName).join('、')],
    [
      t('quality.paramsLabel'),
      `${t('stab.ladder')} [${p.concurrencyLadder.join(', ')}] · ${t('stab.requestsPerStage')} ${p.requestsPerStage} · ${t('stab.warmupPerStage')} ${p.warmupPerStage} · ${t('stab.ladderMaxTokens')} ${p.ladderMaxTokens}`,
    ],
    [
      t('stab.maxTotalRequests'),
      `${p.maxTotalRequests} · ${t('stab.maxTotalTokens')} ${p.maxTotalTokens} · ${t('stab.requestTimeout')} ${p.requestTimeoutMs}`,
    ],
    [
      t('quality.createdAt'),
      `${new Date(task.createdAt).toLocaleString()}${task.createdBy ? ` · ${task.createdBy}` : ''}`,
    ],
  ]
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
