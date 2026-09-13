import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    // 产物直接落到仓库根的 web/，由 Go 侧 //go:embed all:web 嵌进单二进制。
    // emptyOutDir 每次构建都会清空 web/（连 .gitkeep 一起删），随后 vite 把
    // frontend/public/ 原样复制回去 —— public/.gitkeep 就是让 web/ 在仓库里
    // 永远非空（go:embed 编译要求）的载体，看着是空文件，千万别删。
    outDir: '../web',
    emptyOutDir: true,
    rollupOptions: {
      onwarn(warning, warn) {
        // tokens.ts 是从 animego（Next.js 项目）逐字节原样搬来的，顶部带有
        // "use client" 模块级指令。Vite 打包时不认识该指令，会发出
        // MODULE_LEVEL_DIRECTIVE 告警，但指令在产物里会被安全丢弃、完全无害。
        // 为保持 tokens.ts 一个字符都不改，这里静默这一类告警；其余告警照常输出。
        if (warning.code === 'MODULE_LEVEL_DIRECTIVE') return
        warn(warning)
      },
    },
  },
  server: {
    proxy: {
      // 开发模式下把 /api 转给本地 Go 后端。
      // 后端有 Host 头白名单（只认 127.0.0.1:<port> / localhost:<port>，挡 DNS
      // rebinding），所以必须 changeOrigin 把 Host 改写成目标地址才能过校验。
      // 8590 是后端默认端口；被占时后端会自动向上找并回写配置，此时用
      // NAGARE_DEV_PORT=<实际端口> bun run dev 覆盖。
      '/art': {
        target: `http://127.0.0.1:${process.env.NAGARE_DEV_PORT ?? '8590'}`,
        changeOrigin: true,
      },
      '/api': {
        target: `http://127.0.0.1:${process.env.NAGARE_DEV_PORT ?? '8590'}`,
        changeOrigin: true,
      },
    },
  },
})
