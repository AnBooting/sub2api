import { describe, expect, it, vi } from "vitest";

// 生产构建通过 vite.config.ts 的 define 打开 vue-i18n 的 JIT 编译；
// Vitest 不继承该 define，这里补上，否则 t() 无法编译文案。
vi.hoisted(() => {
  vi.stubGlobal("__INTLIFY_JIT_COMPILATION__", true);
});

import { mount } from "@vue/test-utils";
import { createI18n } from "vue-i18n";

import DeepSeekPricingStateBadge from "@/components/admin/group/DeepSeekPricingStateBadge.vue";
import enAdminOverview from "@/i18n/locales/en/admin/overview";
import zhAdminOverview from "@/i18n/locales/zh/admin/overview";

const mountBadge = (state: unknown) => {
  const i18n = createI18n({
    legacy: false,
    locale: "zh",
    fallbackLocale: "en",
    messages: { zh: { admin: zhAdminOverview }, en: { admin: enAdminOverview } }
  });
  return mount(DeepSeekPricingStateBadge, {
    props: { state: state as never },
    global: { plugins: [i18n] }
  });
};

describe("DeepSeekPricingStateBadge", () => {
  it("renders the peak badge with the backend label and effective multiplier", () => {
    const wrapper = mountBadge({
      state: "peak",
      label: "峰价时段 09:00-12:00 / 14:00-18:00",
      timezone: "Asia/Shanghai",
      multiplier: 1.5
    });

    const badge = wrapper.get('[data-testid="deepseek-pricing-state-badge"]');
    expect(badge.text()).toContain("峰价时段 09:00-12:00 / 14:00-18:00");
    expect(badge.text()).toContain("1.5x");
    expect(badge.classes().join(" ")).toContain("amber");
  });

  it("renders the valley badge in the normal (non-warning) color", () => {
    const wrapper = mountBadge({
      state: "valley",
      label: "谷价时段",
      timezone: "Asia/Shanghai",
      multiplier: 1
    });

    const badge = wrapper.get('[data-testid="deepseek-pricing-state-badge"]');
    expect(badge.text()).toContain("谷价时段");
    // 倍率为 1 时不重复显示，避免噪音。
    expect(badge.text()).not.toContain("1x");
    expect(badge.classes().join(" ")).toContain("emerald");
  });

  it("falls back to the built-in label when the backend label is empty", () => {
    const wrapper = mountBadge({ state: "peak", label: "  ", multiplier: 2 });
    expect(wrapper.get('[data-testid="deepseek-pricing-state-badge"]').text()).toContain(
      zhAdminOverview.groups.peakState.peak
    );
  });

  it("treats an unknown state as not enabled instead of erroring", () => {
    const wrapper = mountBadge({ state: "something-new", multiplier: 1 });
    const badge = wrapper.get('[data-testid="deepseek-pricing-state-badge"]');
    expect(badge.text()).toContain(zhAdminOverview.groups.peakState.none);
    expect(badge.classes().join(" ")).toContain("gray");
  });

  it("renders no badge at all when the backend field is missing", () => {
    // 老数据 / 接口尚未下发该字段：必须容错，不显示徽标而不是报错。
    expect(mountBadge(undefined).find('[data-testid="deepseek-pricing-state-badge"]').exists()).toBe(false);
    expect(mountBadge(null).find('[data-testid="deepseek-pricing-state-badge"]').exists()).toBe(false);
    expect(mountBadge({}).find('[data-testid="deepseek-pricing-state-badge"]').exists()).toBe(false);
    expect(mountBadge({ state: "" }).find('[data-testid="deepseek-pricing-state-badge"]').exists()).toBe(false);
  });

  it("does not crash on a malformed multiplier", () => {
    const wrapper = mountBadge({ state: "peak", label: "峰", multiplier: Number.NaN });
    const badge = wrapper.get('[data-testid="deepseek-pricing-state-badge"]');
    expect(badge.text()).toContain("峰");
    expect(badge.text()).not.toContain("NaN");
  });

  it("explains that the peak/valley window is auto-judged in the tooltip", () => {
    const wrapper = mountBadge({ state: "peak", label: "峰", multiplier: 1 });
    expect(
      wrapper.get('[data-testid="deepseek-pricing-state-badge"]').attributes("title")
    ).toContain(zhAdminOverview.groups.peakState.autoJudged);
  });
});
