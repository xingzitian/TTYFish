import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 开发时把 /api 代理到本地后端，浏览器看到的始终是同源请求——不用配 CORS。
// 生产环境由后端（或 nginx）把 /api 和静态文件放在同一个域名下，同理。
export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://127.0.0.1:8081', changeOrigin: false },
      '/healthz': { target: 'http://127.0.0.1:8081', changeOrigin: false },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    // element-plus 整包比较大，拆成独立 chunk，二次访问能命中缓存
    rollupOptions: {
      output: {
        manualChunks: {
          'element-plus': ['element-plus'],
          vue: ['vue', 'vue-router'],
        },
      },
    },
  },
})
