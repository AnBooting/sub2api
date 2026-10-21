import { defineComponent } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { AdminGroup } from "@/types";
import GroupsView from "@/views/admin/GroupsView.vue";
import en from "@/i18n/locales/en/admin/overview";
import zh from "@/i18n/locales/zh/admin/overview";

const {
  listGroups,
  getModelAllowlistCandidates,
  getUsageSummary,
  getCapacitySummary,
  getLiveCapability,
} = vi.hoisted(() => ({
  listGroups: vi.fn(),
  getModelAllowlistCandidates: vi.fn(),
  getUsageSummary: vi.fn(),
  getCapacitySummary: vi.fn(),
  getLiveCapability: vi.fn(),
}));

vi.mock("@/api/admin", () => ({
  adminAPI: {
    groups: {
      list: listGroups,
      getAll: vi.fn(),
      getModelAllowlistCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      create: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
      duplicate: vi.fn(),
      updateSortOrder: vi.fn(),
    },
    accounts: {
      list: vi.fn(),
      getById: vi.fn(),
    },
  },
}));

vi.mock("@/stores/app", () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
  }),
}));

vi.mock("@/stores/onboarding", () => ({
  useOnboardingStore: () => ({
    isCurrentStep: vi.fn(() => false),
    nextStep: vi.fn(),
  }),
}));

vi.mock("vue-i18n", async () => {
  const actual = await vi.importActual<typeof import("vue-i18n")>("vue-i18n");
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  };
});

const baseGroup = {
  id: 42,
  name: "Primary",
  description: null,
  platform: "openai",
  rate_multiplier: 1,
  rpm_limit: 0,
  is_exclusive: false,
  status: "active",
  subscription_type: "standard",
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  long_context_pricing_enabled: true,
  force_openai_fast: false,
  free_openai_fast: false,
  model_pricing: [],
  profit_control_enabled: false,
  profit_min_margin: 0,
  profit_safety_buffer: 0,
  allow_image_generation: false,
  allow_batch_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  batch_image_discount_multiplier: 0.5,
  batch_image_hold_multiplier: 0.6,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  video_price_480p: null,
  video_price_720p: null,
  video_price_1080p: null,
  web_search_price_per_call: null,
  search_price_per_1k: null,
  audio_realtime_price_per_min: null,
  audio_tts_price_per_million_chars: null,
  audio_stt_price_per_hour: null,
  peak_rate_enabled: false,
  peak_start: "",
  peak_end: "",
  peak_rate_multiplier: 1,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  allow_messages_dispatch: false,
  allow_live: false,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: "2026-09-05T00:00:00Z",
  updated_at: "2026-09-05T00:00:00Z",
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: true,
  supported_model_scopes: [],
  account_count: 1,
  active_account_count: 1,
  rate_limited_account_count: 0,
  codex_models_manifest_config: {
    enabled: false,
    account_ids: [],
    fallback_to_scheduler: false,
  },
  sort_order: 10,
} satisfies AdminGroup;

const AppLayoutStub = defineComponent({
  template: "<main><slot /></main>",
});

const TablePageLayoutStub = defineComponent({
  template:
    '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>',
});

const DataTableStub = defineComponent({
  props: {
    data: { type: Array, default: () => [] },
  },
  template:
    '<div><div v-if="data.length"><slot name="cell-actions" :row="data[0]" /></div></div>',
});

const BaseDialogStub = defineComponent({
  props: {
    show: { type: Boolean, default: false },
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
});

const mountView = () =>
  mount(GroupsView, {
    global: {
      plugins: [createPinia()],
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: true,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        EmptyState: true,
        Select: true,
        PlatformIcon: true,
        Icon: true,
        GroupCapacityBadge: true,
        GroupRateMultipliersModal: true,
        GroupRPMOverridesModal: true,
        ReasoningEffortPolicyFields: true,
        CodexManifestAccountsField: true,
        PricingEntryCard: true,
        VueDraggable: true,
      },
    },
  });

