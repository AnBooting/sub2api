import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

/**
 * e2e harness 专用 Vite 配置。
 *
 * 不复用主 vite.config.ts 的原因：那里注册了 dev 期的公开配置注入插件，
 * 会向不存在后端发起 2 秒超时的请求，拖慢每个用例并刷警告。
 * harness 只挂载组件，不需要代理与品牌注入。
 */
const projectRoot = resolve(import.meta.dirname, '..')

export default defineConfig({
  root: projectRoot,
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(projectRoot, 'src')
    }
  },
  server: {
    host: '127.0.0.1',
    port: Number(process.env.E2E_PORT ?? 5199),
    strictPort: true
  }
})
