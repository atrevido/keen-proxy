import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// Proxy development API requests to keenProxy (default: keenproxy -ui-demo on :8086).
const target = process.env.KEENPROXY_URL ?? 'http://127.0.0.1:8086'

export default defineConfig({
  plugins: [vue()],
  server: { proxy: { '/api': { target, changeOrigin: true } } },
  build: { outDir: 'dist', emptyOutDir: true, chunkSizeWarningLimit: 1500 },
})
