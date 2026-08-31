import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router'
import { ConnStatus } from '@/components/channel-form-dialog'
import { ScoreText, TaskStatusBadge } from '@/components/quality-badges'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  channelsApi,
  overviewApi,
  type ConnectivityHistoryPoint,
  type OverviewChannel,
  type OverviewModelScore,
} from '@/lib/api'
import { type DictKey, useI18n } from '@/lib/i18n'
import { cn } from '@/lib/utils'

// 协议 → 曲线/图例配色（与协议 Badge 无关，仅 sparkline 专用）
const protoStroke: Record<string, string> = {
  openai_chat: 'stroke-blue-500',
  openai_responses: 'stroke-violet-500',
  anthropic_messages: 'stroke-orange-500',
}
const protoDot: Record<string, string> = {
  openai_chat: 'bg-blue-500',
  openai_responses: 'bg-violet-500',
  anthropic_messages: 'bg-orange-500',
}

// LatencySparkline 裸 SVG 延迟曲线：每协议一组折线段，失败点（无 ttft）断线留白
function LatencySparkline({ points }: { points: ConnectivityHistoryPoint[] }) {
  const { t } = useI18n()

  if (points.length === 0) {
    return <p className="text-xs text-muted-foreground">{t('dash.noLatency')}</p>
  }

  const W = 240
  const H = 48
  const times = points.map((p) => new Date(p.testedAt).getTime())
  const tMin = Math.min(...times)
  const span = Math.max(Math.max(...times) - tMin, 1)
  const maxTtft = Math.max(...points.map((p) => p.ttftMs ?? 0), 1)

  // 按协议分组（接口按 tested_at 升序返回，组内天然有序）
  const byProto = new Map<string, ConnectivityHistoryPoint[]>()
  for (const p of points) {
    const list = byProto.get(p.protocol) ?? []
    list.push(p)
    byProto.set(p.protocol, list)
  }

  const x = (iso: string) => ((new Date(iso).getTime() - tMin) / span) * W
  // 上下各留 2px，避免峰值贴边被裁
  const y = (ttft: number) => H - 2 - (ttft / maxTtft) * (H - 4)

  return (
    <div className="grid gap-1.5">
      <svg
        viewBox={`0 0 ${W} ${H}`}
        preserveAspectRatio="none"
        className="h-12 w-full rounded-sm bg-muted/40"
        role="img"
      >
        {[...byProto.entries()].map(([proto, list]) => {
          // 失败点切断折线：连续可用点为一段，单点段画圆
          const segments: ConnectivityHistoryPoint[][] = []
          let cur: ConnectivityHistoryPoint[] = []
          for (const p of list) {
            if (p.ttftMs == null) {
              if (cur.length > 0) segments.push(cur)
              cur = []
            } else {
              cur.push(p)
            }
          }
          if (cur.length > 0) segments.push(cur)
          const stroke = protoStroke[proto] ?? 'stroke-muted-foreground'
          return segments.map((seg, i) =>
            seg.length === 1 ? (
              <circle
                key={`${proto}-${i}`}
                cx={x(seg[0].testedAt)}
                cy={y(seg[0].ttftMs ?? 0)}
                r={2}
                className={`fill-none ${stroke}`}
                strokeWidth={1.5}
                vectorEffect="non-scaling-stroke"
              />
            ) : (
              <polyline
                key={`${proto}-${i}`}
                points={seg.map((p) => `${x(p.testedAt)},${y(p.ttftMs ?? 0)}`).join(' ')}
                fill="none"
                className={stroke}
                strokeWidth={1.5}
                vectorEffect="non-scaling-stroke"
              />
            ),
          )
        })}
      </svg>
      <div className="flex flex-wrap gap-x-3 gap-y-1">
        {[...byProto.entries()].map(([proto, list]) => {
          const latest = list[list.length - 1]
          return (
            <span key={proto} className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
              <span className={`size-2 shrink-0 rounded-full ${protoDot[proto] ?? 'bg-muted-foreground'}`} />
              {t(`proto.${proto}` as DictKey)}
              <span className="tabular-nums whitespace-nowrap">
                {latest.ttftMs != null ? `${latest.ttftMs} ms` : '—'}
              </span>
            </span>
          )
        })}
      </div>
    </div>
  )
}

