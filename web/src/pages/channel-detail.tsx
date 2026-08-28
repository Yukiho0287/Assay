import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowLeft,
  CircleCheck,
  CircleX,
  Loader2,
  Pencil,
  Plus,
  Trash2,
} from 'lucide-react'
import { useNavigate, useParams } from 'react-router'
import { ChannelFormDialog, ConnStatus, errText } from '@/components/channel-form-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
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
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
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
import {
  channelsApi,
  type ChannelDetail,
  type ModelEntry,
  type ModelEntryUpsert,
} from '@/lib/api'
import { type DictKey, useI18n } from '@/lib/i18n'

// 价格展示：币种符号 + 数值；未填显示占位横线
function priceText(symbol: string, v?: number): string {
  return v == null ? '—' : `${symbol}${v}`
}

// 表单空串 → undefined（不定价），否则转数字
function priceValue(data: FormData, name: string): number | undefined {
  const raw = String(data.get(name) ?? '').trim()
  return raw === '' ? undefined : Number(raw)
}

// ModelFormDialog 添加/编辑模型条目共用表单（编辑走 PUT 全量替换）
function ModelFormDialog({
  open,
  title,
  initial,
  pending,
  error,
  onClose,
  onSubmit,
}: {
  open: boolean
  title: string
  initial?: ModelEntry
  pending: boolean
  error: unknown
  onClose: () => void
  onSubmit: (v: ModelEntryUpsert) => void
}) {
  const { t } = useI18n()

  function handleSubmit(e: React.SubmitEvent<HTMLFormElement>) {
    e.preventDefault()
    const data = new FormData(e.currentTarget)
    onSubmit({
      name: String(data.get('name') ?? ''),
      inputPrice: priceValue(data, 'inputPrice'),
      outputPrice: priceValue(data, 'outputPrice'),
      cachedInputPrice: priceValue(data, 'cachedInputPrice'),
    })
  }

  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) onClose() }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <form key={initial?.id ?? 'new'} className="grid gap-4" onSubmit={handleSubmit}>
          <div className="grid gap-2">
            <Label htmlFor="m-name">{t('models.name')}</Label>
            <Input
              id="m-name"
              name="name"
              maxLength={128}
              placeholder="gpt-5.2"
              defaultValue={initial?.name}
              required
            />
          </div>
          <div className="grid grid-cols-3 gap-3">
            <div className="grid gap-2">
              <Label htmlFor="m-input">{t('models.inputPrice')}</Label>
              <Input
                id="m-input"
                name="inputPrice"
                type="number"
                step="any"
                min={0}
                defaultValue={initial?.inputPrice}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="m-output">{t('models.outputPrice')}</Label>
              <Input
                id="m-output"
                name="outputPrice"
                type="number"
                step="any"
                min={0}
                defaultValue={initial?.outputPrice}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="m-cached">{t('models.cachedInputPrice')}</Label>
              <Input
                id="m-cached"
                name="cachedInputPrice"
                type="number"
                step="any"
                min={0}
                defaultValue={initial?.cachedInputPrice}
              />
            </div>
          </div>
          <p className="text-xs text-muted-foreground">{t('models.priceHint')}</p>
          {error != null && <p className="text-sm text-destructive">{errText(error)}</p>}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={pending}>
              {pending && <Loader2 className="animate-spin" />}
              {initial ? t('common.save') : t('common.create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// TestCard 连通测试卡：选模型发起测试 + 展示渠道最近一次结果快照
function TestCard({ channel }: { channel: ChannelDetail }) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [modelId, setModelId] = useState<string>('')

  const test = useMutation({
    mutationFn: (mid: string) => channelsApi.test(channel.id, mid),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['channels'] }),
  })

  const selected = modelId || channel.models[0]?.id || ''
  const last = channel.lastTest

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('test.title')}</CardTitle>
        <CardDescription>{t('test.desc')}</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        <div className="flex flex-wrap items-end gap-3">
          <div className="grid gap-2">
            <Label>{t('test.model')}</Label>
            <Select value={selected} onValueChange={setModelId} disabled={channel.models.length === 0}>
              <SelectTrigger className="w-56">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {channel.models.map((m) => (
                  <SelectItem key={m.id} value={m.id}>
                    {m.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <Button
            onClick={() => test.mutate(selected)}
            disabled={test.isPending || !selected}
          >
            {test.isPending && <Loader2 className="animate-spin" />}
            {test.isPending ? t('test.running') : t('test.run')}
          </Button>
          {channel.models.length === 0 && (
            <p className="text-sm text-muted-foreground">{t('test.noModels')}</p>
          )}
        </div>
        {test.isError && <p className="text-sm text-destructive">{errText(test.error)}</p>}
        {last && (
          <div className="grid gap-2">
            <p className="text-sm text-muted-foreground">
              {t('test.lastResult')}：{new Date(last.testedAt).toLocaleString()} · {last.model}
            </p>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('test.protocol')}</TableHead>
                  <TableHead>{t('channels.connectivity')}</TableHead>
                  <TableHead>{t('test.ttft')}</TableHead>
                  <TableHead>HTTP</TableHead>
                  <TableHead>{t('test.fail')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {last.results.map((r) => (
                  <TableRow key={r.protocol}>
                    <TableCell>{t(`proto.${r.protocol}` as DictKey)}</TableCell>
                    <TableCell>
                      <span className="inline-flex items-center gap-1.5">
                        {r.ok ? (
                          <CircleCheck className="size-4 text-green-500" />
                        ) : (
                          <CircleX className="size-4 text-red-500" />
                        )}
                        {r.ok ? t('test.ok') : t('test.fail')}
                      </span>
                    </TableCell>
                    <TableCell>{r.ttftMs != null ? `${r.ttftMs} ms` : '—'}</TableCell>
                    <TableCell>{r.status ?? '—'}</TableCell>
                    <TableCell className="max-w-80 truncate text-muted-foreground" title={r.error}>
                      {r.error ?? '—'}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

export default function ChannelDetailPage() {
  const { id = '' } = useParams()
  const { t } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const [editOpen, setEditOpen] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [modelAddOpen, setModelAddOpen] = useState(false)
  const [modelEdit, setModelEdit] = useState<ModelEntry | null>(null)
  const [modelDelete, setModelDelete] = useState<ModelEntry | null>(null)

  const query = useQuery({
    queryKey: ['channels', id],
    queryFn: () => channelsApi.get(id),
    retry: false,
  })

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['channels'] })

  const update = useMutation({
    mutationFn: (v: Parameters<typeof channelsApi.update>[1]) => channelsApi.update(id, v),
    onSuccess: () => {
      invalidate()
      setEditOpen(false)
    },
  })
  const removeChannel = useMutation({
    mutationFn: () => channelsApi.remove(id),
    onSuccess: () => {
      // 先跳转再失效：详情查询随页面卸载后不会对已删渠道发起 404 重取
      navigate('/channels')
      invalidate()
    },
  })
  const addModel = useMutation({
    mutationFn: (v: ModelEntryUpsert) => channelsApi.addModel(id, v),
    onSuccess: () => {
      invalidate()
      setModelAddOpen(false)
    },
  })
  const updateModel = useMutation({
    mutationFn: ({ modelId, body }: { modelId: string; body: ModelEntryUpsert }) =>
      channelsApi.updateModel(id, modelId, body),
    onSuccess: () => {
      invalidate()
      setModelEdit(null)
    },
  })
  const removeModel = useMutation({
    mutationFn: (modelId: string) => channelsApi.removeModel(id, modelId),
    onSuccess: () => {
      invalidate()
      setModelDelete(null)
    },
  })

  if (query.isPending) {
    return <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
  }
  if (query.isError) {
    return (
      <div className="grid gap-4">
        <p className="text-sm text-destructive">{t('channels.notFound')}</p>
        <Button variant="outline" className="w-fit" onClick={() => navigate('/channels')}>
          <ArrowLeft />
          {t('common.back')}
        </Button>
      </div>
    )
  }

  const ch = query.data
  const symbol = ch.currency === 'CNY' ? '¥' : '$'

  return (
    <div className="grid gap-6">
      <div className="flex flex-wrap items-center gap-3">
        <Button
          variant="ghost"
          size="icon"
          className="size-8"
          title={t('common.back')}
          onClick={() => navigate('/channels')}
        >
          <ArrowLeft />
        </Button>
        <h1 className="text-2xl font-semibold">{ch.name}</h1>
        {ch.disabled && <Badge variant="secondary">{t('channels.disabled')}</Badge>}
        <div className="ml-auto flex items-center gap-2">
          <Button variant="outline" onClick={() => setEditOpen(true)}>
            <Pencil />
            {t('common.edit')}
          </Button>
          <Button
            variant="outline"
            title={t('channels.disabledHint')}
            disabled={update.isPending}
            onClick={() => update.mutate({ disabled: !ch.disabled })}
          >
            {ch.disabled ? t('channels.enable') : t('channels.disable')}
          </Button>
          <Button variant="destructive" onClick={() => setDeleteOpen(true)}>
            <Trash2 />
            {t('common.delete')}
          </Button>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{t('channels.basicInfo')}</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid gap-x-8 gap-y-3 text-sm sm:grid-cols-2">
            <div className="grid gap-1">
              <dt className="text-muted-foreground">{t('channels.baseUrl')}</dt>
              <dd className="break-all">{ch.baseUrl}</dd>
            </div>
            <div className="grid gap-1">
              <dt className="text-muted-foreground">{t('channels.keyPrefix')}</dt>
              <dd className="font-mono">{ch.keyPrefix}</dd>
            </div>
            <div className="grid gap-1">
              <dt className="text-muted-foreground">{t('channels.protocols')}</dt>
              <dd className="flex flex-wrap gap-1">
                {ch.protocols.map((p) => (
                  <Badge key={p} variant="outline">
                    {t(`proto.${p}` as DictKey)}
                  </Badge>
                ))}
              </dd>
            </div>
            <div className="grid gap-1">
              <dt className="text-muted-foreground">{t('channels.currency')}</dt>
              <dd>{ch.currency}</dd>
            </div>
            <div className="grid gap-1">
              <dt className="text-muted-foreground">{t('channels.connectivity')}</dt>
              <dd>
                <ConnStatus test={ch.lastTest} />
              </dd>
            </div>
            <div className="grid gap-1">
              <dt className="text-muted-foreground">{t('channels.createdAt')}</dt>
              <dd>{new Date(ch.createdAt).toLocaleString()}</dd>
            </div>
            {ch.note && (
              <div className="grid gap-1 sm:col-span-2">
                <dt className="text-muted-foreground">{t('channels.note')}</dt>
                <dd>{ch.note}</dd>
              </div>
            )}
          </dl>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('models.title')}</CardTitle>
          <CardDescription>{t('models.desc')}</CardDescription>
          <CardAction>
            <Button onClick={() => setModelAddOpen(true)}>
              <Plus />
              {t('models.add')}
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent>
          {ch.models.length === 0 ? (
            <p className="py-6 text-center text-sm text-muted-foreground">{t('models.empty')}</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('models.name')}</TableHead>
                  <TableHead>{t('models.inputPrice')}</TableHead>
                  <TableHead>{t('models.outputPrice')}</TableHead>
                  <TableHead>{t('models.cachedInputPrice')}</TableHead>
                  <TableHead className="text-right">{t('users.actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {ch.models.map((m) => (
                  <TableRow key={m.id}>
                    <TableCell className="font-medium">
                      {m.name}
                      {m.inputPrice == null && m.cachedInputPrice == null && (
                        <Badge variant="secondary" className="ml-2">
                          {t('models.unpriced')}
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell>{priceText(symbol, m.inputPrice)}</TableCell>
                    <TableCell>{priceText(symbol, m.outputPrice)}</TableCell>
                    <TableCell>{priceText(symbol, m.cachedInputPrice)}</TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="ghost"
                        size="icon"
                        className="size-8"
                        title={t('common.edit')}
                        onClick={() => setModelEdit(m)}
                      >
                        <Pencil />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="size-8 text-destructive"
                        title={t('common.delete')}
                        onClick={() => setModelDelete(m)}
                      >
                        <Trash2 />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <TestCard channel={ch} />

      <ChannelFormDialog
        open={editOpen}
        title={t('channels.edit')}
        initial={ch}
        pending={update.isPending}
        error={update.isError ? update.error : null}
        onClose={() => {
          setEditOpen(false)
          update.reset()
        }}
        onSubmit={(v) =>
          // 编辑时 apiKey 留空 = 不修改（PATCH 缺省字段保持原值）
          update.mutate({ ...v, apiKey: v.apiKey || undefined })
        }
      />

      <Dialog open={deleteOpen} onOpenChange={(o) => { if (!o) setDeleteOpen(false) }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t('channels.deleteTitle')}</DialogTitle>
            <DialogDescription>{t('channels.deleteDesc')}</DialogDescription>
          </DialogHeader>
          {removeChannel.isError && (
            <p className="text-sm text-destructive">{errText(removeChannel.error)}</p>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setDeleteOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button
              variant="destructive"
              disabled={removeChannel.isPending}
              onClick={() => removeChannel.mutate()}
            >
              {removeChannel.isPending && <Loader2 className="animate-spin" />}
              {t('common.delete')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ModelFormDialog
        open={modelAddOpen}
        title={t('models.add')}
        pending={addModel.isPending}
        error={addModel.isError ? addModel.error : null}
        onClose={() => {
          setModelAddOpen(false)
          addModel.reset()
        }}
        onSubmit={(v) => addModel.mutate(v)}
      />

      <ModelFormDialog
        open={modelEdit !== null}
        title={t('models.editTitle')}
        initial={modelEdit ?? undefined}
        pending={updateModel.isPending}
        error={updateModel.isError ? updateModel.error : null}
        onClose={() => {
          setModelEdit(null)
          updateModel.reset()
        }}
        onSubmit={(v) => modelEdit && updateModel.mutate({ modelId: modelEdit.id, body: v })}
      />

      <Dialog open={modelDelete !== null} onOpenChange={(o) => { if (!o) setModelDelete(null) }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t('models.deleteTitle')}</DialogTitle>
            <DialogDescription>
              {modelDelete?.name} — {t('models.deleteDesc')}
            </DialogDescription>
          </DialogHeader>
          {removeModel.isError && (
            <p className="text-sm text-destructive">{errText(removeModel.error)}</p>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setModelDelete(null)}>
              {t('common.cancel')}
            </Button>
            <Button
              variant="destructive"
              disabled={removeModel.isPending}
              onClick={() => modelDelete && removeModel.mutate(modelDelete.id)}
            >
              {removeModel.isPending && <Loader2 className="animate-spin" />}
              {t('common.delete')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