const openCreateModal = async (wrapper: ReturnType<typeof mountView>) => {
  await wrapper.get('[data-tour="groups-create-btn"]').trigger("click");
  await flushPromises();
};

const openEditModal = async (wrapper: ReturnType<typeof mountView>) => {
  const editButton = wrapper
    .findAll("button")
    .find((button) => button.text().includes("common.edit"));
  expect(editButton).toBeTruthy();
  await editButton!.trigger("click");
  await flushPromises();
};

describe("GroupsView Voice / Realtime group pricing", () => {
  beforeEach(() => {
    localStorage.clear();
    listGroups.mockReset();
    getModelAllowlistCandidates.mockReset();
    getUsageSummary.mockReset();
    getCapacitySummary.mockReset();
    getLiveCapability.mockReset();

    listGroups.mockResolvedValue({
      items: [baseGroup],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    });
    getModelAllowlistCandidates.mockResolvedValue([]);
    getUsageSummary.mockResolvedValue([]);
    getCapacitySummary.mockResolvedValue([]);
    getLiveCapability.mockResolvedValue({ supported: false });
  });

  it("shows the Voice / Realtime and web_search price fields for a non-Grok platform in the create form", async () => {
    const wrapper = mountView();
    await flushPromises();
    await openCreateModal(wrapper);

    // The create form defaults to the anthropic platform, i.e. a non-Grok platform.
    expect(wrapper.find('[data-testid="create-audio-realtime-price"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="create-audio-tts-price"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="create-audio-stt-price"]').exists()).toBe(true);

    // The web_search price is a group level field, so it is shown for every platform.
    expect(wrapper.find('[data-testid="create-search-price"]').exists()).toBe(true);

    wrapper.unmount();
  });

  it("shows the Voice / Realtime and web_search price fields for a non-Grok group in the edit form", async () => {
    const wrapper = mountView();
    await flushPromises();
    await openEditModal(wrapper);

    expect(wrapper.find('[data-testid="edit-audio-realtime-price"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="edit-audio-tts-price"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="edit-audio-stt-price"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="edit-search-price"]').exists()).toBe(true);

    wrapper.unmount();
  });

  it("keeps the Grok web_search price visible for Grok groups alongside the Voice / Realtime prices", async () => {
    listGroups.mockResolvedValue({
      items: [{ ...baseGroup, platform: "grok" }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    });

    const wrapper = mountView();
    await flushPromises();
    await openEditModal(wrapper);

    expect(wrapper.find('[data-testid="edit-search-price"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="edit-audio-realtime-price"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="edit-audio-tts-price"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="edit-audio-stt-price"]').exists()).toBe(true);

    wrapper.unmount();
  });

  it("binds the stored Voice / Realtime prices of a non-Grok group into the edit form", async () => {
    listGroups.mockResolvedValue({
      items: [
        {
          ...baseGroup,
          audio_realtime_price_per_min: 0.08,
          audio_tts_price_per_million_chars: 12.5,
          audio_stt_price_per_hour: 0.25,
        },
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    });

    const wrapper = mountView();
    await flushPromises();
    await openEditModal(wrapper);

    const realtime = wrapper.get('[data-testid="edit-audio-realtime-price"]');
    const tts = wrapper.get('[data-testid="edit-audio-tts-price"]');
    const stt = wrapper.get('[data-testid="edit-audio-stt-price"]');

    expect((realtime.element as HTMLInputElement).value).toBe("0.08");
    expect((tts.element as HTMLInputElement).value).toBe("12.5");
    expect((stt.element as HTMLInputElement).value).toBe("0.25");

    wrapper.unmount();
  });

  it("uses platform-neutral Voice / Realtime copy in both locales", () => {
    expect(zh.groups.voicePricing.title).toBe("语音 / Realtime 显式定价");
    expect(en.groups.voicePricing.title).toBe("Voice / Realtime Pricing");

    // The Grok-only section must no longer claim to cover Voice pricing.
    expect(zh.groups.explicitPricing.title).not.toContain("Voice");
    expect(en.groups.explicitPricing.title).not.toContain("Voice");
  });
});
