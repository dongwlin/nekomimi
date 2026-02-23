import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react-swc'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) {
            return undefined
          }

          const isAntdChunk =
            id.includes('/antd/') || id.includes('\\antd\\')
          if (isAntdChunk) {
            return 'vendor-antd'
          }

          const isAntdRelatedChunk =
            id.includes('/@ant-design/') ||
            id.includes('\\@ant-design\\') ||
            id.includes('/rc-') ||
            id.includes('\\rc-') ||
            id.includes('/@rc-component/') ||
            id.includes('\\@rc-component\\')
          if (isAntdRelatedChunk) {
            return 'vendor-antd'
          }

          if (id.includes('/react-router/') || id.includes('\\react-router\\')) {
            return 'vendor-router'
          }

          if (id.includes('/zustand/') || id.includes('\\zustand\\')) {
            return 'vendor-zustand'
          }

          const isReactChunk =
            id.includes('/react/') ||
            id.includes('\\react\\') ||
            id.includes('/react-dom/') ||
            id.includes('\\react-dom\\') ||
            id.includes('/scheduler/') ||
            id.includes('\\scheduler\\')
          if (isReactChunk) {
            return 'vendor-react'
          }

          return undefined
        },
      },
    },
  },
})
