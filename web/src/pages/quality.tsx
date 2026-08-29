import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, Loader2, Play, X } from 'lucide-react'
import { useNavigate } from 'react-router'
import { errText } from '@/components/channel-form-dialog'
import { TaskStatusBadge, CostTierBadge } from '@/components/quality-badges'
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
import { channelsApi, probesApi, qualityApi, type QualityTask } from '@/lib/api'
import { useI18n } from '@/lib/i18n'

export default function QualityPage() {
  const { t } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const [channelId, setChannelId] = useState('')
  const [modelId, setModelId] = useState('')
  // null = 默认全选（检测项列表加载后）；勾选过一次即接管
  const [pickedProbes, setPickedProbes] = useState<string[] | null>(null)
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [concurrency, setConcurrency] = useState('4')
  const [reruns, setReruns] = useState('2')
  const [maxCases, setMaxCases] = useState('')

  const channels = useQuery({ queryKey: ['channels'], queryFn: channelsApi.list })
  const probes = useQuery({ queryKey: ['probes'], queryFn: probesApi.list })
  const channelDetail = useQuery({
    queryKey: ['channels', channelId],
    queryFn: () => channelsApi.get(channelId),
    enabled: channelId !== '',
  })
  // 有未终结任务时轮询列表；进度实时性靠详情页 SSE，列表 3s 粗粒度足够
  const tasks = useQuery({
    queryKey: ['quality-tasks'],
    queryFn: () => qualityApi.listTasks(),
    refetchInterval: (q) =>
      q.state.data?.items.some((task) => !isTerminalStatus(task.status)) ? 3000 : false,
  })

  const create = useMutation({
    mutationFn: qualityApi.createTask,
    onSuccess: (task) => {
      queryClient.invalidateQueries({ queryKey: ['quality-tasks'] })
      navigate(`/quality/${task.id}`)
    },
  })
  const cancel = useMutation({
    mutationFn: qualityApi.cancelTask,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['quality-tasks'] }),
  })

  const selectableChannels = (channels.data ?? []).filter((c) => !c.disabled)
  const models = channelDetail.data?.models ?? []
  const checked = pickedProbes ?? (probes.data ?? []).map((p) => p.id)

  const toggleProbe = (id: string) => {
    setPickedProbes(checked.includes(id) ? checked.filter((p) => p !== id) : [...checked, id])
  }

  const maxCasesNum = maxCases === '' ? undefined : Number(maxCases)
  // 受用例数上限影响的检测项按 min(上限, 全量) 估算；固定请求矩阵的按全量
  const estRequests = (probes.data ?? [])
    .filter((p) => checked.includes(p.id))
    .reduce(
      (sum, p) =>
        sum +
        (p.supportsMaxCases ? Math.min(maxCasesNum || p.caseCount, p.caseCount) : p.caseCount) *
          p.requestsPerCase,
      0,
    )

  const submit = () => {
    create.mutate({
      channelId,
      modelEntryId: modelId,
      probes: checked,
      params: {
        concurrency: Number(concurrency),
        reruns: Number(reruns),
        ...(maxCasesNum != null ? { maxCases: maxCasesNum } : {}),
      },
    })
  }

  const probeName = (id: string) => probes.data?.find((p) => p.id === id)?.name ?? id

  return (
    <div className="grid gap-6">
      <div>
        <h1 className="text-2xl font-semibold">{t('nav.quality')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t('quality.desc')}</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{t('quality.launch')}</CardTitle>
          <CardDescription>{t('quality.launchDesc')}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
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

          <div className="grid gap-2">
            <Label>{t('quality.probes')}</Label>
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
                        <CostTierBadge tier={p.costTier} />
                        <span className="text-xs font-normal text-muted-foreground">
                          {p.caseCount} {t('quality.cases')}
                          {p.requestsPerCase > 1 &&
                            ` · ${p.caseCount * p.requestsPerCase} ${t('quality.requests')}`}
                        </span>
                        {p.needsPricing && (
                          <span className="text-xs font-normal text-muted-foreground">
                            · {t('quality.needsPricing')}
                          </span>
                        )}
                        {p.needsControl && (
                          <span className="text-xs font-normal text-muted-foreground">
                            · {t('quality.needsControl')}
                          </span>
                        )}
                      </span>
                      <span className="text-xs text-muted-foreground">{p.description}</span>
                    </div>
                  </label>
                ))}
              </div>
            )}
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
                  <Label htmlFor="q-conc">{t('quality.concurrency')}</Label>
                  <Input
                    id="q-conc"
                    type="number"
                    min={1}
                    max={16}
                    className="w-36"
                    value={concurrency}
                    onChange={(e) => setConcurrency(e.target.value)}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="q-reruns">{t('quality.reruns')}</Label>
                  <Input
                    id="q-reruns"
                    type="number"
                    min={0}
                    max={5}
                    className="w-36"
                    value={reruns}
                    onChange={(e) => setReruns(e.target.value)}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="q-max">{t('quality.maxCases')}</Label>
                  <Input
                    id="q-max"
                    type="number"
                    min={1}
                    className="w-36"
                    placeholder={t('quality.maxCasesHint')}
                    value={maxCases}
                    onChange={(e) => setMaxCases(e.target.value)}
                  />
                </div>
              </div>
            )}
          </div>

          {create.isError && <p className="text-sm text-destructive">{errText(create.error)}</p>}
          <div className="flex items-center gap-4">
            <Button
              onClick={submit}
              disabled={
                create.isPending || channelId === '' || modelId === '' || checked.length === 0
              }
            >
              {create.isPending ? <Loader2 className="animate-spin" /> : <Play />}
              {create.isPending ? t('quality.starting') : t('quality.launch')}
            </Button>
            <span className="text-sm text-muted-foreground">
              {t('quality.estRequests')}：{estRequests}
            </span>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('quality.history')}</CardTitle>
        </CardHeader>
        <CardContent>
          {tasks.isPending ? (
            <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
          ) : tasks.isError ? (
            <p className="text-sm text-destructive">{errText(tasks.error)}</p>
          ) : tasks.data.items.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              {t('quality.historyEmpty')}
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('quality.status')}</TableHead>
                  <TableHead>{t('quality.target')}</TableHead>
                  <TableHead>{t('quality.probes')}</TableHead>
                  <TableHead className="w-44">{t('quality.progress')}</TableHead>
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
  task: QualityTask
  probeName: (id: string) => string
  canceling: boolean
  onCancel: () => void
}) {
  const { t } = useI18n()
  const navigate = useNavigate()
  const pct = task.progressTotal > 0 ? (task.progressDone / task.progressTotal) * 100 : 0

  return (
    <TableRow className="cursor-pointer" onClick={() => navigate(`/quality/${task.id}`)}>
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
