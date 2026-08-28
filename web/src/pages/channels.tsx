import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { useNavigate } from 'react-router'
import { ChannelFormDialog, ConnStatus, errText } from '@/components/channel-form-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { channelsApi } from '@/lib/api'
import { type DictKey, useI18n } from '@/lib/i18n'

export default function ChannelsPage() {
  const { t } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [createOpen, setCreateOpen] = useState(false)

  const channels = useQuery({ queryKey: ['channels'], queryFn: channelsApi.list })

  const create = useMutation({
    mutationFn: channelsApi.create,
    onSuccess: (c) => {
      queryClient.invalidateQueries({ queryKey: ['channels'] })
      setCreateOpen(false)
      // 建完直奔详情页添加模型条目
      navigate(`/channels/${c.id}`)
    },
  })

  return (
    <div className="grid gap-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">{t('nav.channels')}</h1>
          <p className="mt-1 text-sm text-muted-foreground">{t('channels.desc')}</p>
        </div>
        <Button onClick={() => setCreateOpen(true)}>
          <Plus />
          {t('channels.create')}
        </Button>
      </div>

      <Card>
        <CardContent>
          {channels.isPending ? (
            <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
          ) : channels.isError ? (
            <p className="text-sm text-destructive">{errText(channels.error)}</p>
          ) : channels.data.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">{t('channels.empty')}</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('channels.name')}</TableHead>
                  <TableHead>{t('channels.baseUrl')}</TableHead>
                  <TableHead>{t('channels.protocols')}</TableHead>
                  <TableHead className="text-right">{t('channels.modelCount')}</TableHead>
                  <TableHead>{t('channels.currency')}</TableHead>
                  <TableHead>{t('channels.connectivity')}</TableHead>
                  <TableHead>{t('channels.createdAt')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {channels.data.map((c) => (
                  <TableRow
                    key={c.id}
                    className="cursor-pointer"
                    onClick={() => navigate(`/channels/${c.id}`)}
                  >
                    <TableCell className="font-medium">
                      {c.name}
                      {c.disabled && (
                        <Badge variant="secondary" className="ml-2">
                          {t('channels.disabled')}
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="max-w-64 truncate text-muted-foreground">
                      {c.baseUrl}
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {c.protocols.map((p) => (
                          <Badge key={p} variant="outline">
                            {t(`proto.${p}` as DictKey)}
                          </Badge>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">{c.modelCount}</TableCell>
                    <TableCell>{c.currency}</TableCell>
                    <TableCell>
                      <ConnStatus test={c.lastTest} />
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {new Date(c.createdAt).toLocaleString()}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <ChannelFormDialog
        open={createOpen}
        title={t('channels.create')}
        pending={create.isPending}
        error={create.isError ? create.error : null}
        onClose={() => {
          setCreateOpen(false)
          create.reset()
        }}
        onSubmit={(v) => create.mutate(v)}
      />
    </div>
  )
}
