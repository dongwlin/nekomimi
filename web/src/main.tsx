import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App.tsx'
import { AppProviders } from './app/providers.tsx'
import { restoreAuthSession } from './lib/api/auth.ts'
import { initApiClient } from './lib/api/client.ts'
import 'antd/dist/reset.css'
import './styles/index.css'

async function bootstrap() {
  initApiClient()
  await restoreAuthSession()

  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <AppProviders>
        <App />
      </AppProviders>
    </StrictMode>,
  )
}

void bootstrap()
