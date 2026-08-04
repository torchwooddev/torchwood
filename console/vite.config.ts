import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  base: '/console/',
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    // 代理 API 到本地 Go server（configs/config.yaml.template 的 server.http
    // 端口），保证 dev 下 /v1 与页面同源，HttpOnly 会话 cookie 正常工作。
    proxy: {
      '/v1': 'http://localhost:9099',
    },
  },
})
