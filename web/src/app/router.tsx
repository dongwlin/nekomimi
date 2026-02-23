import { lazy, Suspense, type ReactNode } from 'react'
import { Spin } from 'antd'
import { Navigate, createBrowserRouter } from 'react-router'
import { RequireAuth } from './guards/RequireAuth'

const AuthLayout = lazy(async () => {
  const module = await import('./layouts/AuthLayout')
  return { default: module.AuthLayout }
})

const RootLayout = lazy(async () => {
  const module = await import('./layouts/RootLayout')
  return { default: module.RootLayout }
})

const LoginPage = lazy(async () => {
  const module = await import('@/features/auth/pages/Login')
  return { default: module.LoginPage }
})

const DashboardPage = lazy(async () => {
  const module = await import('@/features/dashboard/pages/Dashboard')
  return { default: module.DashboardPage }
})

const SecuritySettingsPage = lazy(async () => {
  const module = await import('@/features/auth/pages/SecuritySettings')
  return { default: module.SecuritySettingsPage }
})

const routeFallback = (
  <div
    style={{
      minHeight: '30vh',
      display: 'grid',
      placeItems: 'center',
    }}
  >
    <Spin size="large" />
  </div>
)

function withSuspense(element: ReactNode) {
  return <Suspense fallback={routeFallback}>{element}</Suspense>
}

export const appRouter = createBrowserRouter([
  {
    path: '/login',
    element: withSuspense(<AuthLayout />),
    children: [{ index: true, element: withSuspense(<LoginPage />) }],
  },
  {
    path: '/',
    element: withSuspense(
      <RequireAuth>
        <RootLayout />
      </RequireAuth>,
    ),
    children: [
      { index: true, element: <Navigate to="/dashboard" replace /> },
      { path: 'dashboard', element: withSuspense(<DashboardPage />) },
      { path: 'settings/security', element: withSuspense(<SecuritySettingsPage />) },
    ],
  },
  {
    path: '*',
    element: <Navigate to="/" replace />,
  },
])
