import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'

// Proxy development API requests to keenProxy (default: keenproxy -ui-demo on :8086).
const target = process.env.KEENPROXY_URL ?? 'http://127.0.0.1:8086'

export default defineConfig(({ mode }) => ({
  plugins: [vue()],
  base: mode === 'demo' ? './' : '/',
  resolve: { alias: mode === 'demo' ? [{ find: /^\.{1,2}\/api$/, replacement: fileURLToPath(new URL('./src/demo/api.ts', import.meta.url)) }] : [] },
  server: { proxy: { '/api': { target, changeOrigin: true } } },
  build: { outDir: mode === 'demo' ? 'dist-demo' : 'dist', emptyOutDir: true, chunkSizeWarningLimit: 1500 },
}))
