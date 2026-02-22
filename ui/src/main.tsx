import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { configureApi } from '@gocodealone/workflow-ui/api'
import useAuthStore from './store/authStore.ts'
import './index.css'
import App from './App.tsx'

// Configure the shared API client for workflow
configureApi({
  baseUrl: '/api/v1',
  onResponseError: (status: number, body: string) => {
    if (status === 401 || status === 403) {
      const msg = body.toLowerCase();
      if (msg.includes('user not found') || msg.includes('unauthorized') || msg.includes('invalid') || msg.includes('expired') || status === 401) {
        useAuthStore.getState().logout();
      }
    }
  },
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
