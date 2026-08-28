import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createBrowserRouter, RouterProvider } from 'react-router'
import '@fontsource-variable/inter'
import './index.css'
import { AppLayout } from '@/layouts/app-layout'
import ChannelsPage from '@/pages/channels'
import DashboardPage from '@/pages/dashboard'
import LoginPage from '@/pages/login'
import QualityPage from '@/pages/quality'
import SettingsPage from '@/pages/settings'
import StabilityPage from '@/pages/stability'

const router = createBrowserRouter([
  { path: '/login', Component: LoginPage },
  {
    path: '/',
    Component: AppLayout,
    children: [
      { index: true, Component: DashboardPage },
      { path: 'channels', Component: ChannelsPage },
      { path: 'quality', Component: QualityPage },
      { path: 'stability', Component: StabilityPage },
      { path: 'settings', Component: SettingsPage },
    ],
  },
])

const queryClient = new QueryClient()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
)
