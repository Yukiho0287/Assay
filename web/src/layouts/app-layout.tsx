import { useQuery, useQueryClient } from '@tanstack/react-query'
import { LogOut } from 'lucide-react'
import { Navigate, Outlet, useLocation, useNavigate } from 'react-router'
import { AppSidebar } from '@/components/app-sidebar'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { SidebarInset, SidebarProvider, SidebarTrigger } from '@/components/ui/sidebar'
import { TooltipProvider } from '@/components/ui/tooltip'
import { authApi } from '@/lib/api'

const pageTitles: Record<string, string> = {
  '/': '总览',
  '/channels': '渠道',
  '/quality': '质量检测',
  '/stability': '稳定性检测',
  '/settings': '设置',
}

export function AppLayout() {
  const { pathname } = useLocation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  // 路由守卫：未登录（/auth/me 401）一律重定向到登录页
  const { data: user, isPending, isError } = useQuery({
    queryKey: ['auth', 'me'],
    queryFn: authApi.me,
    retry: false,
    staleTime: 5 * 60_000,
  })

  if (isPending) return null
  if (isError || !user) return <Navigate to="/login" replace />

  async function handleLogout() {
    await authApi.logout()
    queryClient.clear()
    navigate('/login', { replace: true })
  }

  return (
    <TooltipProvider>
      <SidebarProvider>
        <AppSidebar />
        <SidebarInset>
          <header className="flex h-14 shrink-0 items-center gap-2 border-b px-4">
            <SidebarTrigger className="-ml-1" />
            <Separator orientation="vertical" className="mr-2 data-[orientation=vertical]:h-4" />
            <span className="text-sm font-medium">{pageTitles[pathname] ?? 'Assay'}</span>
            <div className="ml-auto flex items-center gap-2">
              <span className="text-sm text-muted-foreground">{user.username}</span>
              <Button variant="ghost" size="sm" onClick={handleLogout}>
                <LogOut />
                退出
              </Button>
            </div>
          </header>
          <main className="flex flex-1 flex-col gap-4 p-6">
            <Outlet />
          </main>
        </SidebarInset>
      </SidebarProvider>
    </TooltipProvider>
  )
}