// ScoreZone 质量得分格：只展示一个模型（可切换），选择由 ChannelCard 托管（footer 也要用）
function ScoreZone({
  models,
  selected,
  onSelect,
}: {
  models: OverviewModelScore[]
  selected: OverviewModelScore | undefined
  onSelect: (model: string) => void
}) {
  const { t } = useI18n()

  if (!selected) {
    return <p className="text-xs text-muted-foreground">{t('dash.noModels')}</p>
  }

  return (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <ScoreText score={selected.score} className="text-3xl" />
        {selected.taskStatus !== 'succeeded' && <TaskStatusBadge status={selected.taskStatus} />}
      </div>
      {models.length > 1 ? (
        <Select value={selected.model} onValueChange={onSelect}>
          <SelectTrigger size="sm" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {models.map((m) => (
              <SelectItem key={m.model} value={m.model}>
                {m.model}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      ) : (
        <p className="truncate text-xs text-muted-foreground" title={selected.model}>
          {selected.model}
        </p>
      )}
    </>
  )
}

// ChannelCard 渠道卡片：头部基本信息 / 中段左右分区（得分｜延迟）/ 底栏元信息
function ChannelCard({ channel }: { channel: OverviewChannel }) {
  const { t } = useI18n()
  const navigate = useNavigate()
  const history = useQuery({
    queryKey: ['overview', 'history', channel.id],
    queryFn: () => channelsApi.connectivityHistory(channel.id),
    refetchInterval: 60_000,
  })

  // 选中模型状态提在卡片层：得分格与底栏共用同一个 selected
  const storageKey = `assay.overview.model.${channel.id}`
  const [stored, setStored] = useState<string | null>(() => localStorage.getItem(storageKey))
  const models = channel.models
  // 选中优先级：记忆 → 探活模型 → 第一个有分的 → 第一个
  const selected =
    models.find((m) => m.model === stored) ??
    models.find((m) => m.modelEntryId != null && m.modelEntryId === channel.probeModelId) ??
    models.find((m) => m.score != null) ??
    models[0]

  return (
    <Card className={cn(channel.disabled && 'opacity-60 saturate-50')}>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <button
            type="button"
            className="truncate hover:underline"
            title={channel.name}
            onClick={() => navigate(`/channels/${channel.id}`)}
          >
            {channel.name}
          </button>
          <ConnStatus test={channel.lastTest} />
          {channel.disabled && <Badge variant="secondary">{t('channels.disabled')}</Badge>}
        </CardTitle>
        <CardDescription className="truncate" title={channel.baseUrl}>
          {channel.baseUrl}
        </CardDescription>
        <CardAction className="flex max-w-44 flex-wrap justify-end gap-1">
          {channel.protocols.map((p) => (
            <Badge key={p} variant="outline">
              {t(`proto.${p}` as DictKey)}
            </Badge>
          ))}
          <Badge variant="outline">{channel.currency}</Badge>
        </CardAction>
      </CardHeader>
      {/* 中段两格分区：不画分隔线，区域感靠留白 + 小标题 */}
      <CardContent className="grid flex-1 grid-cols-2 gap-4">
        <div className="grid content-start gap-2">
          <p className="text-xs font-medium text-muted-foreground">{t('dash.qualityTitle')}</p>
          <ScoreZone
            models={models}
            selected={selected}
            onSelect={(v) => {
              localStorage.setItem(storageKey, v)
              setStored(v)
            }}
          />
        </div>
        <div className="grid content-start gap-2">
          <p className="text-xs font-medium text-muted-foreground">{t('dash.latencyTitle')}</p>
          <LatencySparkline points={history.data?.items ?? []} />
        </div>
      </CardContent>
      {selected && (
        <CardFooter className="justify-between gap-2 text-xs text-muted-foreground">
          <span>
            {t('dash.dataTime')}：
            {selected.finishedAt ? new Date(selected.finishedAt).toLocaleString() : '—'}
          </span>
          <Link to={`/quality/${selected.taskId}`} className="shrink-0 text-primary hover:underline">
            {t('dash.viewTask')} →
          </Link>
        </CardFooter>
      )}
    </Card>
  )
}

export default function DashboardPage() {
  const { t } = useI18n()
  const query = useQuery({
    queryKey: ['overview'],
    queryFn: overviewApi.channels,
    refetchInterval: 60_000,
  })

  return (
    <div className="grid gap-6">
      <div>
        <h1 className="text-2xl font-semibold">{t('nav.overview')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t('dash.desc')}</p>
      </div>
      {query.isPending ? (
        <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
      ) : (query.data?.items.length ?? 0) === 0 ? (
        <p className="text-sm text-muted-foreground">
          {t('dash.empty')}{' '}
          <Link to="/channels" className="text-primary hover:underline">
            {t('nav.channels')}
          </Link>
        </p>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {query.data?.items.map((c) => (
            <ChannelCard key={c.id} channel={c} />
          ))}
        </div>
      )}
    </div>
  )
}
