import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { describe, expect, it, vi } from 'vitest'
import appSource from '../App.vue?raw'
import LoginView from '@/views/auth/LoginView.vue'

vi.mock('vue-i18n', () => ({
  createI18n: () => ({ global: { t: (key: string) => key } }),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('@/stores', () => ({
  useAuthStore: () => ({ login: vi.fn(), loginWithPasskey: vi.fn(), login2FA: vi.fn() }),
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn(), showWarning: vi.fn() }),
}))
vi.mock('@/api/auth', () => ({
  buildOAuthLoginStartURL: vi.fn(),
  getPublicSettings: vi.fn().mockResolvedValue({ registration_enabled: true }),
  isTotp2FARequired: vi.fn(() => false),
  isWeChatWebOAuthEnabled: vi.fn(() => false),
  startOAuthLogin: vi.fn(),
}))

// 使用生产 App.vue 的路由出口模板，而不是复制一份等价写法。
function productionOutlet(): string {
  const outlet = appSource.match(/<RouterView v-slot[\s\S]*?<\/RouterView>/)?.[0]
  if (!outlet) throw new Error('Could not locate the production RouterView outlet')
  return outlet
}

async function mountOutlet() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', component: LoginView },
      { path: '/next', component: { template: '<div data-testid="next-route">Next page</div>' } },
    ],
  })
  await router.push('/login')
  await router.isReady()
  const wrapper = mount(defineComponent({ components: { RouterView }, template: productionOutlet() }), {
    attachTo: document.body,
    global: {
      plugins: [router],
      stubs: {
        AuthLayout: { template: '<div><slot /><slot name="footer" /></div>' },
        DingTalkOAuthSection: true,
        EmailOAuthButtons: true,
        Icon: true,
        LinuxDoOAuthSection: true,
        LoginAgreementPrompt: true,
        OidcOAuthSection: true,
        TotpLoginModal: true,
        TurnstileWidget: true,
        WechatOAuthSection: true,
      },
    },
  })
  return { router, wrapper }
}

describe('App route outlet', () => {
  it('renders a single stable wrapper so fragment-root pages can leave', async () => {
    const { wrapper } = await mountOutlet()
    try {
      await flushPromises()
      const wrappers = wrapper.findAll('.route-shift-view')
      expect(wrappers).toHaveLength(1)
      expect(wrappers[0].element.parentElement?.tagName.toLowerCase()).not.toBe('router-view')
    } finally {
      wrapper.unmount()
    }
  })

  it('leaves the real fragment-root LoginView and renders the next route', async () => {
    const { router, wrapper } = await mountOutlet()
    try {
      await flushPromises()
      expect(wrapper.text()).toContain('auth.signUp')
      await router.push('/next')
      await flushPromises()
      await new Promise((resolve) => setTimeout(resolve, 250))
      await flushPromises()
      expect(wrapper.find('[data-testid="next-route"]').exists()).toBe(true)
    } finally {
      wrapper.unmount()
    }
  })
})
