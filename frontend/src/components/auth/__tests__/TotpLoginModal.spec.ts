import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import TotpLoginModal from '@/components/auth/TotpLoginModal.vue'

const { showErrorMock } = vi.hoisted(() => ({
  showErrorMock: vi.fn(),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError: (...args: any[]) => showErrorMock(...args),
  }),
}))

describe('TotpLoginModal', () => {
  beforeEach(() => {
    showErrorMock.mockReset()
  })

  // 内联错误块 = danger 三族（底色/描边/文字）；不含 btn-danger，因为按钮合法可以用危险色。
  const DANGER_CALLOUT = /bg-danger-(?:50|100)|border-danger-|text-danger-(?:600|700)/

  it('sends verification errors to toast and does not render an inline danger callout', async () => {
    const wrapper = mount(TotpLoginModal, {
      props: {
        tempToken: 'temp-token',
        userEmailMasked: 'u***@example.com',
      },
    })

    ;(wrapper.vm as unknown as { setError: (message: string) => void }).setError('Invalid code')
    await wrapper.vm.$nextTick()

    expect(showErrorMock).toHaveBeenCalledWith('Invalid code')
    expect(wrapper.text()).not.toContain('Invalid code')
    // 范围限定在弹层面板本身：错误必须走 toast，面板内不得出现 danger 内联块。
    // 旧断言盯的是 '.bg-red-50' —— 该类已从调色板删除、永不渲染，属恒真断言（见 task-2 报告）。
    expect(wrapper.get('.relative.w-full.max-w-md').html()).not.toMatch(DANGER_CALLOUT)
  })
})
