import { useEffect, useState } from 'react'
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, ChevronLeft, ChevronRight, Loader2, Play, X } from 'lucide-react'
import { useNavigate } from 'react-router'
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
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
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
import { isTerminalStatus } from '@/hooks/use-task-events'
import {
  channelsApi,
  stabilityApi,
  stabilityProbesApi,
  type Protocol,
  type StabilityProbeInfo,
  type StabilityTask,
  type TaskStatus,
} from '@/lib/api'
import { type DictKey, useI18n } from '@/lib/i18n'

const PAGE_SIZE = 20
const TASK_STATUSES: TaskStatus[] = ['queued', 'running', 'succeeded', 'failed', 'canceled']
const ALL_PROTOCOLS: Protocol[] = ['openai_chat', 'openai_responses', 'anthropic_messages']

// 某 probe 对某协议是否适用：protocols 为空 = 三协议通用
function probeSupports(p: StabilityProbeInfo, proto: Protocol): boolean {
  return p.protocols.length === 0 || p.protocols.includes(proto)
}

// 解析并发档文本为正整数数组（逗号分隔）；非法 token 直接丢弃，单值上限 512 对齐后端校验
function parseLadder(text: string): number[] {
  return text
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s !== '')
    .map(Number)
    .filter((n) => Number.isInteger(n) && n >= 1 && n <= 512)
}

