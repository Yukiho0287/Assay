import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router'
import { ConnStatus } from '@/components/channel-form-dialog'
import { ScoreText, TaskStatusBadge } from '@/components/quality-badges'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardDescription,
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
} from '@/lib/api'
import { type DictKey, useI18n } from '@/lib/i18n'

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
      <div className="flex flex-wrap gap-x-4 gap-y-1">
        {[...byProto.entries()].map(([proto, list]) => {
          const latest = list[list.length - 1]
          return (
            <span key={proto} className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
              <span className={`size-2 rounded-full ${protoDot[proto] ?? 'bg-muted-foreground'}`} />
              {t(`proto.${proto}` as DictKey)}
              <span className="tabular-nums">
                {latest.ttftMs != null ? `${latest.ttftMs} ms` : '—'}
              </span>
            </span>
          )
        })}
      </div>
    </div>
  )
}

// ScoreBlock 质量得分区：卡片只展示一个模型（可切换），选择记入 localStorage
function ScoreBlock({ channel }: { channel: OverviewChannel }) {
  const { t } = useI18n()
  const storageKey = `assay.overview.model.${channel.id}`
  const [stored, setStored] = useState<string | null>(() => localStorage.getItem(storageKey))

  const models = channel.models
  if (models.length === 0) {
    return <p className="text-xs text-muted-foreground">{t('dash.noModels')}</p>
  }

  // 选中优先级：记忆 → 探活模型 → 第一个有分的 → 第一个
  const selected =
    models.find((m) => m.model === stored) ??
    models.find((m) => m.modelEntryId != null && m.modelEntryId === channel.probeModelId) ??
    models.find((m) => m.score != null) ??
    models[0]

  return (
    <div className="grid gap-2">
      <div className="flex items-center justify-between gap-2">
        <ScoreText score={selected.score} grade={selected.grade} className="text-3xl" />
        {selected.taskStatus !== 'succeeded' && <TaskStatusBadge status={selected.taskStatus} />}
      </div>
      {models.length > 1 && (
        <Select
          value={selected.model}
          onValueChange={(v) => {
            localStorage.setItem(storageKey, v)
            setStored(v)
          }}
        >
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
      )}
      {models.length === 1 && (
        <p className="truncate text-xs text-muted-foreground" title={selected.model}>
          {selected.model}
        </p>
      )}
      <p className="text-xs text-muted-foreground">
        {t('dash.dataTime')}：
        {selected.finishedAt ? new Date(selected.finishedAt).toLocaleString() : '—'}
        <Link to={`/quality/${selected.taskId}`} className="ml-2 text-primary hover:underline">
          {t('dash.viewTask')}
        </Link>
      </p>
    </div>
  )
}

// ChannelCard 渠道卡片：基本信息 + 延迟曲线 + 质量得分；每卡独立拉自己的连通历史
function ChannelCard({ channel }: { channel: OverviewChannel }) {
  const { t } = useI18n()
  const navigate = useNavigate()
  const history = useQuery({
    queryKey: ['overview', 'history', channel.id],
    queryFn: () => channelsApi.connectivityHistory(channel.id),
    refetchInterval: 60_000,
  })

  return (
    <Card className={channel.disabled ? 'opacity-60 saturate-50' : ''}>
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
      </CardHeader>
      <CardContent className="grid gap-4">
        <div className="flex flex-wrap gap-1">
          {channel.protocols.map((p) => (
            <Badge key={p} variant="outline">
              {t(`proto.${p}` as DictKey)}
            </Badge>
          ))}
          <Badge variant="outline">{channel.currency}</Badge>
        </div>
        <div className="grid gap-1.5">
          <p className="text-xs font-medium text-muted-foreground">{t('dash.latencyTitle')}</p>
          <LatencySparkline points={history.data?.items ?? []} />
        </div>
        <div className="grid gap-1.5">
          <p className="text-xs font-medium text-muted-foreground">{t('dash.qualityTitle')}</p>
          <ScoreBlock channel={channel} />
        </div>
      </CardContent>
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
