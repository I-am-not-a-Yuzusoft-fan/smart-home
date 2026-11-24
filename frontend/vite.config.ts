import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// 开发期把 /api 代理到后端，避免前端去猜跨域与 Cookie 属性；
// 浏览器只认 localhost:5173，Cookie 自然是同源的。
const apiPort = process.env.API_PORT ?? '8081'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    // 5173 被占时宁可报错，也别静默换到 5174——start.sh 打印的是 5173
    strictPort: true,
    proxy: {
      '/api': { target: `http://127.0.0.1:${apiPort}`, changeOrigin: false },
      // /ws 要单独代理并开启 ws：只代理 /api 的话浏览器握手会被 Vite 自己吞掉。
      '/ws': { target: `http://127.0.0.1:${apiPort}`, ws: true, changeOrigin: false },
    },
  },
})
