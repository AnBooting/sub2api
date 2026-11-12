import { defineConfig } from '@playwright/test'

/**
 * 浏览器级 e2e（组件 harness 模式）。
 *
 * 目前覆盖不需要后端的能力：弹窗焦点管理、Esc 关闭层级、遮罩点击关闭。
 * 页面由 Vite dev server 提供（e2e/harness），因此不依赖数据库、Redis 或登录态。
 *
 * 浏览器：默认使用本机已安装的 Chrome（channel=chrome），避免下载 Chromium。
 * 没有 Chrome 时用 E2E_BROWSER_CHANNEL=msedge 切到 Edge。
 */
const port = Number(process.env.E2E_PORT ?? 5199)
const channel = process.env.E2E_BROWSER_CHANNEL ?? 'chrome'

export default defineConfig({
  testDir: './e2e/specs',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 15_000,
  expect: { timeout: 5_000 },
  reporter: [['list']],
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    channel,
    headless: true,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure'
  },
  webServer: {
    command: `node node_modules/vite/bin/vite.js --config e2e/vite.config.ts`,
    url: `http://127.0.0.1:${port}/e2e/harness/index.html`,
    reuseExistingServer: true,
    timeout: 120_000
  }
})
