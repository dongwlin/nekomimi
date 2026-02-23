import { authLogin, authPassphraseRotate, authRefresh, authVerify } from './generated'
import type { HttpapiTokenResponse } from './generated'
import { initApiClient } from './client'
import { useAuthStore } from '@/features/auth/store'

interface APIResult<TData = unknown, TError = unknown> {
  data?: TData
  error?: TError
  request: Request
  response: Response
}

let refreshTask: Promise<boolean> | null = null

function redirectToLoginIfNeeded() {
  if (typeof window === 'undefined') {
    return
  }
  if (window.location.pathname === '/login') {
    return
  }
  window.location.replace('/login')
}

function normalizeTokenPair(data: HttpapiTokenResponse | undefined) {
  const accessToken = data?.access_token?.trim()
  const refreshToken = data?.refresh_token?.trim()
  if (!accessToken || !refreshToken) {
    return null
  }
  return { accessToken, refreshToken }
}

export async function loginWithPassphrase(passphrase: string) {
  initApiClient()
  const result = (await authLogin({
    body: {
      passphrase,
    },
  })) as APIResult<HttpapiTokenResponse, { error?: string }>

  if (result.response.status !== 200) {
    const message = result.error?.error ?? 'Login failed.'
    throw new Error(message)
  }

  const tokens = normalizeTokenPair(result.data)
  if (!tokens) {
    throw new Error('Login response is missing tokens.')
  }

  useAuthStore.getState().setTokens(tokens.accessToken, tokens.refreshToken)
}

export async function verifyAccessToken(): Promise<boolean> {
  initApiClient()
  const result = (await withAutoRefresh(async () => authVerify() as Promise<APIResult<{ ok?: boolean }>>)) as APIResult<{
    ok?: boolean
  }>
  return result.response.status === 200 && result.data?.ok === true
}

async function refreshAccessTokenPair(): Promise<boolean> {
  initApiClient()
  const refreshToken = useAuthStore.getState().refreshToken
  if (!refreshToken) {
    return false
  }

  const result = (await authRefresh({
    body: {
      refresh_token: refreshToken,
    },
  })) as APIResult<HttpapiTokenResponse>

  if (result.response.status !== 200) {
    return false
  }

  const tokens = normalizeTokenPair(result.data)
  if (!tokens) {
    return false
  }

  useAuthStore.getState().setTokens(tokens.accessToken, tokens.refreshToken)
  return true
}

export async function refreshAccessTokenOnce(): Promise<boolean> {
  if (refreshTask) {
    return refreshTask
  }

  refreshTask = (async () => {
    const refreshed = await refreshAccessTokenPair()
    if (!refreshed) {
      useAuthStore.getState().clearTokens()
      redirectToLoginIfNeeded()
    }
    return refreshed
  })()

  try {
    return await refreshTask
  } finally {
    refreshTask = null
  }
}

export async function restoreAuthSession() {
  initApiClient()
  const { accessToken, refreshToken, clearTokens } = useAuthStore.getState()
  if (!accessToken && !refreshToken) {
    return
  }

  if (accessToken) {
    const ok = await verifyAccessToken()
    if (ok) {
      return
    }
  }

  const refreshed = await refreshAccessTokenOnce()
  if (!refreshed) {
    clearTokens()
  }
}

export async function withAutoRefresh<TData = unknown>(
  call: () => Promise<APIResult<TData>>,
): Promise<APIResult<TData>> {
  const first = await call()
  if (first.response.status !== 401) {
    return first
  }

  const refreshed = await refreshAccessTokenOnce()
  if (!refreshed) {
    return first
  }

  const retried = await call()
  if (retried.response.status === 401) {
    useAuthStore.getState().clearTokens()
    redirectToLoginIfNeeded()
  }
  return retried
}

export async function rotateSystemPassphrase(currentPassphrase: string, newPassphrase: string) {
  initApiClient()
  const result = (await withAutoRefresh(async () =>
    authPassphraseRotate({
      body: {
        current_passphrase: currentPassphrase,
        new_passphrase: newPassphrase,
      },
    }) as Promise<APIResult<{ ok?: boolean }, { error?: string }>>,
  )) as APIResult<{ ok?: boolean }, { error?: string }>

  if (result.response.status !== 200 || result.data?.ok !== true) {
    const message = result.error?.error ?? 'rotate_passphrase_failed'
    throw new Error(message)
  }
}