export default function StabilityPage() {
  const { t } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const [channelId, setChannelId] = useState('')
  const [modelId, setModelId] = useState('')
  // null = 默认全选（检测项列表加载后）；勾选过一次即接管
  const [pickedProbes, setPickedProbes] = useState<string[] | null>(null)
  const [protocol, setProtocol] = useState('')
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [ladderText, setLadderText] = useState('1,2,4,8,16')
  const [requestsPerStage, setRequestsPerStage] = useState('20')
  const [warmupPerStage, setWarmupPerStage] = useState('2')
  const [ladderMaxTokens, setLadderMaxTokens] = useState('64')
  const [maxTotalRequests, setMaxTotalRequests] = useState('2000')
  const [maxTotalTokens, setMaxTotalTokens] = useState('2000000')
  const [requestTimeoutMs, setRequestTimeoutMs] = useState('60000')
  const [filterStatus, setFilterStatus] = useState('all')
  const [filterChannel, setFilterChannel] = useState('all')
  const [page, setPage] = useState(1)

  const channels = useQuery({ queryKey: ['channels'], queryFn: channelsApi.list })
  const probes = useQuery({ queryKey: ['stability-probes'], queryFn: stabilityProbesApi.list })
  const channelDetail = useQuery({
    queryKey: ['channels', channelId],
    queryFn: () => channelsApi.get(channelId),
    enabled: channelId !== '',
  })
  const tasks = useQuery({
    queryKey: ['stability-tasks', filterStatus, filterChannel, page],
    queryFn: () =>
      stabilityApi.listTasks({
        limit: PAGE_SIZE,
        offset: (page - 1) * PAGE_SIZE,
        status: filterStatus === 'all' ? undefined : (filterStatus as TaskStatus),
        channelId: filterChannel === 'all' ? undefined : filterChannel,
      }),
    placeholderData: keepPreviousData,
    refetchInterval: (q) =>
      q.state.data?.items.some((task) => !isTerminalStatus(task.status)) ? 3000 : false,
  })
  const totalPages = Math.max(1, Math.ceil((tasks.data?.total ?? 0) / PAGE_SIZE))

  const create = useMutation({
    mutationFn: stabilityApi.createTask,
    onSuccess: (task) => {
      queryClient.invalidateQueries({ queryKey: ['stability-tasks'] })
      navigate(`/stability/${task.id}`)
    },
  })
  const cancel = useMutation({
    mutationFn: stabilityApi.cancelTask,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['stability-tasks'] }),
  })

  const selectableChannels = (channels.data ?? []).filter((c) => !c.disabled)
  const models = channelDetail.data?.models ?? []
  const model = models.find((m) => m.id === modelId)
  const currencySymbol = channelDetail.data?.currency === 'CNY' ? '¥' : '$'
  const checked = pickedProbes ?? (probes.data ?? []).map((p) => p.id)
  const checkedProbes = (probes.data ?? []).filter((p) => checked.includes(p.id))

  // 可选协议 = 渠道声明协议 ∩ 所有已选 probe 都支持的协议
  const channelProtocols = (channelDetail.data?.protocols ?? []) as Protocol[]
  const allowedProtocols = ALL_PROTOCOLS.filter(
    (proto) =>
      channelProtocols.includes(proto) && checkedProbes.every((p) => probeSupports(p, proto)),
  )
  const allowedKey = allowedProtocols.join(',')
  // 可选协议集变化时把选择收敛到合法项（默认首项），无交集则清空
  useEffect(() => {
    if (allowedProtocols.length === 0) {
      if (protocol !== '') setProtocol('')
    } else if (!allowedProtocols.includes(protocol as Protocol)) {
      setProtocol(allowedProtocols[0])
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [allowedKey])

  const toggleProbe = (id: string) => {
    setPickedProbes(checked.includes(id) ? checked.filter((p) => p !== id) : [...checked, id])
  }

  const ladderNums = parseLadder(ladderText)
  const rps = Number(requestsPerStage) || 0
  const warmup = Number(warmupPerStage) || 0
  const maxTok = Number(ladderMaxTokens) || 0
  const totalReqCap = Number(maxTotalRequests) || 0
  const totalTokCap = Number(maxTotalTokens) || 0

  // 最坏预估请求数：各 probe 按实选参数求和，再被总请求硬闸钳制。
  // concurrency_ladder 有精确公式；其它 probe（Phase 2/3）暂用服务端默认估算兜底。
  const estPerProbe = (p: StabilityProbeInfo): number =>
    p.id === 'concurrency_ladder' ? ladderNums.length * (rps + warmup) : p.estRequests
  const estRequestsRaw = checkedProbes.reduce((sum, p) => sum + estPerProbe(p), 0)
  const estRequests = Math.min(estRequestsRaw, totalReqCap || estRequestsRaw)
  // 预估费用上界：只算输出侧（阶梯顶格 prompt 输入极小），token 上界受总 token 硬闸钳制
  const estTokens = Math.min(estRequests * maxTok, totalTokCap || estRequests * maxTok)
  const estCost =
    model?.outputPrice != null ? (estTokens * model.outputPrice) / 1_000_000 : null

  const paramsValid = ladderNums.length > 0 && rps >= 1 && maxTok >= 1

  const submit = () => {
    create.mutate({
      channelId,
      modelEntryId: modelId,
      probes: checked,
      params: {
        protocol: protocol as Protocol,
        concurrencyLadder: ladderNums,
        requestsPerStage: rps,
        warmupPerStage: warmup,
        ladderMaxTokens: maxTok,
        maxTotalRequests: totalReqCap,
        maxTotalTokens: totalTokCap,
        requestTimeoutMs: Number(requestTimeoutMs) || 60000,
      },
    })
  }

  const probeName = (id: string) => probes.data?.find((p) => p.id === id)?.name ?? id

  return (
    <div className="grid gap-6">
      <div>
        <h1 className="text-2xl font-semibold">{t('nav.stability')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t('stability.desc')}</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{t('quality.target')}</CardTitle>
          <CardDescription>{t('quality.targetDesc')}</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex flex-wrap items-end gap-3">
            <div className="grid gap-2">
              <Label>{t('quality.channel')}</Label>
              <Select
                value={channelId}
                onValueChange={(v) => {
                  setChannelId(v)
                  setModelId('')
                }}
                disabled={selectableChannels.length === 0}
              >
                <SelectTrigger className="w-56">
                  <SelectValue placeholder={t('quality.selectChannel')} />
                </SelectTrigger>
                <SelectContent>
                  {selectableChannels.map((c) => (
                    <SelectItem key={c.id} value={c.id}>
                      {c.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="grid gap-2">
              <Label>{t('quality.model')}</Label>
              <Select
                value={modelId}
                onValueChange={setModelId}
                disabled={channelId === '' || models.length === 0}
              >
                <SelectTrigger className="w-56">
                  <SelectValue placeholder={t('quality.selectModel')} />
                </SelectTrigger>
                <SelectContent>
                  {models.map((m) => (
                    <SelectItem key={m.id} value={m.id}>
                      {m.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            {selectableChannels.length === 0 && !channels.isPending && (
              <p className="text-sm text-muted-foreground">{t('quality.noChannels')}</p>
            )}
            {channelId !== '' && channelDetail.isSuccess && models.length === 0 && (
              <p className="text-sm text-muted-foreground">{t('quality.noModels')}</p>
            )}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('quality.probes')}</CardTitle>
          <CardDescription>{t('stab.probesDesc')}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          {probes.isPending ? (
            <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
          ) : (
            <div className="grid gap-2">
              {(probes.data ?? []).map((p) => (
                <label
                  key={p.id}
                  className="flex cursor-pointer items-start gap-3 rounded-md border p-3"
                >
                  <Checkbox
                    checked={checked.includes(p.id)}
                    onCheckedChange={() => toggleProbe(p.id)}
                    className="mt-0.5"
                  />
                  <div className="grid gap-1">
                    <span className="flex flex-wrap items-center gap-2 text-sm font-medium">
                      {p.name}
                      <span className="text-xs font-normal text-muted-foreground">
                        ≈ {p.estRequests} {t('quality.requests')}
                      </span>
                      <span className="flex flex-wrap gap-1">
                        {(p.protocols.length === 0 ? ALL_PROTOCOLS : p.protocols).map((proto) => (
                          <Badge key={proto} variant="outline" className="font-normal">
                            {t(`proto.${proto}` as DictKey)}
                          </Badge>
                        ))}
                      </span>
                    </span>
                    <span className="text-xs text-muted-foreground">{p.description}</span>
                  </div>
                </label>
              ))}
            </div>
          )}

          <div className="flex flex-wrap items-end gap-3">
            <div className="grid gap-2">
              <Label>{t('stab.protocol')}</Label>
              <Select
                value={protocol}
                onValueChange={setProtocol}
                disabled={allowedProtocols.length === 0}
              >
                <SelectTrigger className="w-56">
                  <SelectValue placeholder={t('stab.protocol')} />
                </SelectTrigger>
                <SelectContent>
                  {allowedProtocols.map((proto) => (
                    <SelectItem key={proto} value={proto}>
                      {t(`proto.${proto}` as DictKey)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <p className="pb-2 text-xs text-muted-foreground">
              {channelId !== '' && checkedProbes.length > 0 && allowedProtocols.length === 0
                ? t('stab.noProtocol')
                : t('stab.protocolHint')}
            </p>
          </div>

          <div className="grid gap-3">
            <button
              type="button"
              className="flex w-fit items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
              onClick={() => setShowAdvanced((v) => !v)}
            >
              <ChevronDown
                className={`size-4 transition-transform ${showAdvanced ? '' : '-rotate-90'}`}
              />
              {t('quality.advanced')}
            </button>
            {showAdvanced && (
              <div className="flex flex-wrap gap-3">
                <div className="grid gap-2">
                  <Label htmlFor="s-ladder">{t('stab.ladder')}</Label>
                  <Input
                    id="s-ladder"
                    className="w-48"
                    placeholder={t('stab.ladderHint')}
                    value={ladderText}
                    onChange={(e) => setLadderText(e.target.value)}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="s-rps">{t('stab.requestsPerStage')}</Label>
                  <Input
                    id="s-rps"
                    type="number"
                    min={1}
                    max={1000}
                    className="w-36"
                    value={requestsPerStage}
                    onChange={(e) => setRequestsPerStage(e.target.value)}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="s-warmup">{t('stab.warmupPerStage')}</Label>
                  <Input
                    id="s-warmup"
                    type="number"
                    min={0}
                    max={100}
                    className="w-36"
                    value={warmupPerStage}
                    onChange={(e) => setWarmupPerStage(e.target.value)}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="s-maxtok">{t('stab.ladderMaxTokens')}</Label>
                  <Input
                    id="s-maxtok"
                    type="number"
                    min={1}
                    max={4096}
                    className="w-36"
                    value={ladderMaxTokens}
                    onChange={(e) => setLadderMaxTokens(e.target.value)}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="s-maxreq">{t('stab.maxTotalRequests')}</Label>
                  <Input
                    id="s-maxreq"
                    type="number"
                    min={1}
                    className="w-36"
                    value={maxTotalRequests}
                    onChange={(e) => setMaxTotalRequests(e.target.value)}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="s-maxtoktotal">{t('stab.maxTotalTokens')}</Label>
                  <Input
                    id="s-maxtoktotal"
                    type="number"
                    min={1}
                    className="w-40"
                    value={maxTotalTokens}
                    onChange={(e) => setMaxTotalTokens(e.target.value)}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="s-timeout">{t('stab.requestTimeout')}</Label>
                  <Input
                    id="s-timeout"
                    type="number"
                    min={1000}
                    max={600000}
                    className="w-36"
                    value={requestTimeoutMs}
                    onChange={(e) => setRequestTimeoutMs(e.target.value)}
                  />
                </div>
              </div>
            )}
          </div>

          {create.isError && <p className="text-sm text-destructive">{errText(create.error)}</p>}
          <div className="flex flex-wrap items-center gap-4">
            <Button
              onClick={submit}
              disabled={
                create.isPending ||
                channelId === '' ||
                modelId === '' ||
                checked.length === 0 ||
                protocol === '' ||
                !paramsValid
              }
            >
              {create.isPending ? <Loader2 className="animate-spin" /> : <Play />}
              {create.isPending ? t('quality.starting') : t('quality.launch')}
            </Button>
            <span className="text-sm text-muted-foreground">
              {t('quality.estRequests')}：{estRequests}
            </span>
            {estCost != null && (
              <span className="text-sm text-muted-foreground">
                {t('stab.estCost')}：≤ {currencySymbol}
                {estCost.toFixed(estCost < 1 ? 4 : 2)}
              </span>
            )}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('quality.history')}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center gap-2">
            <Select
              value={filterStatus}
              onValueChange={(v) => {
                setFilterStatus(v)
                setPage(1)
              }}
            >
              <SelectTrigger className="w-36">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t('quality.filterAllStatuses')}</SelectItem>
                {TASK_STATUSES.map((s) => (
                  <SelectItem key={s} value={s}>
                    {t(`taskStatus.${s}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {/* 渠道筛选列表不过滤停用渠道：历史任务可能属于已停用渠道 */}
            <Select
              value={filterChannel}
              onValueChange={(v) => {
                setFilterChannel(v)
                setPage(1)
              }}
            >
              <SelectTrigger className="w-48">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t('quality.filterAllChannels')}</SelectItem>
                {(channels.data ?? []).map((c) => (
                  <SelectItem key={c.id} value={c.id}>
                    {c.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {tasks.isPending ? (
            <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
          ) : tasks.isError ? (
            <p className="text-sm text-destructive">{errText(tasks.error)}</p>
          ) : tasks.data.items.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              {t('stab.historyEmpty')}
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('quality.status')}</TableHead>
                  <TableHead>{t('quality.target')}</TableHead>
                  <TableHead>{t('quality.probes')}</TableHead>
                  <TableHead className="w-44">{t('quality.progress')}</TableHead>
                  <TableHead>{t('stab.protocol')}</TableHead>
                  <TableHead>{t('quality.createdBy')}</TableHead>
                  <TableHead>{t('quality.createdAt')}</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {tasks.data.items.map((task) => (
                  <TaskRow
                    key={task.id}
                    task={task}
                    probeName={probeName}
                    canceling={cancel.isPending && cancel.variables === task.id}
                    onCancel={() => cancel.mutate(task.id)}
                  />
                ))}
              </TableBody>
            </Table>
          )}
          {totalPages > 1 && (
            <div className="flex items-center justify-end gap-2">
              <Button
                variant="outline"
                size="icon"
                className="size-7"
                title={t('quality.prevPage')}
                disabled={page <= 1}
                onClick={() => setPage(page - 1)}
              >
                <ChevronLeft />
              </Button>
              <span className="text-sm tabular-nums text-muted-foreground">
                {t('quality.pagePrefix')}
                {page} / {totalPages}
                {t('quality.pageSuffix')}
              </span>
              <Button
                variant="outline"
                size="icon"
                className="size-7"
                title={t('quality.nextPage')}
                disabled={page >= totalPages}
                onClick={() => setPage(page + 1)}
              >
                <ChevronRight />
              </Button>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function TaskRow({
  task,
  probeName,
  canceling,
  onCancel,
}: {
  task: StabilityTask
  probeName: (id: string) => string
  canceling: boolean
  onCancel: () => void
}) {
  const { t } = useI18n()
  const navigate = useNavigate()
  const pct = task.progressTotal > 0 ? (task.progressDone / task.progressTotal) * 100 : 0
  const proto = task.params.protocol

  return (
    <TableRow className="cursor-pointer" onClick={() => navigate(`/stability/${task.id}`)}>
      <TableCell>
        <TaskStatusBadge status={task.status} />
      </TableCell>
      <TableCell className="font-medium">
        {task.target.channelName}
        <span className="text-muted-foreground"> · {task.target.model}</span>
      </TableCell>
      <TableCell className="max-w-56 truncate text-muted-foreground">
        {task.probes.map(probeName).join('、')}
      </TableCell>
      <TableCell>
        {task.status === 'queued' ? (
          <span className="text-muted-foreground">—</span>
        ) : (
          <div className="flex items-center gap-2">
            <Progress value={pct} className="w-20" />
            <span className="text-xs tabular-nums text-muted-foreground">
              {task.progressDone}/{task.progressTotal}
            </span>
          </div>
        )}
      </TableCell>
      <TableCell className="text-muted-foreground">
        {proto ? t(`proto.${proto}` as DictKey) : '—'}
      </TableCell>
      <TableCell className="text-muted-foreground">{task.createdBy ?? '—'}</TableCell>
      <TableCell className="text-muted-foreground">
        {new Date(task.createdAt).toLocaleString()}
      </TableCell>
      <TableCell>
        {task.status === 'queued' && (
          <Button
            variant="ghost"
            size="icon"
            className="size-7"
            title={t('quality.cancelTask')}
            disabled={canceling}
            onClick={(e) => {
              e.stopPropagation()
              onCancel()
            }}
          >
            {canceling ? <Loader2 className="animate-spin" /> : <X />}
          </Button>
        )}
      </TableCell>
    </TableRow>
  )
}
