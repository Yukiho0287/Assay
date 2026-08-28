import { useQuery } from '@tanstack/react-query'
import { systemApi } from '@/lib/api'

export default function DashboardPage() {
  const { data } = useQuery({
    queryKey: ['system', 'version'],
    queryFn: systemApi.version,
    staleTime: Infinity,
  })

  return (
    <div>
      <h1 className="text-2xl font-semibold">总览</h1>
      <p className="mt-1 text-sm text-muted-foreground">
        平台概况与最近检测动态（待实现）。
      </p>
      <p className="mt-4 text-xs text-muted-foreground">服务版本：{data?.version ?? '—'}</p>
    </div>
  )
}
