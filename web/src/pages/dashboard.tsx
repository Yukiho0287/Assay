import { useQuery } from '@tanstack/react-query'
import { systemApi } from '@/lib/api'
import { useI18n } from '@/lib/i18n'

export default function DashboardPage() {
  const { t } = useI18n()
  const { data } = useQuery({
    queryKey: ['system', 'version'],
    queryFn: systemApi.version,
    staleTime: Infinity,
  })

  return (
    <div>
      <h1 className="text-2xl font-semibold">{t('nav.overview')}</h1>
      <p className="mt-1 text-sm text-muted-foreground">{t('dash.desc')}</p>
      <p className="mt-4 text-xs text-muted-foreground">
        {t('dash.version')}：{data?.version ?? '—'}
      </p>
    </div>
  )
}
