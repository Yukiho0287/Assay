import { Outlet, useLocation } from 'react-router'
import { AppSidebar } from '@/components/app-sidebar'
import { Separator } from '@/components/ui/separator'
import { SidebarInset, SidebarProvider, SidebarTrigger } from '@/components/ui/sidebar'
import { TooltipProvider } from '@/components/ui/tooltip'

const pageTitles: Record<string, string> = {
  '/': '总览',
  '/channels': '渠道',
  '/quality': '质量检测',
  '/stability': '稳定性检测',
  '/settings': '设置',
}

export function AppLayout() {
  const { pathname } = useLocation()

  return (
    <TooltipProvider>
      <SidebarProvider>
        <AppSidebar />
        <SidebarInset>
          <header className="flex h-14 shrink-0 items-center gap-2 border-b px-4">
            <SidebarTrigger className="-ml-1" />
            <Separator orientation="vertical" className="mr-2 data-[orientation=vertical]:h-4" />
            <span className="text-sm font-medium">{pageTitles[pathname] ?? 'Assay'}</span>
          </header>
          <main className="flex flex-1 flex-col gap-4 p-6">
            <Outlet />
          </main>
        </SidebarInset>
      </SidebarProvider>
    </TooltipProvider>
  )
}
