import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        // go-techbase 后端(hertz)端口;Python techbase 为 5000
        target: process.env.VITE_API_TARGET || 'http://localhost:9680',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
  },
})
