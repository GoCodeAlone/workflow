/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  optimizeDeps: {
    include: ['@gocodealone/workflow-ui/api', '@gocodealone/workflow-ui/auth', '@gocodealone/workflow-ui/sse', '@gocodealone/workflow-ui/theme'],
  },
  resolve: {
    dedupe: ['react', 'react-dom', 'zustand'],
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8081',
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    exclude: ['e2e/**', 'node_modules/**'],
  },
})
