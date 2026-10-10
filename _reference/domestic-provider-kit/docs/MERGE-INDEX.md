# 国模合并定位索引

以下是原文件行号和上下文摘录，不是可直接执行的补丁。完整函数见 reference；不要整文件覆盖目标项目。

## frontend/src/components/account/CreateAccountModal.vue

[完整参考文件](../reference/shared/frontend/src/components/account/CreateAccountModal.vue)

```text
162:           </button>
163:           <button
164:             type="button"
165:             @click="selectPlatformFamily('domestic')"
166:             :class="[
167:               'flex min-w-0 items-center justify-center gap-1.5 rounded-md px-2 py-2.5 text-sm font-medium transition-all',
168:               platformFamily === 'domestic'
169:                 ? 'bg-white text-indigo-700 shadow-sm dark:bg-dark-600 dark:text-indigo-300'
170:                 : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200'
171:             ]"
172:           >
173:             <Icon name="globe" size="sm" />
174:             国模
175:           </button>
176:           <button
177:             type="button"
178:             @click="selectPlatformFamily('media')"
179:             :class="[
180:               'flex min-w-0 items-center justify-center gap-1.5 rounded-md px-2 py-2.5 text-sm font-medium transition-all',
181:               platformFamily === 'media'
182:                 ? 'bg-white text-cyan-700 shadow-sm dark:bg-dark-600 dark:text-cyan-300'
```

```text
317:         >
318:           <p>{{ t('admin.accounts.vertexAnthropicHint') }}</p>
319:         </div>
320:       </div>
321: 
322:       <!-- Account Type Selection (OpenAI) -->
323:       <div v-if="form.platform === 'openai' && platformFamily !== 'domestic'">
324:         <label class="input-label">{{ t('admin.accounts.accountType') }}</label>
325:         <div class="mt-2 grid grid-cols-2 gap-3" data-tour="account-form-type">
326:           <button
327:             type="button"
328:             @click="accountCategory = 'oauth-based'"
329:             :class="[
330:               'flex items-center gap-3 rounded-lg border-2 p-3 text-left transition-all',
331:               accountCategory === 'oauth-based'
```

```text
375:             </div>
376:           </button>
377: 
378:         </div>
379:       </div>
380: 
381:       <!-- Domestic OpenAI-compatible providers -->
382:       <div v-if="platformFamily === 'domestic'">
383:         <div class="flex items-end justify-between gap-4">
384:           <div>
385:             <label class="input-label">国内厂商</label>
386:             <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
387:               统一按 OpenAI 兼容协议接入，保存后按厂商名称和图标展示。
388:             </p>
389:           </div>
390:           <span class="shrink-0 rounded-full bg-indigo-50 px-2.5 py-1 text-xs font-medium text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-300">
391:             {{ selectedDomesticProvider.company }}
392:           </span>
393:         </div>
394:         <div class="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4" data-tour="account-form-domestic-provider">
395:           <button
396:             v-for="provider in domesticProviders"
397:             :key="provider.id"
398:             type="button"
399:             @click="selectDomesticProvider(provider.id)"
400:             :class="[
401:               'group flex min-h-[72px] items-center gap-3 rounded-xl border-2 px-3 py-3 text-left transition-all',
402:               domesticProvider === provider.id
403:                 ? 'border-indigo-500 bg-indigo-50 shadow-sm dark:border-indigo-500 dark:bg-indigo-900/20'
404:                 : 'border-gray-200 bg-white hover:border-indigo-300 hover:bg-indigo-50/50 dark:border-dark-600 dark:bg-dark-700 dark:hover:border-indigo-700'
405:             ]"
406:           >
407:             <span
408:               :class="[
409:                 'flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-white shadow-sm ring-1 ring-gray-100 dark:bg-dark-600 dark:ring-dark-500',
410:                 domesticProvider === provider.id ? 'ring-indigo-200 dark:ring-indigo-700' : ''
411:               ]"
412:             >
413:               <ModelIcon v-if="provider.iconModel" :model="provider.iconModel" size="20px" />
414:               <Icon v-else name="globe" size="sm" class="text-indigo-600 dark:text-indigo-300" />
415:             </span>
416:             <span class="min-w-0">
417:               <span class="block truncate text-sm font-semibold text-gray-900 dark:text-white">{{ provider.label }}</span>
418:               <span class="mt-0.5 block truncate text-[11px] text-gray-500 dark:text-gray-400">{{ provider.company }}</span>
```

```text
1241:           <label class="input-label">{{ t('admin.accounts.baseUrl') }}</label>
1242:           <input
1243:             v-model="apiKeyBaseUrl"
1244:             type="text"
1245:             class="input"
1246:             :placeholder="
1247:               platformFamily === 'domestic'
1248:                 ? selectedDomesticProvider.baseUrl || 'https://your-provider.example/v1'
1249:                 : form.platform === 'openai'
1250:                 ? 'https://api.openai.com'
1251:                 : form.platform === 'gemini'
1252:                   ? 'https://generativelanguage.googleapis.com'
1253:                   : form.platform === 'grok'
1254:                     ? 'https://api.x.ai/v1'
1255:                     : 'https://api.anthropic.com'
1256:             "
```

```text
1267:           <input
1268:             v-model="apiKeyValue"
1269:             type="password"
1270:             required
1271:             class="input font-mono"
1272:             :placeholder="
1273:               platformFamily === 'domestic'
1274:                 ? '请输入厂商 API Key'
1275:                 : form.platform === 'openai'
1276:                 ? 'sk-proj-...'
1277:                 : form.platform === 'gemini'
1278:                   ? 'AIza...'
1279:                   : form.platform === 'grok'
1280:                     ? 'xai-...'
1281:                     : 'sk-ant-...'
1282:             "
1283:           />
1284:           <p v-if="apiKeyHint" class="input-hint">{{ apiKeyHint }}</p>
1285:         </div>
1286: 
1287:         <div
1288:           v-if="form.platform === 'openai' && platformFamily !== 'domestic'"
1289:           class="flex items-center justify-between gap-4 border-t border-gray-200 pt-4 dark:border-dark-600"
1290:         >
1291:           <div>
1292:             <label class="input-label mb-0">{{ t('admin.accounts.upstreamBilling.autoProbe') }}</label>
1293:             <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
1294:               {{ t('admin.accounts.upstreamBilling.autoProbeHint') }}
1295:             </p>
1296:           </div>
```

```text
2891:         <input v-model="expiresAtInput" type="datetime-local" class="input" />
2892:         <p class="input-hint">{{ t('admin.accounts.expiresAtHint') }}</p>
2893:       </div>
2894: 
2895:       <!-- OpenAI 自动透传开关（OAuth/API Key） -->
2896:       <div
2897:         v-if="form.platform === 'openai' && platformFamily !== 'domestic'"
2898:         class="border-t border-gray-200 pt-4 dark:border-dark-600"
2899:       >
2900:         <div class="flex items-center justify-between">
2901:           <div>
2902:             <label class="input-label mb-0">{{ t('admin.accounts.openai.oauthPassthrough') }}</label>
2903:             <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
2904:               {{ t('admin.accounts.openai.oauthPassthroughDesc') }}
2905:             </p>
```

```text
2955:           </button>
2956:         </div>
2957:       </div>
2958: 
2959:       <!-- OpenAI WS Mode 三态（off/ctx_pool/passthrough） -->
2960:       <div
2961:         v-if="form.platform === 'openai' && platformFamily !== 'domestic' && (accountCategory === 'oauth-based' || accountCategory === 'apikey')"
2962:         class="border-t border-gray-200 pt-4 dark:border-dark-600"
2963:       >
2964:         <div class="flex items-center justify-between">
2965:           <div>
2966:             <label class="input-label mb-0">{{ t('admin.accounts.openai.wsMode') }}</label>
2967:             <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
2968:               {{ t('admin.accounts.openai.wsModeDesc') }}
2969:             </p>
```

```text
3044:           </select>
3045:         </div>
3046:       </div>
3047: 
3048:       <!-- OpenAI OAuth Codex 官方客户端限制开关 -->
3049:       <div
3050:         v-if="form.platform === 'openai' && platformFamily !== 'domestic' && (accountCategory === 'oauth-based' || accountCategory === 'apikey')"
3051:         class="border-t border-gray-200 pt-4 dark:border-dark-600"
3052:       >
3053:         <div class="flex items-center justify-between gap-4">
3054:           <div>
3055:             <label class="input-label mb-0">{{ t('admin.accounts.openai.longContextBilling') }}</label>
3056:             <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
3057:               {{ t('admin.accounts.openai.longContextBillingDesc') }}
3058:             </p>
```

```text
3076:             />
3077:           </button>
3078:         </div>
3079:       </div>
3080: 
3081:       <div
3082:         v-if="form.platform === 'openai' && platformFamily !== 'domestic' && accountCategory === 'oauth-based'"
3083:         class="border-t border-gray-200 pt-4 dark:border-dark-600"
3084:       >
3085:         <div class="flex items-center justify-between">
3086:           <div>
3087:             <label class="input-label mb-0">{{ t('admin.accounts.openai.codexCLIOnly') }}</label>
3088:             <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
3089:               {{ t('admin.accounts.openai.codexCLIOnlyDesc') }}
3090:             </p>
```

```text
3132:           </button>
3133:         </div>
3134:       </div>
3135: 
3136:       <!-- OpenAI Compact 能力配置 -->
3137:       <div
3138:         v-if="form.platform === 'openai' && platformFamily !== 'domestic' && (accountCategory === 'oauth-based' || accountCategory === 'apikey')"
3139:         class="border-t border-gray-200 pt-4 dark:border-dark-600 space-y-4"
3140:       >
3141:         <div class="flex items-center justify-between">
3142:           <div>
3143:             <label class="input-label mb-0">{{ t('admin.accounts.openai.compactMode') }}</label>
3144:             <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
3145:               {{ t('admin.accounts.openai.compactModeDesc') }}
3146:             </p>
```

```text
3171:           </button>
3172:         </div>
3173:       </div>
3174: 
3175:       <!-- OpenAI APIKey Responses API support mode -->
3176:       <div
3177:         v-if="form.platform === 'openai' && platformFamily !== 'domestic' && accountCategory === 'apikey'"
3178:         class="space-y-4 border-t border-gray-200 pt-4 dark:border-dark-600"
3179:       >
3180:         <div class="flex items-center justify-between gap-4">
3181:           <div>
3182:             <label class="input-label mb-0">{{ t('admin.accounts.openai.responsesMode') }}</label>
3183:             <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
3184:               {{ t('admin.accounts.openai.responsesModeDesc') }}
3185:             </p>
```

```text
3311:         <!-- Group Selection - 仅标准模式显示 -->
3312:         <GroupSelector
3313:           v-if="!authStore.isSimpleMode"
3314:           v-model="form.group_ids"
3315:           :groups="groups"
3316:           :platform="form.platform"
3317:           :domestic-provider="platformFamily === 'domestic' ? domesticProvider : ''"
3318:           :mixed-scheduling="mixedScheduling"
3319:           data-tour="account-form-groups"
3320:         />
3321:       </div>
3322: 
3323:     </form>
3324: 
3325:     <!-- Step 2: OAuth Authorization -->
```

```text
3748:   resolveOpenAIWSModeConcurrencyHintKey,
3749:   type OpenAIWSMode
3750: } from '@/utils/openaiWsMode'
3751: import OAuthAuthorizationFlow from './OAuthAuthorizationFlow.vue'
3752: import {
3753:   DOMESTIC_PROVIDERS,
3754:   type DomesticProviderId
3755: } from '@/utils/domesticProviders'
3756: 
3757: // Type for exposed OAuthAuthorizationFlow component
3758: // Note: defineExpose automatically unwraps refs, so we use the unwrapped types
3759: interface OAuthFlowExposed {
3760:   authCode: string
3761:   oauthState: string
3762:   projectId: string
3763:   sessionKey: string
```

```text
3780:   if (form.platform === 'grok') return t('admin.accounts.oauth.grok.title')
3781:   return t('admin.accounts.oauth.title')
3782: })
3783: 
3784: // Platform-specific hints for API Key type
3785: const baseUrlHint = computed(() => {
3786:   if (platformFamily.value === 'domestic') {
3787:     return '已按所选国内厂商预填 OpenAI 兼容地址，可按地域、业务空间或套餐要求修改。'
3788:   }
3789:   if (form.platform === 'openai') return t('admin.accounts.openai.baseUrlHint')
3790:   if (form.platform === 'gemini') return t('admin.accounts.gemini.baseUrlHint')
3791:   if (form.platform === 'grok') return ''
3792:   return t('admin.accounts.baseUrlHint')
3793: })
3794: 
3795: const apiKeyHint = computed(() => {
3796:   if (platformFamily.value === 'domestic') {
3797:     return `使用 ${selectedDomesticProvider.value.label} 控制台创建的 API Key。`
3798:   }
3799:   if (form.platform === 'openai') return t('admin.accounts.openai.apiKeyHint')
3800:   if (form.platform === 'gemini') return t('admin.accounts.gemini.apiKeyHint')
3801:   if (form.platform === 'grok') return ''
3802:   return t('admin.accounts.apiKeyHint')
3803: })
3804: 
3805: interface Props {
```

```text
3827:   protocol: MediaProtocol
3828:   baseUrl: string
3829:   capabilities: MediaCapability[]
3830:   description: string
3831: }
3832: 
3833: const domesticProviders = DOMESTIC_PROVIDERS
3834: 
3835: const platformFamily = ref<PlatformFamily>('anthropic')
3836: const domesticProvider = ref<DomesticProviderId>('deepseek')
3837: const selectedDomesticProvider = computed(() =>
3838:   domesticProviders.find((provider) => provider.id === domesticProvider.value) || domesticProviders[0]
3839: )
3840: 
3841: const mediaProtocol = ref<MediaProtocol>('new_api')
3842: const mediaProvider = ref<MediaProvider>('new_api')
3843: const mediaBaseUrl = ref('')
3844: const mediaApiKey = ref('')
3845: const mediaCapabilities = ref<MediaCapability[]>(['image', 'video', 'music'])
3846: const mediaModelsPath = ref('/v1/models')
```

```text
4109:   { value: 'force_on', label: t('admin.accounts.openai.compactModeForceOn') },
4110:   { value: 'force_off', label: t('admin.accounts.openai.compactModeForceOff') }
4111: ])
4112: const openAIResponsesModeOptions = computed(() => [
4113:   { value: 'auto', label: t('admin.accounts.openai.responsesModeAuto') },
4114:   { value: 'force_responses', label: t('admin.accounts.openai.responsesModeForceResponses') },
4115:   { value: 'force_chat_completions', label: t('admin.accounts.openai.responsesModeForceChatCompletions') }
4116: ])
4117: const openAITextEndpointCapabilityLabel = computed(() => {
4118:   if (openAIResponsesMode.value === 'force_responses') {
4119:     return t('admin.accounts.openai.capabilityResponses')
4120:   }
4121:   if (openAIResponsesMode.value === 'force_chat_completions') {
4122:     return t('admin.accounts.openai.capabilityChatCompletions')
4123:   }
4124:   return t('admin.accounts.openai.capabilityTextAuto')
4125: })
4126: const openAIEndpointCapabilityOptions = computed<{ value: OpenAIEndpointCapability; label: string }[]>(() => [
4127:   { value: 'chat_completions', label: openAITextEndpointCapabilityLabel.value },
4128:   { value: 'embeddings', label: t('admin.accounts.openai.capabilityEmbeddings') }
4129: ])
```

```text
4328:   priority: 1,
4329:   rate_multiplier: 1,
4330:   group_ids: [] as number[],
4331:   expires_at: null as number | null
4332: })
4333: 
4334: const selectDomesticProvider = (providerID: DomesticProviderId) => {
4335:   const previous = selectedDomesticProvider.value
4336:   domesticProvider.value = providerID
4337:   const next = selectedDomesticProvider.value
4338:   if (!form.name.trim() || form.name === `${previous.label} API`) {
4339:     form.name = `${next.label} API`
4340:   }
4341:   apiKeyBaseUrl.value = next.baseUrl
4342:   form.group_ids = []
4343:   allowedModels.value = []
4344:   modelMappings.value = []
4345: }
```

```text
4348:   platformFamily.value = family
4349:   if (family === 'domestic') {
4350:     form.platform = 'openai'
4351:     accountCategory.value = 'apikey'
4352:     form.type = 'apikey'
4353:     openaiPassthroughEnabled.value = false
4354:     openAIResponsesMode.value = 'force_chat_completions'
4355:     openAIEndpointCapabilities.value = ['chat_completions']
4356:     apiKeyBaseUrl.value = selectedDomesticProvider.value.baseUrl
4357:     allowedModels.value = []
4358:     modelMappings.value = []
4359:     if (!form.name.trim()) {
4360:       form.name = `${selectedDomesticProvider.value.label} API`
4361:     }
4362:     return
4363:   }
4364: 
4365:   if (family === 'media') {
4366:     form.platform = 'media' as AccountPlatform
4367:     accountCategory.value = 'apikey'
4368:     form.type = 'apikey'
```

```text
4490: // Reset platform-specific settings when platform changes
4491: watch(
4492:   () => form.platform,
4493:   (newPlatform) => {
4494:     // Reset base URL based on platform
4495:     apiKeyBaseUrl.value =
4496:       (platformFamily.value === 'domestic')
4497:         ? selectedDomesticProvider.value.baseUrl
4498:         : (newPlatform === 'openai')
4499:         ? 'https://api.openai.com'
4500:         : newPlatform === 'gemini'
4501:           ? 'https://generativelanguage.googleapis.com'
4502:           : newPlatform === 'grok'
4503:             ? 'https://api.x.ai/v1'
4504:             : 'https://api.anthropic.com'
4505:     // Clear model-related settings
```

```text
4621: }
4622: 
4623: // Auto-fill related models when switching to whitelist mode or changing platform
4624: watch(
4625:   [modelRestrictionMode, () => form.platform],
4626:   ([newMode]) => {
4627:     if (newMode === 'whitelist' && platformFamily.value !== 'domestic') {
4628:       allowedModels.value = [...getModelsByPlatform(form.platform)]
4629:     }
4630:   }
4631: )
4632: 
4633: watch(
4634:   [antigravityModelRestrictionMode, () => form.platform],
4635:   ([, platform]) => {
```

```text
4927: }
4928: 
4929: // Methods
4930: const resetForm = () => {
4931:   step.value = 1
4932:   platformFamily.value = 'anthropic'
4933:   domesticProvider.value = 'deepseek'
4934:   mediaProtocol.value = 'new_api'
4935:   mediaProvider.value = 'new_api'
4936:   mediaBaseUrl.value = ''
4937:   mediaApiKey.value = ''
4938:   mediaCapabilities.value = ['image', 'video', 'music']
4939:   mediaModelsPath.value = '/v1/models'
4940:   mediaModels.value = {}
4941:   mediaModelCosts.value = {}
```

```text
5054: 
5055: const buildOpenAIExtra = (base?: Record<string, unknown>): Record<string, unknown> | undefined => {
5056:   if (form.platform !== 'openai') {
5057:     return base
5058:   }
5059: 
5060:   if (platformFamily.value === 'domestic') {
5061:     const extra: Record<string, unknown> = { ...(base || {}) }
5062:     extra.domestic_provider = domesticProvider.value
5063:     extra.domestic_provider_label = selectedDomesticProvider.value.label
5064:     extra.domestic_provider_company = selectedDomesticProvider.value.company
5065:     return extra
5066:   }
5067: 
5068:   const extra: Record<string, unknown> = { ...(base || {}) }
5069:   if (accountCategory.value === 'oauth-based') {
5070:     extra.openai_oauth_responses_websockets_v2_mode = openaiOAuthResponsesWebSocketV2Mode.value
5071:     extra.openai_oauth_responses_websockets_v2_enabled = isOpenAIWSModeEnabled(openaiOAuthResponsesWebSocketV2Mode.value)
5072:   } else if (accountCategory.value === 'apikey') {
```

```text
5113: 
5114:   if (
5115:     accountCategory.value === 'apikey' &&
5116:     openAITextGenerationCapabilityEnabled.value &&
5117:     openAIResponsesMode.value !== 'auto'
5118:   ) {
5119:     extra.openai_responses_mode = openAIResponsesMode.value
5120:   } else {
5121:     delete extra.openai_responses_mode
5122:   }
5123: 
5124:   return Object.keys(extra).length > 0 ? extra : undefined
5125: }
5126: 
5127: const buildOpenAICodexImportExtra = (): Record<string, unknown> | undefined => {
5128:   const extra = buildOpenAIExtra()
5129:   if (!extra) {
```

```text
5453:     appStore.showError(t('admin.accounts.pleaseEnterApiKey'))
5454:     return
5455:   }
5456: 
5457:   // Determine default base URL based on platform
5458:   const defaultBaseUrl =
5459:     platformFamily.value === 'domestic'
5460:       ? selectedDomesticProvider.value.baseUrl
5461:       : form.platform === 'openai'
5462:       ? 'https://api.openai.com'
5463:       : form.platform === 'gemini'
5464:         ? 'https://generativelanguage.googleapis.com'
5465:         : form.platform === 'grok'
5466:           ? 'https://api.x.ai/v1'
5467:           : 'https://api.anthropic.com'
5468: 
```

```text
5479:   if (!isOpenAIModelRestrictionDisabled.value) {
5480:     const modelMapping = buildModelMappingObject(modelRestrictionMode.value, allowedModels.value, modelMappings.value)
5481:     if (modelMapping) {
5482:       credentials.model_mapping = modelMapping
5483:     }
5484:   }
5485:   if (form.platform === 'openai' && platformFamily.value !== 'domestic') {
5486:     applyOpenAIEndpointCapabilities(credentials)
5487:     const compactModelMapping = buildOpenAICompactModelMapping()
5488:     if (compactModelMapping) {
5489:       credentials.compact_model_mapping = compactModelMapping
5490:     }
5491:   }
5492: 
5493:   // Add pool mode if enabled
```

```text
5528: 
5529:   await doCreateAccount({
5530:     ...form,
5531:     group_ids: form.group_ids,
5532:     extra,
5533:     upstream_billing_probe_enabled:
5534:       form.platform === 'openai' && platformFamily.value !== 'domestic'
5535:         ? upstreamBillingAutoProbeEnabled.value
5536:         : undefined,
5537:     auto_pause_on_expired: autoPauseOnExpired.value
5538:   })
5539: }
5540: 
5541: const goBackToBasicInfo = () => {
5542:   step.value = 1
```

## frontend/src/components/account/EditAccountModal.vue

[完整参考文件](../reference/shared/frontend/src/components/account/EditAccountModal.vue)

```text
68:           :sync-supported="editMediaProvider !== 'kling'"
69:         />
70:       </div>
71: 
72:       <!-- API Key fields (only for apikey type) -->
73:       <div v-if="account.type === 'apikey' && account.platform !== 'media'" class="space-y-4">
74:         <div v-if="isDomesticAccount">
75:           <div class="flex items-end justify-between gap-4">
76:             <div>
77:               <label class="input-label">国内厂商</label>
78:               <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
79:                 修改厂商会同步更新默认 Base URL，保存后列表图标也会一起更新。
80:               </p>
81:             </div>
82:             <span class="shrink-0 rounded-full bg-indigo-50 px-2.5 py-1 text-xs font-medium text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-300">
83:               {{ selectedDomesticProvider.company }}
84:             </span>
85:           </div>
86:           <div class="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
87:             <button
88:               v-for="provider in domesticProviders"
89:               :key="provider.id"
90:               type="button"
91:               @click="selectDomesticProvider(provider.id)"
92:               :class="[
93:                 'flex min-h-[68px] items-center gap-3 rounded-xl border-2 px-3 py-3 text-left transition-all',
94:                 domesticProvider === provider.id
95:                   ? 'border-indigo-500 bg-indigo-50 shadow-sm dark:border-indigo-500 dark:bg-indigo-900/20'
96:                   : 'border-gray-200 bg-white hover:border-indigo-300 hover:bg-indigo-50/50 dark:border-dark-600 dark:bg-dark-700 dark:hover:border-indigo-700'
97:               ]"
98:             >
99:               <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-white shadow-sm ring-1 ring-gray-100 dark:bg-dark-600 dark:ring-dark-500">
100:                 <ModelIcon v-if="provider.iconModel" :model="provider.iconModel" size="20px" />
101:                 <Icon v-else name="globe" size="sm" class="text-indigo-600 dark:text-indigo-300" />
102:               </span>
```

```text
111:           <label class="input-label">{{ t('admin.accounts.baseUrl') }}</label>
112:           <input
113:             v-model="editBaseUrl"
114:             type="text"
115:             class="input"
116:             :placeholder="
117:               isDomesticAccount
118:                 ? selectedDomesticProvider.baseUrl || 'https://your-provider.example/v1'
119:                 : account.platform === 'openai'
120:                 ? 'https://api.openai.com'
121:                 : account.platform === 'gemini'
122:                   ? 'https://generativelanguage.googleapis.com'
123:                   : account.platform === 'antigravity'
124:                     ? 'https://cloudcode-pa.googleapis.com'
125:                     : account.platform === 'grok'
126:                       ? 'https://api.x.ai/v1'
```

```text
142:             class="input font-mono"
143:             autocomplete="new-password"
144:             data-1p-ignore
145:             data-lpignore="true"
146:             data-bwignore="true"
147:             :placeholder="
148:               isDomesticAccount
149:                 ? '请输入厂商 API Key'
150:                 : account.platform === 'openai'
151:                 ? 'sk-proj-...'
152:                 : account.platform === 'gemini'
153:                   ? 'AIza...'
154:                   : account.platform === 'antigravity'
155:                     ? 'sk-...'
156:                     : account.platform === 'grok'
```

```text
344:                 </button>
345:               </div>
346:             </div>
347:           </template>
348:         </div>
349: 
350:         <!-- Domestic provider Responses API support mode -->
351:         <div
352:           v-if="isDomesticAccount && account?.type === 'apikey'"
353:           class="border-t border-gray-200 pt-4 dark:border-dark-600"
354:         >
355:           <div class="flex items-center justify-between gap-4">
356:             <div>
357:               <label class="input-label mb-0">{{ t('admin.accounts.openai.responsesMode') }}</label>
358:               <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
359:                 {{ t('admin.accounts.openai.responsesModeDesc') }}
360:               </p>
```

```text
1558:         <input v-model="expiresAtInput" type="datetime-local" class="input" />
1559:         <p class="input-hint">{{ t('admin.accounts.expiresAtHint') }}</p>
1560:       </div>
1561: 
1562:       <!-- OpenAI 自动透传开关（OAuth/API Key） -->
1563:       <div
1564:         v-if="account?.platform === 'openai' && !isDomesticAccount && (account?.type === 'oauth' || account?.type === 'setup-token' || account?.type === 'apikey')"
1565:         class="border-t border-gray-200 pt-4 dark:border-dark-600"
1566:       >
1567:         <div class="flex items-center justify-between">
1568:           <div>
1569:             <label class="input-label mb-0">{{ t('admin.accounts.openai.oauthPassthrough') }}</label>
1570:             <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
1571:               {{ t('admin.accounts.openai.oauthPassthroughDesc') }}
1572:             </p>
```

```text
1622:           </button>
1623:         </div>
1624:       </div>
1625: 
1626:       <!-- OpenAI Codex hosted image_generation bridge policy -->
1627:       <div
1628:         v-if="account?.platform === 'openai' && !isDomesticAccount && (account?.type === 'oauth' || account?.type === 'setup-token' || account?.type === 'apikey')"
1629:         class="border-t border-gray-200 pt-4 dark:border-dark-600"
1630:       >
1631:         <div class="overflow-hidden rounded-lg border border-sky-100 bg-sky-50/60 shadow-sm dark:border-sky-900/50 dark:bg-sky-950/20">
1632:           <div class="flex items-start gap-3 px-4 py-3">
1633:             <div class="mt-0.5 flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-white text-sky-600 shadow-sm ring-1 ring-sky-100 dark:bg-dark-800 dark:text-sky-300 dark:ring-sky-900/60">
1634:               <Icon name="sparkles" size="sm" />
1635:             </div>
1636:             <div class="min-w-0 flex-1">
```

```text
1682:           </div>
1683:         </div>
1684:       </div>
1685: 
1686:       <!-- OpenAI WS Mode 三态（off/ctx_pool/passthrough） -->
1687:       <div
1688:         v-if="account?.platform === 'openai' && !isDomesticAccount && (account?.type === 'oauth' || account?.type === 'setup-token' || account?.type === 'apikey')"
1689:         class="border-t border-gray-200 pt-4 dark:border-dark-600"
1690:       >
1691:         <div class="flex items-center justify-between">
1692:           <div>
1693:             <label class="input-label mb-0">{{ t('admin.accounts.openai.wsMode') }}</label>
1694:             <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
1695:               {{ t('admin.accounts.openai.wsModeDesc') }}
1696:             </p>
```

```text
1703:           </div>
1704:         </div>
1705:       </div>
1706: 
1707:       <!-- OpenAI APIKey Responses API support mode -->
1708:       <div
1709:         v-if="account?.platform === 'openai' && !isDomesticAccount && account?.type === 'apikey'"
1710:         class="space-y-4 border-t border-gray-200 pt-4 dark:border-dark-600"
1711:       >
1712:         <div class="flex items-center justify-between gap-4">
1713:           <div>
1714:             <label class="input-label mb-0">{{ t('admin.accounts.openai.responsesMode') }}</label>
1715:             <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
1716:               {{ t('admin.accounts.openai.responsesModeDesc') }}
1717:             </p>
```

```text
1758:           </div>
1759:           <p class="input-hint">{{ t('admin.accounts.openai.endpointCapabilitiesDesc') }}</p>
1760:         </div>
1761:       </div>
1762: 
1763:       <div
1764:         v-if="account?.platform === 'openai' && !isDomesticAccount && account?.type === 'apikey'"
1765:         class="flex items-center justify-between gap-4 border-t border-gray-200 pt-4 dark:border-dark-600"
1766:       >
1767:         <div>
1768:           <label class="input-label mb-0">{{ t('admin.accounts.upstreamBilling.autoProbe') }}</label>
1769:           <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
1770:             {{ t('admin.accounts.upstreamBilling.autoProbeHint') }}
1771:           </p>
1772:         </div>
```

```text
1953:           @update:quotaNotifyTotalThresholdType="quotaNotifyState.total.thresholdType = $event"
1954:         />
1955:       </div>
1956: 
1957:       <!-- OpenAI API 长上下文计费开关 -->
1958:       <div
1959:         v-if="account?.platform === 'openai' && !isDomesticAccount && !isSparkShadow && (account?.type === 'oauth' || account?.type === 'setup-token' || account?.type === 'apikey')"
1960:         class="border-t border-gray-200 pt-4 dark:border-dark-600"
1961:       >
1962:         <div class="flex items-center justify-between gap-4">
1963:           <div>
1964:             <label class="input-label mb-0">{{ t('admin.accounts.openai.longContextBilling') }}</label>
1965:             <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
1966:               {{ t('admin.accounts.openai.longContextBillingDesc') }}
1967:             </p>
```

```text
1985:             />
1986:           </button>
1987:         </div>
1988:       </div>
1989: 
1990:       <div
1991:         v-if="account?.platform === 'openai' && !isDomesticAccount && (account?.type === 'oauth' || account?.type === 'setup-token')"
1992:         class="border-t border-gray-200 pt-4 dark:border-dark-600"
1993:       >
1994:         <div class="flex items-center justify-between">
1995:           <div>
1996:             <label class="input-label mb-0">{{ t('admin.accounts.openai.codexCLIOnly') }}</label>
1997:             <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
1998:               {{ t('admin.accounts.openai.codexCLIOnlyDesc') }}
1999:             </p>
```

```text
2041:           </button>
2042:         </div>
2043:       </div>
2044: 
2045:       <!-- OpenAI 订阅档位手动覆盖（Plus/Pro/Free），仅 OAuth 非影子账号 -->
2046:       <div
2047:         v-if="account?.platform === 'openai' && !isDomesticAccount && account?.type === 'oauth' && !isSparkShadow"
2048:         class="border-t border-gray-200 pt-4 dark:border-dark-600"
2049:       >
2050:         <div class="flex items-center justify-between gap-4">
2051:           <div class="min-w-0">
2052:             <label class="input-label mb-0">{{ t('admin.accounts.openai.planType') }}</label>
2053:             <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
2054:               {{ t('admin.accounts.openai.planTypeDesc') }}
2055:             </p>
```

```text
2058:             <Select v-model="editPlanType" :options="planTypeOptions" />
2059:           </div>
2060:         </div>
2061:       </div>
2062: 
2063:       <div
2064:         v-if="account?.platform === 'openai' && !isDomesticAccount && (account?.type === 'oauth' || account?.type === 'setup-token' || account?.type === 'apikey')"
2065:         class="border-t border-gray-200 pt-4 dark:border-dark-600 space-y-4"
2066:       >
2067:         <div class="flex items-center justify-between">
2068:           <div>
2069:             <label class="input-label mb-0">{{ t('admin.accounts.openai.compactMode') }}</label>
2070:             <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
2071:               {{ t('admin.accounts.openai.compactModeDesc') }}
2072:             </p>
```

```text
2144:             />
2145:           </button>
2146:         </div>
2147:       </div>
2148: 
2149:       <div
2150:         v-if="account?.platform === 'openai' && !isDomesticAccount"
2151:         class="border-t border-gray-200 pt-4 dark:border-dark-600 space-y-4"
2152:       >
2153:         <div class="space-y-2">
2154:           <div class="flex items-center justify-between">
2155:             <label class="input-label mb-0">{{ t('admin.accounts.autoPause5hDisabled') }}</label>
2156:             <button
2157:               type="button"
2158:               @click="autoPause5hDisabled = !autoPause5hDisabled"
```

```text
2672:       <!-- Group Selection - 仅标准模式显示 -->
2673:       <GroupSelector
2674:         v-if="!authStore.isSimpleMode"
2675:         v-model="form.group_ids"
2676:         :groups="groups"
2677:         :platform="account?.platform"
2678:         :domestic-provider="isDomesticAccount ? domesticProvider : ''"
2679:         :mixed-scheduling="mixedScheduling"
2680:         data-tour="account-form-groups"
2681:       />
2682: 
2683:       <AccountIntelligenceSettings v-if="account?.platform === 'openai'" v-model="intelligenceConfig" />
2684:     </form>
2685: 
2686:     <template #footer>
```

```text
2807:   buildModelMappingObject,
2808:   splitModelMappingObject,
2809:   isValidWildcardPattern
2810: } from '@/composables/useModelWhitelist'
2811: import {
2812:   DOMESTIC_PROVIDERS,
2813:   normalizeDomesticProviderId,
2814:   type DomesticProviderId
2815: } from '@/utils/domesticProviders'
2816: 
2817: interface Props {
2818:   show: boolean
2819:   account: Account | null
2820:   proxies: Proxy[]
2821:   groups: AdminGroup[]
2822: }
2823: 
```

```text
2828: }>()
2829: 
2830: const { t } = useI18n()
2831: const appStore = useAppStore()
2832: const authStore = useAuthStore()
2833: 
2834: const domesticProviders = DOMESTIC_PROVIDERS
2835: 
2836: type EditMediaProtocol = 'new_api' | 'official'
2837: type EditMediaCapability = 'image' | 'video' | 'music'
2838: const editMediaProtocol = ref<EditMediaProtocol>('new_api')
2839: const editMediaProvider = ref('new_api')
2840: const editMediaCapabilities = ref<EditMediaCapability[]>(['image', 'video', 'music'])
2841: const editMediaModelsPath = ref('/v1/models')
2842: const editMediaModels = ref<Partial<Record<EditMediaCapability, string[]>>>({})
```

```text
2865: }
2866: const resetEditMediaProvider = () => {
2867:   editMediaProvider.value = editMediaProtocol.value === 'new_api' ? 'new_api' : 'openai'
2868:   syncEditMediaCapabilities()
2869: }
2870: 
2871: const domesticProvider = ref<DomesticProviderId>('custom')
2872: const selectedDomesticProvider = computed(() =>
2873:   domesticProviders.find((provider) => provider.id === domesticProvider.value) || domesticProviders[domesticProviders.length - 1]
2874: )
2875: const isDomesticAccount = computed(() => {
2876:   const raw = props.account?.extra?.domestic_provider
2877:   return props.account?.platform === 'openai' && props.account?.type === 'apikey' && typeof raw === 'string' && raw.length > 0
2878: })
2879: 
2880: const selectDomesticProvider = (providerID: DomesticProviderId) => {
2881:   domesticProvider.value = providerID
2882:   editBaseUrl.value = selectedDomesticProvider.value.baseUrl
2883:   form.group_ids = []
2884: }
2885: 
2886: // Spark 影子账号(parent_account_id 非空):代理恒继承母账号,不可独立编辑(外审 B/P1),
2887: // 故隐藏代理选择器。
2888: const isSparkShadow = computed(() => props.account?.parent_account_id != null)
2889: 
2890: const handleOllamaCloudUsageUpdated = (state: OllamaCloudUsageState) => {
2891:   if (props.account) emit('updated', { ...props.account, ollama_cloud_usage: state })
2892: }
2893: 
2894: // Platform-specific hint for Base URL
2895: const baseUrlHint = computed(() => {
2896:   if (!props.account) return t('admin.accounts.baseUrlHint')
2897:   if (isDomesticAccount.value) {
2898:     return '当前账号按国内厂商分类，底层仍使用 OpenAI 兼容的 Base URL 与 API Key。'
2899:   }
2900:   if (props.account.platform === 'openai') return t('admin.accounts.openai.baseUrlHint')
2901:   if (props.account.platform === 'gemini') return t('admin.accounts.gemini.baseUrlHint')
2902:   if (props.account.platform === 'grok') return ''
2903:   return t('admin.accounts.baseUrlHint')
2904: })
2905: 
```

```text
3184: const planTypeOptions = computed(() =>
3185:   buildPlanTypeOptions(editPlanType.value, t('admin.accounts.openai.planTypeClear'))
3186: )
3187: const openAIResponsesModeOptions = computed(() => [
3188:   { value: 'auto', label: t('admin.accounts.openai.responsesModeAuto') },
3189:   { value: 'force_responses', label: t('admin.accounts.openai.responsesModeForceResponses') },
3190:   { value: 'force_chat_completions', label: t('admin.accounts.openai.responsesModeForceChatCompletions') }
3191: ])
3192: const openAITextEndpointCapabilityLabel = computed(() => {
3193:   if (openAIResponsesMode.value === 'force_responses') {
3194:     return t('admin.accounts.openai.capabilityResponses')
3195:   }
3196:   if (openAIResponsesMode.value === 'force_chat_completions') {
3197:     return t('admin.accounts.openai.capabilityChatCompletions')
3198:   }
3199:   const extra = props.account?.extra as Record<string, unknown> | undefined
3200:   if (extra?.openai_responses_supported === true) {
3201:     return t('admin.accounts.openai.capabilityResponsesAuto')
3202:   }
3203:   if (extra?.openai_responses_supported === false) {
3204:     return t('admin.accounts.openai.capabilityChatCompletionsAuto')
```

```text
3266:     delete credentials.openai_capabilities
3267:     return
3268:   }
3269:   credentials.openai_capabilities = capabilities
3270: }
3271: const normalizeOpenAIResponsesMode = (mode: unknown): OpenAIResponsesMode => {
3272:   if (mode === 'force_responses' || mode === 'force_chat_completions') {
3273:     return mode
3274:   }
3275:   return 'auto'
3276: }
3277: const isOpenAIModelRestrictionDisabled = computed(() =>
3278:   props.account?.platform === 'openai' && openaiPassthroughEnabled.value
3279: )
3280: const openAIResponsesStatusKey = computed(() => {
3281:   if (openAIResponsesMode.value === 'force_responses') {
3282:     return 'admin.accounts.openai.responsesStatusForcedResponses'
3283:   }
3284:   if (openAIResponsesMode.value === 'force_chat_completions') {
3285:     return 'admin.accounts.openai.responsesStatusForcedChatCompletions'
3286:   }
3287:   const extra = props.account?.extra as Record<string, unknown> | undefined
3288:   if (extra?.openai_responses_supported === true) {
3289:     return 'admin.accounts.openai.responsesStatusAutoSupported'
3290:   }
3291:   if (extra?.openai_responses_supported === false) {
3292:     return 'admin.accounts.openai.responsesStatusAutoUnsupported'
```

```text
3338:     }
3339:   }
3340: ])
3341: 
3342: // Computed: default base URL based on platform
3343: const defaultBaseUrl = computed(() => {
3344:   if (isDomesticAccount.value) return selectedDomesticProvider.value.baseUrl
3345:   if (props.account?.platform === 'openai') return 'https://api.openai.com'
3346:   if (props.account?.platform === 'gemini') return 'https://generativelanguage.googleapis.com'
3347:   if (props.account?.platform === 'grok') return 'https://api.x.ai/v1'
3348:   return 'https://api.anthropic.com'
3349: })
3350: 
3351: const mixedChannelWarningMessageText = computed(() => {
3352:   if (mixedChannelWarningDetails.value) {
```

```text
3498:       : ''
3499: 
3500:   // Load mixed scheduling setting (only for antigravity accounts)
3501:   mixedScheduling.value = false
3502:   allowOverages.value = false
3503: 	const extra = newAccount.extra as Record<string, unknown> | undefined
3504: 	const rawDomesticProvider = typeof extra?.domestic_provider === 'string'
3505: 	  ? extra.domestic_provider.trim().toLowerCase()
3506: 	  : ''
3507: 	domesticProvider.value = normalizeDomesticProviderId(rawDomesticProvider)
3508: 	if (rawDomesticProvider && newAccount.type === 'apikey') {
3509: 		openAIResponsesMode.value = normalizeOpenAIResponsesMode(extra?.openai_responses_mode)
3510: 	}
3511: 	mixedScheduling.value = extra?.mixed_scheduling === true
3512: 	allowOverages.value = extra?.allow_overages === true
3513: 	autoPause5hThreshold.value = typeof extra?.auto_pause_5h_threshold === 'number' ? extra.auto_pause_5h_threshold * 100 : null
3514: 	autoPause7dThreshold.value = typeof extra?.auto_pause_7d_threshold === 'number' ? extra.auto_pause_7d_threshold * 100 : null
3515: 	autoPause5hDisabled.value = extra?.auto_pause_5h_disabled === true
3516: 	autoPause7dDisabled.value = extra?.auto_pause_7d_disabled === true
3517: 	upstreamBillingAutoProbeEnabled.value = extra?.upstream_billing_probe_enabled === true
```

```text
3530:   codexCLIOnlyEnabled.value = false
3531:   codexCLIOnlyAppServerEnabled.value = false
3532:   codexImageToolMode.value = 'inherit'
3533:   anthropicPassthroughEnabled.value = false
3534:   anthropicAPIKeyAuthScheme.value = 'x_api_key'
3535:   webSearchEmulationMode.value = 'default'
3536:   if (!rawDomesticProvider && newAccount.platform === 'openai' && (newAccount.type === 'oauth' || newAccount.type === 'setup-token' || newAccount.type === 'apikey')) {
3537:     openaiPassthroughEnabled.value = extra?.openai_passthrough === true || extra?.openai_oauth_passthrough === true
3538:     openaiFlattenNamespacesEnabled.value =
3539:       newAccount.type === 'oauth' && extra?.openai_responses_flatten_namespaces === true
3540:     const longContextBillingValue = extra?.openai_long_context_billing_enabled
3541:     openAILongContextBillingEnabled.value = longContextBillingValue === true
3542:     // plan_type 手动覆盖仅 OAuth 有实际调度语义(IsOpenAIChatGPTSubscription 要求 oauth),故只对 oauth 回填
3543:     editPlanType.value = newAccount.type === 'oauth'
3544:       ? readPlanType(newAccount.credentials as Record<string, unknown> | undefined)
3545:       : ''
3546:     openAICompactMode.value = (extra?.openai_compact_mode as OpenAICompactMode) || 'auto'
3547:     if (newAccount.type === 'apikey') {
3548:       openAIResponsesMode.value = normalizeOpenAIResponsesMode(extra?.openai_responses_mode)
3549:       openAIEndpointCapabilities.value = readOpenAIEndpointCapabilities(
3550:         newAccount.credentials as Record<string, unknown> | undefined
3551:       )
3552:       if (!openAITextGenerationCapabilityEnabled.value) {
3553:         openAIResponsesMode.value = 'auto'
3554:       }
3555:     }
3556:     const codexImageGenerationBridgeValue = typeof extra?.codex_image_generation_bridge === 'boolean'
```

```text
3700:   }
3701: 
3702:   // Initialize API Key fields for apikey type
3703:   if (newAccount.type === 'apikey' && newAccount.credentials) {
3704:     const credentials = newAccount.credentials as Record<string, unknown>
3705:     const platformDefaultUrl =
3706:       rawDomesticProvider
3707:         ? selectedDomesticProvider.value.baseUrl
3708:         : newAccount.platform === 'openai'
3709:         ? 'https://api.openai.com'
3710:         : newAccount.platform === 'gemini'
3711:           ? 'https://generativelanguage.googleapis.com'
3712:           : newAccount.platform === 'grok'
3713:             ? 'https://api.x.ai/v1'
3714:             : 'https://api.anthropic.com'
3715:     editBaseUrl.value = (credentials.base_url as string) || platformDefaultUrl
```

```text
3773:     editVertexLocation.value = (credentials.location as string) || (credentials.vertex_location as string) || 'us-central1'
3774: 
3775:     // Load model mappings for service_account
3776:     loadModelRestrictionFromMapping(credentials.model_mapping as Record<string, unknown> | undefined)
3777:   } else {
3778:     const platformDefaultUrl =
3779:       rawDomesticProvider
3780:         ? selectedDomesticProvider.value.baseUrl
3781:         : newAccount.platform === 'openai'
3782:         ? 'https://api.openai.com'
3783:         : newAccount.platform === 'gemini'
3784:           ? 'https://generativelanguage.googleapis.com'
3785:           : newAccount.platform === 'grok'
3786:             ? 'https://api.x.ai/v1'
3787:             : 'https://api.anthropic.com'
3788:     editBaseUrl.value = platformDefaultUrl
```

```text
4349:         } else {
4350:           delete newCredentials.model_mapping
4351:         }
4352:       } else if (currentCredentials.model_mapping) {
4353:         newCredentials.model_mapping = currentCredentials.model_mapping
4354:       }
4355:       if (props.account.platform === 'openai' && !isDomesticAccount.value) {
4356:         applyOpenAIEndpointCapabilities(newCredentials)
4357:         const compactModelMapping = buildModelMappingObject('mapping', [], openAICompactModelMappings.value)
4358:         if (compactModelMapping) {
4359:           newCredentials.compact_model_mapping = compactModelMapping
4360:         } else {
4361:           delete newCredentials.compact_model_mapping
4362:         }
4363:       }
```

```text
4806:         newExtra.web_search_emulation = webSearchEmulationMode.value
4807:       }
4808:       updatePayload.extra = newExtra
4809:     }
4810: 
4811:     // For OpenAI OAuth/SetupToken/API Key accounts, handle passthrough mode in extra
4812:     if (props.account.platform === 'openai' && !isDomesticAccount.value && (props.account.type === 'oauth' || props.account.type === 'setup-token' || props.account.type === 'apikey')) {
4813:       const currentExtra = (props.account.extra as Record<string, unknown>) || {}
4814:       const newExtra: Record<string, unknown> = { ...currentExtra }
4815:       const hadCodexCLIOnlyEnabled = currentExtra.codex_cli_only === true
4816:       if (props.account.type === 'oauth' || props.account.type === 'setup-token') {
4817:         newExtra.openai_oauth_responses_websockets_v2_mode = openaiOAuthResponsesWebSocketV2Mode.value
4818:         newExtra.openai_oauth_responses_websockets_v2_enabled = isOpenAIWSModeEnabled(openaiOAuthResponsesWebSocketV2Mode.value)
4819:       } else if (props.account.type === 'apikey') {
4820:         newExtra.openai_apikey_responses_websockets_v2_mode = openaiAPIKeyResponsesWebSocketV2Mode.value
```

```text
4843:         delete newExtra.openai_compact_mode
4844:       } else {
4845:         newExtra.openai_compact_mode = openAICompactMode.value
4846:       }
4847: 		if (props.account.type === 'apikey') {
4848:         if (!openAITextGenerationCapabilityEnabled.value || openAIResponsesMode.value === 'auto') {
4849:           delete newExtra.openai_responses_mode
4850:         } else {
4851:           newExtra.openai_responses_mode = openAIResponsesMode.value
4852:         }
4853: 			newExtra.upstream_billing_probe_enabled = upstreamBillingAutoProbeEnabled.value
4854: 		}
4855: 		if (autoPause5hThreshold.value != null && autoPause5hThreshold.value > 0) {
4856: 			newExtra.auto_pause_5h_threshold = autoPause5hThreshold.value / 100
4857: 		} else {
4858: 			delete newExtra.auto_pause_5h_threshold
4859: 		}
```

```text
4908:       }
4909: 
4910:       updatePayload.extra = newExtra
4911:     }
4912: 
4913:     // 国内厂商仍走 OpenAI-compatible 通道，只额外保存展示分类，不改变后端平台枚举。
4914:     if (isDomesticAccount.value && props.account.type === 'apikey') {
4915:       const currentExtra = (updatePayload.extra as Record<string, unknown>) ||
4916:         (props.account.extra as Record<string, unknown>) || {}
4917:       const newExtra: Record<string, unknown> = { ...currentExtra }
4918:       newExtra.domestic_provider = domesticProvider.value
4919:       newExtra.domestic_provider_label = selectedDomesticProvider.value.label
4920:       newExtra.domestic_provider_company = selectedDomesticProvider.value.company
4921:       if (openAIResponsesMode.value === 'auto') {
4922:         delete newExtra.openai_responses_mode
4923:       } else {
4924:         newExtra.openai_responses_mode = openAIResponsesMode.value
4925:       }
4926:       updatePayload.extra = newExtra
4927:     }
4928: 
4929:     // For apikey/bedrock accounts, handle quota_limit in extra
4930:     if (props.account.type === 'apikey' || props.account.type === 'bedrock') {
4931:       const currentExtra = (updatePayload.extra as Record<string, unknown>) ||
4932:         (props.account.extra as Record<string, unknown>) || {}
```

## frontend/src/components/account/AccountGroupsCell.vue

[完整参考文件](../reference/shared/frontend/src/components/account/AccountGroupsCell.vue)

```text
4:     <div class="flex flex-wrap gap-1 max-h-14 overflow-hidden">
5:       <GroupBadge
6:         v-for="group in displayGroups"
7:         :key="group.id"
8:         :name="group.name"
9:         :platform="group.platform"
10:         :domestic-provider="group.domestic_provider"
11:         :subscription-type="group.subscription_type"
12:         :rate-multiplier="group.rate_multiplier"
13:         :show-rate="false"
14:         class="max-w-24"
15:       />
16:       <!-- 更多数量徽章 -->
17:       <button
18:         v-if="hiddenCount > 0"
```

```text
56:           <div class="flex flex-wrap gap-1.5 max-h-64 overflow-y-auto">
57:             <GroupBadge
58:               v-for="group in groups"
59:               :key="group.id"
60:               :name="group.name"
61:               :platform="group.platform"
62:               :domestic-provider="group.domestic_provider"
63:               :subscription-type="group.subscription_type"
64:               :rate-multiplier="group.rate_multiplier"
65:               :show-rate="false"
66:             />
67:           </div>
68:         </div>
69:       </Transition>
70:     </Teleport>
```

## frontend/src/views/admin/GroupsView.vue

[完整参考文件](../reference/shared/frontend/src/views/admin/GroupsView.vue)

```text
135:           </template>
136: 
137:           <template #cell-platform="{ row, value }">
138:             <span
139:               :class="[
140:                 'inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium',
141:                 row.domestic_provider
142:                   ? 'bg-indigo-100 text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-300'
143:                   : value === 'anthropic'
144:                   ? 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
145:                   : value === 'openai'
146:                     ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
147:                     : value === 'antigravity'
148:                       ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
149:                       : value === 'grok'
150:                         ? 'bg-zinc-200 text-zinc-800 dark:bg-zinc-700 dark:text-zinc-100'
151:                         : 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400',
152:               ]"
153:             >
154:               <ModelIcon
155:                 v-if="domesticProviderMeta(row.domestic_provider)?.iconModel"
156:                 :model="domesticProviderMeta(row.domestic_provider)!.iconModel"
157:                 size="12px"
158:               />
159:               <Icon v-else-if="row.domestic_provider" name="globe" size="xs" />
160:               <PlatformIcon v-else :platform="value" size="xs" />
161:               {{ groupPlatformLabel(row) }}
162:             </span>
163:           </template>
164: 
165:           <template #cell-billing_type="{ row }">
166:             <div class="space-y-1">
167:               <!-- Type Badge -->
```

```text
497:             v-model="createPlatformSelection"
498:             :options="platformOptions"
499:             data-tour="group-form-platform"
500:           />
501:           <p class="input-hint">{{ t("admin.groups.platformHint") }}</p>
502:         </div>
503:         <DomesticProviderPicker
504:           v-if="createForm.domestic_provider"
505:           v-model="createForm.domestic_provider"
506:           :catalog="domesticPricingCatalog"
507:           :loading="domesticPricingLoading"
508:         />
509:         <!-- 从分组复制账号 -->
510:         <div v-if="copyAccountsGroupOptions.length > 0">
511:           <div class="mb-1.5 flex items-center gap-1">
512:             <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
513:               {{ t("admin.groups.copyAccounts.title") }}
514:             </label>
515:             <div class="group relative inline-flex">
```

```text
2055:             :options="platformOptions"
2056:             :disabled="true"
2057:             data-tour="group-form-platform"
2058:           />
2059:           <p class="input-hint">{{ t("admin.groups.platformNotEditable") }}</p>
2060:         </div>
2061:         <DomesticProviderPicker
2062:           v-if="editForm.domestic_provider"
2063:           v-model="editForm.domestic_provider"
2064:           :catalog="domesticPricingCatalog"
2065:           :loading="domesticPricingLoading"
2066:         />
2067:         <!-- 从分组复制账号（编辑时） -->
2068:         <div v-if="copyAccountsGroupOptionsForEdit.length > 0">
2069:           <div class="mb-1.5 flex items-center gap-1">
2070:             <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
2071:               {{ t("admin.groups.copyAccounts.title") }}
2072:             </label>
2073:             <div class="group relative inline-flex">
```

```text
4088: import ModelIcon from "@/components/common/ModelIcon.vue";
4089: import Icon from "@/components/icons/Icon.vue";
4090: import GroupRateMultipliersModal from "@/components/admin/group/GroupRateMultipliersModal.vue";
4091: import GroupRPMOverridesModal from "@/components/admin/group/GroupRPMOverridesModal.vue";
4092: import GroupCapacityBadge from "@/components/common/GroupCapacityBadge.vue";
4093: import ReasoningEffortPolicyFields from "@/components/admin/group/ReasoningEffortPolicyFields.vue";
4094: import DomesticProviderPicker from "@/components/admin/group/DomesticProviderPicker.vue";
4095: import type { DomesticPricingCatalogEntry } from "@/api/admin/domesticPricing";
4096: import {
4097:   DOMESTIC_PROVIDERS,
4098:   getDomesticProvider,
4099:   type DomesticProviderId,
4100: } from "@/utils/domesticProviders";
4101: import { VueDraggable } from "vue-draggable-plus";
4102: import { createStableObjectKeyResolver } from "@/utils/stableObjectKey";
4103: import { extractApiErrorMessage } from "@/utils/apiError";
4104: import { useKeyedDebouncedSearch } from "@/composables/useKeyedDebouncedSearch";
4105: import { getPersistedPageSize } from "@/composables/usePersistedPageSize";
4106: import {
4107:   createDefaultMessagesDispatchFormState,
4108:   messagesDispatchConfigToFormState,
```

```text
4315:   { value: "false", label: t("admin.groups.nonExclusive") },
4316: ]);
4317: 
4318: const platformOptions = computed(() => [
4319:   { value: "anthropic", label: "Anthropic" },
4320:   { value: "openai", label: "OpenAI" },
4321:   { value: "domestic", label: "国模 · OpenAI 兼容" },
4322:   { value: "gemini", label: "Gemini" },
4323:   { value: "antigravity", label: "Antigravity" },
4324:   { value: "grok", label: "Grok" },
4325:   { value: "media", label: "媒体" },
4326:   { value: "composite", label: "Composite" },
4327: ]);
4328: 
4329: const platformFilterOptions = computed(() => [
4330:   { value: "", label: t("admin.groups.allPlatforms") },
4331:   { value: "anthropic", label: "Anthropic" },
4332:   { value: "openai", label: "OpenAI" },
4333:   { value: "domestic", label: "国模" },
4334:   { value: "gemini", label: "Gemini" },
4335:   { value: "antigravity", label: "Antigravity" },
4336:   { value: "grok", label: "Grok" },
4337:   { value: "media", label: "媒体" },
4338:   { value: "composite", label: "Composite" },
4339: ]);
4340: 
4341: const compositeRoutePlatformOptions = computed(() => [
```

```text
4460:   });
4461:   return options;
4462: });
4463: 
4464: const canCopyAccountsFromGroup = (
4465:   targetPlatform: GroupPlatform,
4466:   targetDomesticProvider: string,
4467:   source: AdminGroup,
4468: ) =>
4469:   targetPlatform === "composite" ||
4470:   (source.platform === targetPlatform &&
4471:     (source.domestic_provider || "") === (targetDomesticProvider || ""));
4472: 
4473: const copyAccountsGroupLabel = (g: AdminGroup) => {
4474:   const count = g.account_count || 0;
4475:   const platform = groupPlatformLabel(g);
4476:   return `${g.name} - ${platform} (${t("admin.groups.accountsCount", { count })})`;
4477: };
4478: 
4479: // 复制账号的源分组选项（创建时）- 相同平台；composite 分组可汇总各平台账号
4480: const copyAccountsGroupOptions = computed(() => {
4481:   const eligibleGroups = groups.value.filter(
4482:     (g) =>
4483:       canCopyAccountsFromGroup(createForm.platform, createForm.domestic_provider, g) &&
4484:       (g.account_count || 0) > 0,
4485:   );
4486:   return eligibleGroups.map((g) => ({
4487:     value: g.id,
4488:     label: copyAccountsGroupLabel(g),
4489:   }));
4490: });
4491: 
4492: // 复制账号的源分组选项（编辑时）- 相同平台；composite 分组可汇总各平台账号，排除自身
4493: const copyAccountsGroupOptionsForEdit = computed(() => {
4494:   const currentId = editingGroup.value?.id;
4495:   const eligibleGroups = groups.value.filter(
4496:     (g) =>
4497:       canCopyAccountsFromGroup(editForm.platform, editForm.domestic_provider, g) &&
4498:       (g.account_count || 0) > 0 &&
4499:       g.id !== currentId,
4500:   );
4501:   return eligibleGroups.map((g) => ({
4502:     value: g.id,
4503:     label: copyAccountsGroupLabel(g),
4504:   }));
4505: });
```

```text
4529: const searchQuery = ref("");
4530: const filters = reactive({
4531:   platform: "",
4532:   status: "",
4533:   is_exclusive: "",
4534: });
4535: const domesticPricingCatalog = ref<DomesticPricingCatalogEntry[]>([]);
4536: const domesticPricingLoading = ref(false);
4537: 
4538: const domesticProviderMeta = (provider?: string | null) =>
4539:   getDomesticProvider(provider);
4540: 
4541: const groupPlatformLabel = (group: Pick<AdminGroup, "platform" | "domestic_provider">) =>
4542:   domesticProviderMeta(group.domestic_provider)?.label ||
4543:   t("admin.groups.platforms." + group.platform);
4544: 
4545: const loadDomesticPricingCatalog = async () => {
4546:   if (domesticPricingLoading.value || domesticPricingCatalog.value.length > 0) return;
4547:   domesticPricingLoading.value = true;
4548:   try {
4549:     domesticPricingCatalog.value = await adminAPI.domesticPricing.getCatalog();
4550:   } catch (error) {
4551:     // The picker deliberately renders "待定价" when the catalog is not
4552:     // available. Never substitute OpenAI prices in the UI.
4553:     console.error("Failed to load domestic pricing catalog:", error);
4554:   } finally {
4555:     domesticPricingLoading.value = false;
4556:   }
4557: };
4558: const pagination = reactive({
4559:   page: 1,
4560:   page_size: getPersistedPageSize(),
4561:   total: 0,
4562:   pages: 0,
4563: });
```

```text
4644: );
4645: 
4646: const createForm = reactive({
4647:   name: "",
4648:   description: "",
4649:   platform: "anthropic" as GroupPlatform,
4650:   domestic_provider: "" as "" | DomesticProviderId,
4651:   rate_multiplier: 1.0,
4652:   is_exclusive: false,
4653:   subscription_type: "standard" as SubscriptionType,
4654:   daily_limit_usd: null as number | null,
4655:   weekly_limit_usd: null as number | null,
4656:   monthly_limit_usd: null as number | null,
4657:   // 图片生成计费配置
4658:   allow_image_generation: false,
```

```text
4994: };
4995: 
4996: const editForm = reactive({
4997:   name: "",
4998:   description: "",
4999:   platform: "anthropic" as GroupPlatform,
5000:   domestic_provider: "" as "" | DomesticProviderId,
5001:   rate_multiplier: 1.0,
5002:   is_exclusive: false,
5003:   status: "active" as "active" | "inactive",
5004:   subscription_type: "standard" as SubscriptionType,
5005:   daily_limit_usd: null as number | null,
5006:   weekly_limit_usd: null as number | null,
5007:   monthly_limit_usd: null as number | null,
5008:   // 图片生成计费配置
```

```text
5057:   reasoning_effort_mappings: [] as ReasoningEffortMappingRow[],
5058: });
5059: 
5060: type GroupPlatformSelection = GroupPlatform | "domestic";
5061: 
5062: const createPlatformSelection = computed<GroupPlatformSelection>({
5063:   get: () => (createForm.domestic_provider ? "domestic" : createForm.platform),
5064:   set: (value) => {
5065:     createForm.copy_accounts_from_group_ids = [];
5066:     if (value === "domestic") {
5067:       createForm.platform = "openai";
5068:       createForm.domestic_provider ||= DOMESTIC_PROVIDERS[0].id;
5069:       void loadDomesticPricingCatalog();
5070:       return;
5071:     }
5072:     createForm.platform = value;
5073:     createForm.domestic_provider = "";
5074:   },
5075: });
5076: 
5077: const editPlatformSelection = computed<GroupPlatformSelection>({
5078:   get: () => (editForm.domestic_provider ? "domestic" : editForm.platform),
5079:   // Platform is immutable in the edit flow; provider classification remains editable.
5080:   set: () => undefined,
5081: });
5082: 
5083: watch(
5084:   () => createForm.domestic_provider,
5085:   (provider, previousProvider) => {
5086:     if (provider !== previousProvider) createForm.copy_accounts_from_group_ids = [];
5087:   },
5088: );
5089: 
5090: watch(
5091:   () => editForm.domestic_provider,
5092:   (provider, previousProvider) => {
5093:     if (provider !== previousProvider) editForm.copy_accounts_from_group_ids = [];
5094:   },
5095: );
5096: 
5097: type ImagePricingFormState = {
5098:   platform: GroupPlatform;
5099:   allow_image_generation: boolean;
```

```text
5481:     accountSearchRunner.clearKey(getCreateRuleSearchKey(rule));
5482:   });
5483:   clearAllAccountSearchState();
5484:   createForm.name = "";
5485:   createForm.description = "";
5486:   createForm.platform = "anthropic";
5487:   createForm.domestic_provider = "";
5488:   createForm.rate_multiplier = 1.0;
5489:   createForm.is_exclusive = false;
5490:   createForm.subscription_type = "standard";
5491:   createForm.daily_limit_usd = null;
5492:   createForm.weekly_limit_usd = null;
5493:   createForm.monthly_limit_usd = null;
5494:   createForm.allow_image_generation = false;
5495:   createForm.allow_batch_image_generation = false;
```

```text
5661: 
5662: const handleEdit = async (group: AdminGroup) => {
5663:   editingGroup.value = group;
5664:   editForm.name = group.name;
5665:   editForm.description = group.description || "";
5666:   editForm.platform = group.platform;
5667:   editForm.domestic_provider = (getDomesticProvider(group.domestic_provider)?.id || "") as "" | DomesticProviderId;
5668:   if (editForm.domestic_provider) void loadDomesticPricingCatalog();
5669:   editForm.rate_multiplier = group.rate_multiplier;
5670:   editForm.is_exclusive = group.is_exclusive;
5671:   editForm.status = group.status;
5672:   editForm.subscription_type = group.subscription_type || "standard";
5673:   editForm.daily_limit_usd = group.daily_limit_usd;
5674:   editForm.weekly_limit_usd = group.weekly_limit_usd;
5675:   editForm.monthly_limit_usd = group.monthly_limit_usd;
5676:   editForm.allow_image_generation = group.allow_image_generation ?? false;
```

```text
5747:   editingGroup.value = null;
5748:   editForm.max_reasoning_effort = "";
5749:   editForm.reasoning_effort_mappings = [];
5750:   editReasoningEffortPolicyRef.value?.resetValidation();
5751:   editModelRoutingRules.value = [];
5752:   editForm.copy_accounts_from_group_ids = [];
5753:   editForm.domestic_provider = "";
5754:   editForm.peak_rate_enabled = false;
5755:   editForm.peak_start = "";
5756:   editForm.peak_end = "";
5757:   editForm.peak_rate_multiplier = 1.0;
5758:   editForm.video_rate_independent = false;
5759:   editForm.video_rate_multiplier = 1;
5760:   editForm.video_price_480p = null;
5761:   editForm.video_price_720p = null;
```

## frontend/src/views/admin/AccountsView.vue

[完整参考文件](../reference/shared/frontend/src/views/admin/AccountsView.vue)

```text
252:             <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
253:           </template>
254:           <template #cell-platform_type="{ row }">
255:             <div class="flex min-w-0 flex-col gap-1">
256:               <div class="flex flex-wrap items-center gap-1">
257:                 <PlatformTypeBadge :platform="row.platform" :type="row.type"
258:                   :domestic-provider="typeof row.extra?.domestic_provider === 'string' ? row.extra.domestic_provider : undefined"
259:                   :auth-mode="getOpenAIAuthMode(row)"
260:                   :plan-type="getAccountPlanType(row)"
261:                   :privacy-mode="row.extra?.privacy_mode || row.parent_privacy_mode"
262:                   :subscription-expires-at="row.credentials?.subscription_expires_at || row.parent_subscription_expires_at" />
263:                 <span
264:                   v-if="getAntigravityTierLabel(row)"
265:                   :class="['inline-block rounded px-1.5 py-0.5 text-[10px] font-medium', getAntigravityTierClass(row)]"
266:                 >
```

## frontend/src/components/common/GroupSelector.vue

[完整参考文件](../reference/shared/frontend/src/components/common/GroupSelector.vue)

```text
37:           @change="handleChange(group.id, ($event.target as HTMLInputElement).checked)"
38:           class="h-3.5 w-3.5 shrink-0 rounded border-gray-300 text-primary-500 focus:ring-primary-500 dark:border-dark-500"
39:         />
40:         <GroupBadge
41:           :name="group.name"
42:           :platform="group.platform"
43:           :domestic-provider="group.domestic_provider"
44:           :subscription-type="group.subscription_type"
45:           :rate-multiplier="group.rate_multiplier"
46:           class="min-w-0 flex-1"
47:         />
48:         <span class="shrink-0 text-xs text-gray-400">{{ group.account_count || 0 }}</span>
49:       </label>
50:       <div
51:         v-if="filteredGroups.length === 0"
```

```text
67: const { t } = useI18n()
68: 
69: interface Props {
70:   modelValue: number[]
71:   groups: AdminGroup[]
72:   platform?: GroupPlatform // Optional platform filter
73:   domesticProvider?: string
74:   mixedScheduling?: boolean // For antigravity accounts: allow anthropic/gemini groups
75:   searchable?: boolean | 'auto'
76: }
77: 
78: const props = withDefaults(defineProps<Props>(), {
79:   searchable: 'auto'
80: })
81: const emit = defineEmits<{
```

```text
101:     } else {
102:       // 默认：只能选择同 platform 的分组；composite 分组可接收任意具体平台账号
103:       result = result.filter((g) => g.platform === props.platform || g.platform === 'composite')
104:     }
105:   }
106:   if (props.platform === 'openai') {
107:     const provider = (props.domesticProvider || '').trim().toLowerCase()
108:     result = result.filter((g) => {
109:       if (g.platform === 'composite') return true
110:       return (g.domestic_provider || '').trim().toLowerCase() === provider
111:     })
112:   }
113:   if (isSearchable.value && searchText.value) {
114:     const q = searchText.value.toLowerCase()
115:     result = result.filter(
116:       (g) => g.name.toLowerCase().includes(q) || g.description?.toLowerCase().includes(q)
117:     )
118:   }
```

## frontend/src/components/common/GroupOptionItem.vue

[完整参考文件](../reference/shared/frontend/src/components/common/GroupOptionItem.vue)

```text
6:       :title="description || undefined"
7:     >
8:       <!-- Row 1: platform badge (name bold) -->
9:       <GroupBadge
10:         :name="name"
11:         :platform="platform"
12:         :domestic-provider="domesticProvider"
13:         :subscription-type="subscriptionType"
14:         :show-rate="false"
15:         class="groupOptionItemBadge"
16:       />
17:       <!-- Row 2: description with top spacing -->
18:       <span
19:         v-if="description"
20:         class="mt-1.5 w-full text-left text-xs leading-relaxed text-gray-500 dark:text-gray-400 line-clamp-2"
```

```text
31:           v-if="rateMultiplier !== undefined"
32:           :class="[
33:             'inline-flex items-center whitespace-nowrap rounded-full px-3 py-1 text-xs font-semibold',
34:             ratePillClass,
35:           ]"
36:         >
37:           <template v-if="hasDomesticUserFactor">
38:             <span class="font-bold">
39:               {{ rateMultiplier }}x × {{ userRateMultiplier }}x =
40:               {{ domesticEffectiveRate }}x
41:             </span>
42:           </template>
43:           <template v-else-if="hasCustomRate">
44:             <span class="mr-1 line-through opacity-50"
45:               >{{ rateMultiplier }}x</span
46:             >
47:             <span class="font-bold">{{ userRateMultiplier }}x</span>
48:           </template>
```

```text
87: 
88: const { t } = useI18n();
89: 
90: interface Props {
91:   name: string;
92:   platform: GroupPlatform;
93:   domesticProvider?: string;
94:   subscriptionType?: SubscriptionType;
95:   rateMultiplier?: number;
96:   userRateMultiplier?: number | null;
97:   peakRateEnabled?: boolean;
98:   peakStart?: string;
99:   peakEnd?: string;
100:   peakRateMultiplier?: number;
101:   description?: string | null;
```

```text
111:   peakRateEnabled: false,
112: });
113: 
114: // Whether user has a custom rate different from default
115: const hasCustomRate = computed(() => {
116:   return (
117:     !props.domesticProvider &&
118:     props.userRateMultiplier !== null &&
119:     props.userRateMultiplier !== undefined &&
120:     props.rateMultiplier !== undefined &&
121:     props.userRateMultiplier !== props.rateMultiplier
122:   );
123: });
124: 
125: const hasDomesticUserFactor = computed(() => {
126:   return Boolean(
127:     props.domesticProvider &&
128:     props.rateMultiplier !== undefined &&
129:     props.userRateMultiplier !== null &&
130:     props.userRateMultiplier !== undefined,
131:   );
132: });
133: 
134: const domesticEffectiveRate = computed(() => {
135:   return Number(
136:     ((props.rateMultiplier ?? 1) * (props.userRateMultiplier ?? 1)).toFixed(6),
137:   );
138: });
139: 
140: const appStore = useAppStore();
141: 
142: const hasPeakRate = computed(() => {
```

```text
158: const peakRateTitle = computed(() => {
159:   return t("common.peakRateTooltip", { window: peakRateText.value });
160: });
161: 
162: // Rate pill color matches platform badge color
163: const ratePillClass = computed(() => {
164:   if (props.domesticProvider) {
165:     return "bg-indigo-50 text-indigo-700 dark:bg-indigo-900/20 dark:text-indigo-300";
166:   }
167:   switch (props.platform) {
168:     case "anthropic":
169:       return "bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-400";
170:     case "openai":
171:       return "bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-400";
172:     case "gemini":
```

## frontend/src/components/common/GroupBadge.vue

[完整参考文件](../reference/shared/frontend/src/components/common/GroupBadge.vue)

```text
4:       'inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium transition-colors',
5:       badgeClass,
6:     ]"
7:   >
8:     <!-- Platform logo -->
9:     <ModelIcon
10:       v-if="domesticProviderMeta?.iconModel"
11:       :model="domesticProviderMeta.iconModel"
12:       size="14px"
13:     />
14:     <Icon v-else-if="domesticProviderMeta" name="globe" size="xs" />
15:     <PlatformIcon v-else-if="platform" :platform="platform" size="sm" />
16:     <!-- Group name -->
17:     <span class="truncate">{{ name }}</span>
18:     <!-- Right side label -->
19:     <span v-if="showLabel" :class="labelClass">
20:       <template v-if="hasDomesticUserFactor">
21:         <span class="font-semibold" :title="domesticRateTitle">
22:           {{ rateMultiplier }}x × {{ userRateMultiplier }}x =
23:           {{ domesticEffectiveRate }}x
24:         </span>
25:       </template>
26:       <template v-else-if="hasCustomRate">
27:         <!-- 原倍率删除线 + 专属倍率高亮 -->
28:         <span class="line-through opacity-50 mr-0.5"
29:           >{{ rateMultiplier }}x</span
30:         >
31:         <span class="font-bold">{{ userRateMultiplier }}x</span>
```

```text
46: import type { SubscriptionType, GroupPlatform } from "@/types";
47: import { useAppStore } from "@/stores/app";
48: import { formatPeakRateWindow, serverTimezoneLabel } from "@/utils/peak-rate";
49: import PlatformIcon from "./PlatformIcon.vue";
50: import ModelIcon from "./ModelIcon.vue";
51: import Icon from "@/components/icons/Icon.vue";
52: import { getDomesticProvider } from "@/utils/domesticProviders";
53: 
54: interface Props {
55:   name: string;
56:   platform?: GroupPlatform;
57:   domesticProvider?: string;
58:   subscriptionType?: SubscriptionType;
59:   rateMultiplier?: number;
60:   userRateMultiplier?: number | null; // 用户专属倍率
61:   peakRateEnabled?: boolean;
62:   peakStart?: string;
63:   peakEnd?: string;
64:   peakRateMultiplier?: number;
65:   showRate?: boolean;
```

```text
83: 
84: const { t } = useI18n();
85: 
86: const isSubscription = computed(
87:   () => props.subscriptionType === "subscription",
88: );
89: const domesticProviderMeta = computed(() =>
90:   getDomesticProvider(props.domesticProvider),
91: );
92: 
93: // Domestic pricing treats the user-specific value as a second multiplicative
94: // factor (base USD × group × user). Keep the legacy replacement display for
95: // non-domestic groups so existing semantics remain unchanged.
96: const hasDomesticUserFactor = computed(() => {
97:   return Boolean(
98:     domesticProviderMeta.value &&
99:     props.rateMultiplier !== undefined &&
100:     props.userRateMultiplier !== null &&
101:     props.userRateMultiplier !== undefined,
102:   );
103: });
104: 
105: const domesticEffectiveRate = computed(() => {
106:   const groupRate = props.rateMultiplier ?? 1;
107:   const userRate = props.userRateMultiplier ?? 1;
108:   return Number((groupRate * userRate).toFixed(6));
109: });
110: 
111: const domesticRateTitle = computed(
112:   () =>
113:     `${domesticProviderMeta.value?.label ?? props.domesticProvider}: ${props.rateMultiplier}x × ${props.userRateMultiplier}x`,
114: );
115: 
116: // 是否有专属倍率（且与默认倍率不同）
117: const hasCustomRate = computed(() => {
118:   return (
119:     !domesticProviderMeta.value &&
120:     props.userRateMultiplier !== null &&
121:     props.userRateMultiplier !== undefined &&
122:     props.rateMultiplier !== undefined &&
123:     props.userRateMultiplier !== props.rateMultiplier
124:   );
125: });
126: 
127: const appStore = useAppStore();
```

```text
181: 
182:   if (!isSubscription.value) {
183:     // Standard: subtle background (不再为专属倍率使用不同的背景色)
184:     return `${base} bg-black/10 dark:bg-white/10`;
185:   }
186: 
187:   if (domesticProviderMeta.value) {
188:     return `${base} bg-indigo-200/60 text-indigo-800 dark:bg-indigo-800/40 dark:text-indigo-300`;
189:   }
190: 
191:   // 订阅类型：根据剩余天数显示不同颜色
192:   if (props.daysRemaining !== null && props.daysRemaining !== undefined) {
193:     if (props.daysRemaining <= 0 || props.daysRemaining <= 3) {
194:       // 已过期或紧急（<=3天）：红色
195:       return `${base} bg-red-200/80 text-red-800 dark:bg-red-800/50 dark:text-red-300`;
```

```text
225: const peakRateClass = computed(() => {
226:   return "px-1.5 py-0.5 rounded text-[10px] font-semibold bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300";
227: });
228: 
229: // Badge color based on platform and subscription type
230: const badgeClass = computed(() => {
231:   if (domesticProviderMeta.value) {
232:     return isSubscription.value
233:       ? "bg-indigo-100 text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-300"
234:       : "bg-indigo-50 text-indigo-700 dark:bg-indigo-900/20 dark:text-indigo-300";
235:   }
236:   if (props.platform === "anthropic") {
237:     // Claude: orange theme
238:     return isSubscription.value
239:       ? "bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400"
```

## frontend/src/components/common/PlatformTypeBadge.vue

[完整参考文件](../reference/shared/frontend/src/components/common/PlatformTypeBadge.vue)

```text
1: <template>
2:   <div class="inline-flex flex-col gap-0.5 text-xs font-medium">
3:     <!-- Row 1: Platform + Type -->
4:     <div class="inline-flex items-center overflow-hidden rounded-md">
5:       <span :class="['inline-flex items-center gap-1 px-2 py-1', platformClass]">
6:         <ModelIcon v-if="domesticProviderMeta?.iconModel" :model="domesticProviderMeta.iconModel" size="12px" />
7:         <Icon v-else-if="domesticProviderMeta" name="globe" size="xs" />
8:         <PlatformIcon v-else :platform="platform" size="xs" />
9:         <span>{{ platformLabel }}</span>
10:       </span>
11:       <span :class="['inline-flex items-center gap-1 px-1.5 py-1', typeClass]">
12:         <!-- OAuth icon -->
13:         <svg
14:           v-if="type === 'oauth'"
15:           class="h-3 w-3"
```

```text
71: import { useI18n } from 'vue-i18n'
72: import type { AccountPlatform, AccountType } from '@/types'
73: import GrokFreeIcon from './GrokFreeIcon.vue'
74: import PlatformIcon from './PlatformIcon.vue'
75: import ModelIcon from './ModelIcon.vue'
76: import Icon from '@/components/icons/Icon.vue'
77: import { getDomesticProvider } from '@/utils/domesticProviders'
78: 
79: const { t } = useI18n()
80: 
81: interface Props {
82:   platform: AccountPlatform
83:   type: AccountType
84:   authMode?: string
85:   planType?: string
86:   privacyMode?: string
87:   subscriptionExpiresAt?: string
88:   domesticProvider?: string
89: }
90: 
91: const props = defineProps<Props>()
92: 
93: const domesticProviderMeta = computed(() => {
94:   return getDomesticProvider(props.domesticProvider)
95: })
96: 
97: const platformLabel = computed(() => {
98:   if (domesticProviderMeta.value) return domesticProviderMeta.value.label
99:   if (props.platform === 'anthropic') return 'Anthropic'
100:   if (props.platform === 'openai') return 'OpenAI'
101:   if (props.platform === 'antigravity') return 'Antigravity'
102:   if (props.platform === 'grok') return 'Grok'
103:   return 'Gemini'
104: })
105: 
106: const normalizedAuthMode = computed(() =>
```

```text
170:     return 'bolt'
171:   }
172:   return null
173: })
174: 
175: const platformClass = computed(() => {
176:   if (domesticProviderMeta.value) {
177:     return 'bg-indigo-100 text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-300'
178:   }
179:   if (props.platform === 'anthropic') {
180:     return 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
181:   }
182:   if (props.platform === 'openai') {
183:     return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
184:   }
```

```text
189:     return 'bg-zinc-100 text-zinc-700 dark:bg-zinc-800 dark:text-zinc-300'
190:   }
191:   return 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400'
192: })
193: 
194: const typeClass = computed(() => {
195:   if (domesticProviderMeta.value) {
196:     return 'bg-indigo-100 text-indigo-600 dark:bg-indigo-900/30 dark:text-indigo-300'
197:   }
198:   if (props.platform === 'anthropic') {
199:     return 'bg-orange-100 text-orange-600 dark:bg-orange-900/30 dark:text-orange-400'
200:   }
201:   if (props.platform === 'openai') {
202:     return 'bg-emerald-100 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-400'
203:   }
```

## frontend/src/types/index.ts

[完整参考文件](../reference/shared/frontend/src/types/index.ts)

```text
519: 
520: export interface Group {
521:   id: number
522:   name: string
523:   description: string | null
524:   platform: GroupPlatform
525:   domestic_provider?: string
526:   rate_multiplier: number
527:   rpm_limit?: number // Group-level RPM cap (0 = unlimited); overrides user-level rpm_limit when set
528:   max_reasoning_effort?: string // OpenAI/Codex reasoning ceiling; empty means unlimited
529:   reasoning_effort_mappings?: ReasoningEffortMapping[]
530:   is_exclusive: boolean
531:   status: 'active' | 'inactive'
532:   subscription_type: SubscriptionType
533:   daily_limit_usd: number | null
```

```text
736: }
737: 
738: export interface CreateGroupRequest {
739:   name: string
740:   description?: string | null
741:   platform?: GroupPlatform
742:   domestic_provider?: string
743:   rate_multiplier?: number
744:   is_exclusive?: boolean
745:   subscription_type?: SubscriptionType
746:   daily_limit_usd?: number | null
747:   weekly_limit_usd?: number | null
748:   monthly_limit_usd?: number | null
749:   allow_image_generation?: boolean
750:   allow_batch_image_generation?: boolean
```

```text
788: }
789: 
790: export interface UpdateGroupRequest {
791:   name?: string
792:   description?: string | null
793:   platform?: GroupPlatform
794:   domestic_provider?: string
795:   rate_multiplier?: number
796:   is_exclusive?: boolean
797:   status?: 'active' | 'inactive'
798:   subscription_type?: SubscriptionType
799:   daily_limit_usd?: number | null
800:   weekly_limit_usd?: number | null
801:   monthly_limit_usd?: number | null
802:   allow_image_generation?: boolean
```

```text
1328:   codex_7d_window_minutes?: number // 7d window in minutes (should be ~10080)
1329: 
1330:   codex_usage_updated_at?: string // Last update timestamp
1331: }
1332: 
1333: export type OpenAICompactMode = 'auto' | 'force_on' | 'force_off'
1334: export type OpenAIResponsesMode = 'auto' | 'force_responses' | 'force_chat_completions'
1335: export type OpenAIEndpointCapability = 'chat_completions' | 'embeddings'
1336: 
1337: export interface OpenAICompactState {
1338:   openai_compact_mode?: OpenAICompactMode
1339:   openai_compact_supported?: boolean
1340:   openai_compact_checked_at?: string
1341:   openai_compact_last_status?: number
1342:   openai_compact_last_error?: string
1343: }
1344: 
1345: export interface OpenAIResponsesState {
1346:   openai_responses_mode?: OpenAIResponsesMode
1347:   openai_responses_supported?: boolean
1348: }
1349: 
1350: export interface CreateAccountRequest {
1351:   name: string
1352:   notes?: string | null
1353:   platform: AccountPlatform
1354:   type: AccountType
```

## frontend/src/api/admin/index.ts

[完整参考文件](../reference/shared/frontend/src/api/admin/index.ts)

```text
31: import channelMonitorTemplateAPI from './channelMonitorTemplate'
32: import adminPaymentAPI from './payment'
33: import affiliatesAPI from './affiliates'
34: import riskControlAPI from './riskControl'
35: import adminComplianceAPI from './compliance'
36: import auditAPI from './audit'
37: import domesticPricingAPI from './domesticPricing'
38: import mediaConfigAPI from './mediaConfig'
39: 
40: /**
41:  * Unified admin API object for convenient access
42:  */
43: export const adminAPI = {
44:   dashboard: dashboardAPI,
45:   users: usersAPI,
```

```text
69:   channelMonitorTemplate: channelMonitorTemplateAPI,
70:   payment: adminPaymentAPI,
71:   affiliates: affiliatesAPI,
72:   riskControl: riskControlAPI,
73:   compliance: adminComplianceAPI,
74:   audit: auditAPI,
75:   domesticPricing: domesticPricingAPI,
76:   mediaConfig: mediaConfigAPI
77: }
78: 
79: export {
80:   dashboardAPI,
81:   usersAPI,
82:   groupsAPI,
83:   accountsAPI,
```

```text
105:   channelMonitorTemplateAPI,
106:   adminPaymentAPI,
107:   affiliatesAPI,
108:   riskControlAPI,
109:   adminComplianceAPI,
110:   auditAPI,
111:   domesticPricingAPI,
112:   mediaConfigAPI
113: }
114: 
115: export default adminAPI
116: 
117: // Re-export types used by components
118: export type { AuditLog, AuditLogQuery, AuditLogListResponse } from './audit'
119: export type { BalanceHistoryItem } from './users'
```

## backend/ent/schema/group.go

[完整参考文件](../reference/shared/backend/ent/schema/group.go)

```text
74: 			Comment("内部幂等恢复标识，不对 API 暴露"),
75: 
76: 		// Subscription-related fields (added by migration 003)
77: 		field.String("platform").
78: 			MaxLen(50).
79: 			Default(domain.PlatformAnthropic),
80: 		field.String("domestic_provider").
81: 			MaxLen(50).
82: 			Default("").
83: 			Comment("国内 OpenAI-compatible 厂商标识；platform 始终保持 openai，空字符串表示普通 OpenAI 分组"),
84: 		field.String("subscription_type").
85: 			MaxLen(20).
86: 			Default(domain.SubscriptionTypeStandard),
87: 		field.Float("daily_limit_usd").
88: 			Optional().
```

## backend/internal/service/group.go

[完整参考文件](../reference/shared/backend/internal/service/group.go)

```text
26: 
27: type Group struct {
28: 	ID          int64
29: 	Name        string
30: 	Description string
31: 	Platform    string
32: 	// DomesticProvider classifies an OpenAI-compatible domestic provider.
33: 	// It never changes the runtime gateway platform, which remains "openai".
34: 	DomesticProvider string
35: 	RateMultiplier   float64
36: 	// 高峰时段倍率：peak_rate_enabled 为 true 且当前时刻处于 [PeakStart, PeakEnd) 时，
37: 	// token 计费倍率额外乘以 PeakRateMultiplier。详见 PeakMultiplierAt。
38: 	PeakRateEnabled    bool
39: 	PeakStart          string
40: 	PeakEnd            string
41: 	PeakRateMultiplier float64
42: 	IsExclusive        bool
```

## backend/internal/handler/dto/types.go

[完整参考文件](../reference/shared/backend/internal/handler/dto/types.go)

```text
91: 
92: type Group struct {
93: 	ID               int64   `json:"id"`
94: 	Name             string  `json:"name"`
95: 	Description      string  `json:"description"`
96: 	Platform         string  `json:"platform"`
97: 	DomesticProvider string  `json:"domestic_provider"`
98: 	RateMultiplier   float64 `json:"rate_multiplier"`
99: 	IsExclusive      bool    `json:"is_exclusive"`
100: 	Status           string  `json:"status"`
101: 
102: 	SubscriptionType string   `json:"subscription_type"`
103: 	DailyLimitUSD    *float64 `json:"daily_limit_usd"`
104: 	WeeklyLimitUSD   *float64 `json:"weekly_limit_usd"`
105: 	MonthlyLimitUSD  *float64 `json:"monthly_limit_usd"`
```

## backend/internal/handler/dto/mappers.go

[完整参考文件](../reference/shared/backend/internal/handler/dto/mappers.go)

```text
173: func groupFromServiceBase(g *service.Group) Group {
174: 	out := Group{
175: 		ID:                              g.ID,
176: 		Name:                            g.Name,
177: 		Description:                     g.Description,
178: 		Platform:                        g.Platform,
179: 		DomesticProvider:                g.DomesticProvider,
180: 		RateMultiplier:                  g.RateMultiplier,
181: 		IsExclusive:                     g.IsExclusive,
182: 		Status:                          g.Status,
183: 		SubscriptionType:                g.SubscriptionType,
184: 		DailyLimitUSD:                   g.DailyLimitUSD,
185: 		WeeklyLimitUSD:                  g.WeeklyLimitUSD,
186: 		MonthlyLimitUSD:                 g.MonthlyLimitUSD,
187: 		AllowImageGeneration:            g.AllowImageGeneration,
```

## backend/internal/handler/admin/group_handler.go

[完整参考文件](../reference/shared/backend/internal/handler/admin/group_handler.go)

```text
97: 
98: // CreateGroupRequest represents create group request
99: type CreateGroupRequest struct {
100: 	Name             string             `json:"name" binding:"required"`
101: 	Description      string             `json:"description"`
102: 	Platform         string             `json:"platform" binding:"omitempty,oneof=anthropic openai gemini antigravity grok media composite"`
103: 	DomesticProvider string             `json:"domestic_provider"`
104: 	RateMultiplier   float64            `json:"rate_multiplier"`
105: 	IsExclusive      bool               `json:"is_exclusive"`
106: 	SubscriptionType string             `json:"subscription_type" binding:"omitempty,oneof=standard subscription"`
107: 	DailyLimitUSD    optionalLimitField `json:"daily_limit_usd"`
108: 	WeeklyLimitUSD   optionalLimitField `json:"weekly_limit_usd"`
109: 	MonthlyLimitUSD  optionalLimitField `json:"monthly_limit_usd"`
110: 	// 图片生成计费配置（antigravity 和 gemini 平台使用，负数表示清除配置）
111: 	AllowImageGeneration            bool                                     `json:"allow_image_generation"`
```

```text
157: 
158: // UpdateGroupRequest represents update group request
159: type UpdateGroupRequest struct {
160: 	Name             string             `json:"name"`
161: 	Description      *string            `json:"description"`
162: 	Platform         string             `json:"platform" binding:"omitempty,oneof=anthropic openai gemini antigravity grok media composite"`
163: 	DomesticProvider *string            `json:"domestic_provider"`
164: 	RateMultiplier   *float64           `json:"rate_multiplier"`
165: 	IsExclusive      *bool              `json:"is_exclusive"`
166: 	Status           string             `json:"status" binding:"omitempty,oneof=active inactive"`
167: 	SubscriptionType string             `json:"subscription_type" binding:"omitempty,oneof=standard subscription"`
168: 	DailyLimitUSD    optionalLimitField `json:"daily_limit_usd"`
169: 	WeeklyLimitUSD   optionalLimitField `json:"weekly_limit_usd"`
170: 	MonthlyLimitUSD  optionalLimitField `json:"monthly_limit_usd"`
171: 	// 图片生成计费配置（antigravity 和 gemini 平台使用，负数表示清除配置）
```

```text
481: 	}
482: 
483: 	group, err := h.adminService.CreateGroup(c.Request.Context(), &service.CreateGroupInput{
484: 		Name:                            req.Name,
485: 		Description:                     req.Description,
486: 		Platform:                        req.Platform,
487: 		DomesticProvider:                req.DomesticProvider,
488: 		RateMultiplier:                  req.RateMultiplier,
489: 		IsExclusive:                     req.IsExclusive,
490: 		SubscriptionType:                req.SubscriptionType,
491: 		DailyLimitUSD:                   req.DailyLimitUSD.ToServiceInput(),
492: 		WeeklyLimitUSD:                  req.WeeklyLimitUSD.ToServiceInput(),
493: 		MonthlyLimitUSD:                 req.MonthlyLimitUSD.ToServiceInput(),
494: 		AllowImageGeneration:            req.AllowImageGeneration,
495: 		AllowBatchImageGeneration:       req.AllowBatchImageGeneration,
```

```text
601: 	}
602: 
603: 	group, err := h.adminService.UpdateGroup(c.Request.Context(), groupID, &service.UpdateGroupInput{
604: 		Name:                            req.Name,
605: 		Description:                     req.Description,
606: 		Platform:                        req.Platform,
607: 		DomesticProvider:                req.DomesticProvider,
608: 		RateMultiplier:                  req.RateMultiplier,
609: 		IsExclusive:                     req.IsExclusive,
610: 		Status:                          req.Status,
611: 		SubscriptionType:                req.SubscriptionType,
612: 		DailyLimitUSD:                   req.DailyLimitUSD.ToServiceInput(),
613: 		WeeklyLimitUSD:                  req.WeeklyLimitUSD.ToServiceInput(),
614: 		MonthlyLimitUSD:                 req.MonthlyLimitUSD.ToServiceInput(),
615: 		AllowImageGeneration:            req.AllowImageGeneration,
```

## backend/internal/service/admin_service.go

[完整参考文件](../reference/shared/backend/internal/service/admin_service.go)

```text
209: }
210: 
211: type CreateGroupInput struct {
212: 	Name             string
213: 	Description      string
214: 	Platform         string
215: 	DomesticProvider string
216: 	RateMultiplier   float64
217: 	IsExclusive      bool
218: 	SubscriptionType string   // standard/subscription
219: 	DailyLimitUSD    *float64 // 日限额 (USD)
220: 	WeeklyLimitUSD   *float64 // 周限额 (USD)
221: 	MonthlyLimitUSD  *float64 // 月限额 (USD)
222: 	// 图片生成计费配置（仅 antigravity 平台使用）
223: 	AllowImageGeneration         bool
```

```text
271: }
272: 
273: type UpdateGroupInput struct {
274: 	Name             string
275: 	Description      *string
276: 	Platform         string
277: 	DomesticProvider *string
278: 	RateMultiplier   *float64 // 使用指针以支持设置为0
279: 	IsExclusive      *bool
280: 	Status           string
281: 	SubscriptionType string   // standard/subscription
282: 	DailyLimitUSD    *float64 // 日限额 (USD)
283: 	WeeklyLimitUSD   *float64 // 周限额 (USD)
284: 	MonthlyLimitUSD  *float64 // 月限额 (USD)
285: 	// 图片生成计费配置（仅 antigravity 平台使用）
```

## backend/internal/service/admin_group.go

[完整参考文件](../reference/shared/backend/internal/service/admin_group.go)

```text
301: 	}
302: 
303: 	platform := input.Platform
304: 	if platform == "" {
305: 		platform = PlatformAnthropic
306: 	}
307: 	domesticProvider, err := NormalizeDomesticProvider(platform, input.DomesticProvider)
308: 	if err != nil {
309: 		return nil, infraerrors.Newf(http.StatusBadRequest, "INVALID_DOMESTIC_PROVIDER", "%v", err)
310: 	}
311: 	maxReasoningEffort, err := normalizeMaxReasoningEffortForPlatform(platform, input.MaxReasoningEffort)
312: 	if err != nil {
313: 		return nil, infraerrors.Newf(http.StatusBadRequest, "INVALID_MAX_REASONING_EFFORT", "%v", err)
314: 	}
315: 	reasoningEffortMappings, err := NormalizeReasoningEffortMappings(platform, input.ReasoningEffortMappings)
```

```text
426: 		for _, srcGroupID := range uniqueSourceGroupIDs {
427: 			srcGroup, err := s.groupRepo.GetByIDLite(ctx, srcGroupID)
428: 			if err != nil {
429: 				return nil, fmt.Errorf("source group %d not found: %w", srcGroupID, err)
430: 			}
431: 			if !canCopyAccountsFromGroupPlatform(platform, srcGroup.Platform) ||
432: 				(platform != PlatformComposite && !isDomesticProviderCompatible(domesticProvider, srcGroup.DomesticProvider)) {
433: 				return nil, fmt.Errorf("source group %d platform mismatch: expected %s, got %s", srcGroupID, platform, srcGroup.Platform)
434: 			}
435: 		}
436: 
437: 		// 获取所有源分组的账号（去重）
438: 		var err error
439: 		accountIDsToCopy, err = s.groupRepo.GetAccountIDsByGroupIDs(ctx, uniqueSourceGroupIDs)
440: 		if err != nil {
```

```text
443: 	}
444: 
445: 	group := &Group{
446: 		Name:                            input.Name,
447: 		Description:                     input.Description,
448: 		Platform:                        platform,
449: 		DomesticProvider:                domesticProvider,
450: 		RateMultiplier:                  input.RateMultiplier,
451: 		IsExclusive:                     input.IsExclusive,
452: 		Status:                          StatusActive,
453: 		SubscriptionType:                subscriptionType,
454: 		DailyLimitUSD:                   dailyLimit,
455: 		WeeklyLimitUSD:                  weeklyLimit,
456: 		MonthlyLimitUSD:                 monthlyLimit,
457: 		AllowImageGeneration:            allowImageGeneration,
```

```text
673: 
674: func (s *adminServiceImpl) UpdateGroup(ctx context.Context, id int64, input *UpdateGroupInput) (*Group, error) {
675: 	group, err := s.groupRepo.GetByID(ctx, id)
676: 	if err != nil {
677: 		return nil, err
678: 	}
679: 	originalDomesticProvider := group.DomesticProvider
680: 
681: 	if input.Name != "" {
682: 		group.Name = input.Name
683: 	}
684: 	if input.Description != nil {
685: 		group.Description = *input.Description
686: 	}
687: 	if input.Platform != "" {
688: 		group.Platform = input.Platform
689: 	}
690: 	if input.DomesticProvider != nil {
691: 		domesticProvider, normalizeErr := NormalizeDomesticProvider(group.Platform, *input.DomesticProvider)
692: 		if normalizeErr != nil {
693: 			return nil, infraerrors.Newf(http.StatusBadRequest, "INVALID_DOMESTIC_PROVIDER", "%v", normalizeErr)
694: 		}
695: 		group.DomesticProvider = domesticProvider
696: 	} else if !isDomesticProviderPlatform(group.Platform) {
697: 		// Moving a domestic group away from supported domestic routing clears the
698: 		// visual classification instead of leaving impossible metadata behind.
699: 		group.DomesticProvider = ""
700: 	}
701: 	if input.RateMultiplier != nil {
702: 		if *input.RateMultiplier <= 0 {
703: 			return nil, errors.New("rate_multiplier must be > 0")
704: 		}
705: 		group.RateMultiplier = *input.RateMultiplier
706: 	}
707: 	if input.IsExclusive != nil {
```

```text
928: 		for _, srcGroupID := range uniqueSourceGroupIDs {
929: 			srcGroup, lookupErr := s.groupRepo.GetByIDLite(ctx, srcGroupID)
930: 			if lookupErr != nil {
931: 				return nil, fmt.Errorf("source group %d not found: %w", srcGroupID, lookupErr)
932: 			}
933: 			if !canCopyAccountsFromGroupPlatform(group.Platform, srcGroup.Platform) ||
934: 				(group.Platform != PlatformComposite && !isDomesticProviderCompatible(group.DomesticProvider, srcGroup.DomesticProvider)) {
935: 				return nil, fmt.Errorf(
936: 					"source group %d platform/provider mismatch: expected %s/%s, got %s/%s",
937: 					srcGroupID,
938: 					group.Platform,
939: 					group.DomesticProvider,
940: 					srcGroup.Platform,
941: 					srcGroup.DomesticProvider,
942: 				)
943: 			}
944: 		}
945: 
946: 		accountIDsToCopy, err = s.groupRepo.GetAccountIDsByGroupIDs(ctx, uniqueSourceGroupIDs)
947: 		if err != nil {
948: 			return nil, fmt.Errorf("failed to get accounts from source groups: %w", err)
949: 		}
```

```text
971: 			accountIDsToCopy = filtered
972: 		}
973: 	}
974: 
975: 	// Existing non-domestic platform transitions keep their legacy behavior.
976: 	// Account/provider validation is required when entering, leaving, or changing
977: 	// an explicit domestic-provider scope (and for every account replacement).
978: 	providerScopeChanged := originalDomesticProvider != group.DomesticProvider &&
979: 		(originalDomesticProvider != "" || group.DomesticProvider != "")
980: 	accountIDsToValidate := accountIDsToCopy
981: 	if providerScopeChanged && !replaceAccountBindings {
982: 		accountIDsToValidate, err = s.groupRepo.GetAccountIDsByGroupIDs(ctx, []int64{id})
983: 		if err != nil {
984: 			return nil, fmt.Errorf("failed to get current group accounts: %w", err)
985: 		}
986: 	}
987: 	if replaceAccountBindings || providerScopeChanged {
```

```text
1031: 				"account %d platform %q does not match group platform %q",
1032: 				account.ID,
1033: 				account.Platform,
1034: 				group.Platform,
1035: 			)
1036: 		}
1037: 		if !isDomesticProviderPlatform(group.Platform) {
1038: 			continue
1039: 		}
1040: 		provider, providerErr := domesticProviderFromAccountExtra(account.Platform, account.Extra)
1041: 		if providerErr != nil {
1042: 			return providerErr
1043: 		}
1044: 		if !isDomesticProviderCompatible(provider, group.DomesticProvider) {
1045: 			return infraerrors.Newf(
1046: 				http.StatusBadRequest,
1047: 				"DOMESTIC_PROVIDER_GROUP_MISMATCH",
1048: 				"account %d domestic_provider %q does not match group domestic_provider %q",
1049: 				account.ID,
1050: 				provider,
1051: 				group.DomesticProvider,
1052: 			)
1053: 		}
1054: 	}
1055: 	return nil
1056: }
1057: 
1058: func (s *adminServiceImpl) DeleteGroup(ctx context.Context, id int64) error {
1059: 	var groupKeys []string
```

## backend/internal/service/admin_group_duplicate.go

[完整参考文件](../reference/shared/backend/internal/service/admin_group_duplicate.go)

```text
80: 
81: func cloneGroupForDuplicate(source *Group, operationID string) *Group {
82: 	return &Group{
83: 		Name:                            duplicateGroupName(source.Name, 1),
84: 		Description:                     source.Description,
85: 		Platform:                        source.Platform,
86: 		DomesticProvider:                source.DomesticProvider,
87: 		RateMultiplier:                  source.RateMultiplier,
88: 		PeakRateEnabled:                 source.PeakRateEnabled,
89: 		PeakStart:                       source.PeakStart,
90: 		PeakEnd:                         source.PeakEnd,
91: 		PeakRateMultiplier:              source.PeakRateMultiplier,
92: 		IsExclusive:                     source.IsExclusive,
93: 		Status:                          duplicateGroupInactiveStatus,
94: 		DuplicateOperationID:            operationID,
```

## backend/internal/service/admin_account.go

[完整参考文件](../reference/shared/backend/internal/service/admin_account.go)

```text
478: 		return nil, err
479: 	}
480: 	accountExtra, err := normalizeOpenAILongContextBillingExtra(input.Platform, input.Extra)
481: 	if err != nil {
482: 		return nil, err
483: 	}
484: 	domesticProvider, err := domesticProviderFromAccountExtra(input.Platform, accountExtra)
485: 	if err != nil {
486: 		return nil, err
487: 	}
488: 
489: 	// 绑定分组
490: 	groupIDs := input.GroupIDs
491: 	// 如果没有指定分组,自动绑定对应平台的默认分组
492: 	if len(groupIDs) == 0 && !input.SkipDefaultGroupBind {
493: 		defaultGroupName := input.Platform + "-default"
494: 		if domesticProvider != "" {
495: 			defaultGroupName = domesticProvider + "-default"
496: 		}
497: 		groups, err := s.groupRepo.ListActiveByPlatform(ctx, input.Platform)
498: 		if err == nil {
499: 			var firstCompatibleID int64
500: 			for _, g := range groups {
501: 				if !isDomesticProviderCompatible(domesticProvider, g.DomesticProvider) {
502: 					continue
503: 				}
504: 				if firstCompatibleID == 0 {
505: 					firstCompatibleID = g.ID
506: 				}
507: 				if strings.EqualFold(g.Name, defaultGroupName) {
508: 					groupIDs = []int64{g.ID}
509: 					break
510: 				}
511: 			}
512: 			if len(groupIDs) == 0 && domesticProvider != "" && firstCompatibleID > 0 {
513: 				groupIDs = []int64{firstCompatibleID}
514: 			}
515: 		}
516: 	}
517: 	if err := s.validateAccountDomesticGroups(ctx, input.Platform, domesticProvider, groupIDs); err != nil {
518: 		return nil, err
519: 	}
520: 	if err := s.validateMediaAccountGroups(ctx, input.Platform, groupIDs); err != nil {
521: 		return nil, err
522: 	}
523: 
524: 	// 检查混合渠道风险（除非用户已确认）
525: 	if len(groupIDs) > 0 && !input.SkipMixedChannelCheck {
```

```text
777: 	}
778: 	if input.AutoPauseOnExpired != nil {
779: 		account.AutoPauseOnExpired = *input.AutoPauseOnExpired
780: 	}
781: 
782: 	// 先验证分组是否存在（在任何写操作之前）。当只修改 extra 中的
783: 	// domestic_provider 时，也必须用账号现有分组重做兼容性校验，避免账号
784: 	// 元数据已经切换厂商、分组仍停留在旧厂商的不可调度状态。
785: 	effectiveGroupIDs := account.GroupIDs
786: 	if input.GroupIDs != nil {
787: 		if err := s.validateGroupIDsExist(ctx, *input.GroupIDs); err != nil {
788: 			return nil, err
789: 		}
790: 		effectiveGroupIDs = *input.GroupIDs
791: 	}
792: 	if input.GroupIDs != nil || input.Extra != nil {
793: 		domesticProvider, providerErr := domesticProviderFromAccountExtra(account.Platform, account.Extra)
794: 		if providerErr != nil {
795: 			return nil, providerErr
796: 		}
797: 		if err := s.validateAccountDomesticGroups(ctx, account.Platform, domesticProvider, effectiveGroupIDs); err != nil {
798: 			return nil, err
799: 		}
800: 		if err := s.validateMediaAccountGroups(ctx, account.Platform, effectiveGroupIDs); err != nil {
801: 			return nil, err
802: 		}
803: 	}
804: 	if input.GroupIDs != nil {
805: 		// 检查混合渠道风险（除非用户已确认）
```

```text
909: 			return nil, err
910: 		}
911: 	}
912: 
913: 	needMixedChannelCheck := input.GroupIDs != nil && !input.SkipMixedChannelCheck
914: 	_, hasLongContextBillingUpdate := input.Extra[openAILongContextBillingEnabledKey]
915: 	_, hasDomesticProviderUpdate := input.Extra["domestic_provider"]
916: 	needDomesticGroupValidation := input.GroupIDs != nil || hasDomesticProviderUpdate
917: 
918: 	// 预取所有目标账号，供凭据守卫/代理守卫/混合渠道检查共用，避免多次 DB 查询。
919: 	var cachedTargets []*Account
920: 	if len(input.Credentials) > 0 || input.ProxyID != nil || needMixedChannelCheck || hasLongContextBillingUpdate || input.ProbeEnabled != nil || needDomesticGroupValidation {
921: 		loaded, err := s.accountRepo.GetByIDs(ctx, input.AccountIDs)
922: 		if err != nil {
923: 			return nil, err
924: 		}
925: 		cachedTargets = loaded
926: 	}
927: 	if input.ProbeEnabled != nil {
928: 		targetsByID := make(map[int64]*Account, len(cachedTargets))
```

```text
949: 			if err := ValidateOpenAILongContextBillingExtra(account.Platform, input.Extra); err != nil {
950: 				return nil, err
951: 			}
952: 			break
953: 		}
954: 	}
955: 	if needDomesticGroupValidation {
956: 		targetsByID := make(map[int64]*Account, len(cachedTargets))
957: 		for _, account := range cachedTargets {
958: 			if account != nil {
959: 				targetsByID[account.ID] = account
960: 			}
961: 		}
962: 		for _, accountID := range input.AccountIDs {
963: 			account, ok := targetsByID[accountID]
```

```text
965: 				return nil, ErrAccountNotFound
966: 			}
967: 			effectiveExtra := make(map[string]any, len(account.Extra)+1)
968: 			for key, value := range account.Extra {
969: 				effectiveExtra[key] = value
970: 			}
971: 			if value, present := input.Extra["domestic_provider"]; present {
972: 				effectiveExtra["domestic_provider"] = value
973: 			}
974: 			provider, err := domesticProviderFromAccountExtra(account.Platform, effectiveExtra)
975: 			if err != nil {
976: 				return nil, err
977: 			}
978: 			effectiveGroupIDs := account.GroupIDs
979: 			if input.GroupIDs != nil {
980: 				effectiveGroupIDs = *input.GroupIDs
981: 			}
982: 			if err := s.validateAccountDomesticGroups(ctx, account.Platform, provider, effectiveGroupIDs); err != nil {
983: 				return nil, err
984: 			}
985: 		}
986: 	}
987: 
988: 	// 影子账号绝不持有凭据:批量更新携带凭据时,目标中不得含影子(外审 G5,与单账号
989: 	// UpdateAccount 守卫对齐)。覆盖显式 IDs 与 filter 解析出的 IDs(此处 AccountIDs 已解析完成)。
990: 	if len(input.Credentials) > 0 {
```

```text
1092: 	}
1093: 	if input.Schedulable != nil {
1094: 		repoUpdates.Schedulable = input.Schedulable
1095: 	}
1096: 
1097: 	// Group replacement and the account row/JSONB mutation are one unit. In
1098: 	// particular, domestic_provider must never be committed separately from its
1099: 	// provider-scoped group bindings.
1100: 	if input.GroupIDs != nil {
1101: 		updater, ok := s.accountRepo.(AccountBulkGroupUpdater)
1102: 		if !ok {
1103: 			return nil, errors.New("atomic bulk account group update is not configured")
1104: 		}
1105: 		if _, err := updater.BulkUpdateWithGroupBindings(ctx, input.AccountIDs, repoUpdates, *input.GroupIDs); err != nil {
1106: 			return nil, err
```

```text
1504: 			return fmt.Errorf("get group: %w", err)
1505: 		}
1506: 	}
1507: 	return nil
1508: }
1509: 
1510: func domesticProviderFromAccountExtra(platform string, extra map[string]any) (string, error) {
1511: 	if extra == nil {
1512: 		return "", nil
1513: 	}
1514: 	raw, ok := extra["domestic_provider"]
1515: 	if !ok || raw == nil {
1516: 		return "", nil
1517: 	}
1518: 	provider, ok := raw.(string)
1519: 	if !ok {
1520: 		return "", infraerrors.BadRequest("INVALID_DOMESTIC_PROVIDER", "domestic_provider must be a string")
1521: 	}
1522: 	if !isDomesticProviderPlatform(platform) {
1523: 		if strings.TrimSpace(provider) != "" {
1524: 			return "", infraerrors.BadRequest("INVALID_DOMESTIC_PROVIDER", "domestic_provider requires openai or anthropic platform")
1525: 		}
1526: 		return "", nil
1527: 	}
1528: 	normalized, err := NormalizeDomesticProvider(platform, provider)
1529: 	if err != nil {
1530: 		return "", infraerrors.BadRequest("INVALID_DOMESTIC_PROVIDER", err.Error())
1531: 	}
1532: 	return normalized, nil
1533: }
1534: 
1535: // validateAccountDomesticGroups keeps display classification and routing
1536: // membership aligned. Composite groups remain valid aggregation targets.
1537: func (s *adminServiceImpl) validateAccountDomesticGroups(ctx context.Context, platform, provider string, groupIDs []int64) error {
1538: 	if !isDomesticProviderPlatform(platform) || len(groupIDs) == 0 {
1539: 		return nil
1540: 	}
1541: 	for _, groupID := range groupIDs {
1542: 		group, err := s.groupRepo.GetByIDLite(ctx, groupID)
1543: 		if err != nil {
1544: 			return fmt.Errorf("get group: %w", err)
1545: 		}
1546: 		if group.Platform == PlatformComposite {
1547: 			continue
1548: 		}
1549: 		if group.Platform != platform || !isDomesticProviderCompatible(provider, group.DomesticProvider) {
1550: 			return infraerrors.Newf(
1551: 				http.StatusBadRequest,
1552: 				"DOMESTIC_PROVIDER_GROUP_MISMATCH",
1553: 				"account domestic_provider %q does not match group %d domestic_provider %q",
1554: 				provider,
1555: 				group.ID,
1556: 				group.DomesticProvider,
1557: 			)
1558: 		}
1559: 	}
1560: 	return nil
1561: }
1562: 
1563: func (s *adminServiceImpl) validateMediaAccountGroups(ctx context.Context, platform string, groupIDs []int64) error {
1564: 	if platform != PlatformMedia || len(groupIDs) == 0 {
```

## backend/internal/service/account_service.go

[完整参考文件](../reference/shared/backend/internal/service/account_service.go)

```text
131: 	// and the scheduler outbox event for the new routing snapshot.
132: 	CreateWithAccountGroups(ctx context.Context, account *Account, groups []AccountGroup) error
133: }
134: 
135: // AccountBulkGroupUpdater atomically applies a bulk account mutation together
136: // with replacing every selected account's group bindings. This prevents JSONB
137: // metadata (notably domestic_provider) from being committed without the
138: // matching routing scope, or vice versa.
139: type AccountBulkGroupUpdater interface {
140: 	BulkUpdateWithGroupBindings(ctx context.Context, ids []int64, updates AccountBulkUpdate, groupIDs []int64) (int64, error)
141: }
142: 
143: // AdminAccountRepository makes the account-duplication write capability an explicit
144: // construction dependency without forcing read-only gateway test doubles to implement it.
145: type AdminAccountRepository interface {
```

## backend/internal/repository/group_repo.go

[完整参考文件](../reference/shared/backend/internal/repository/group_repo.go)

```text
99: 		return errors.New("group is nil")
100: 	}
101: 	builder := client.Group.Create().
102: 		SetName(groupIn.Name).
103: 		SetDescription(groupIn.Description).
104: 		SetPlatform(groupIn.Platform).
105: 		SetDomesticProvider(groupIn.DomesticProvider).
106: 		SetRateMultiplier(groupIn.RateMultiplier).
107: 		SetSortOrder(groupIn.SortOrder).
108: 		SetIsExclusive(groupIn.IsExclusive).
109: 		SetStatus(groupIn.Status).
110: 		SetSubscriptionType(groupIn.SubscriptionType).
111: 		SetNillableDailyLimitUsd(groupIn.DailyLimitUSD).
112: 		SetNillableWeeklyLimitUsd(groupIn.WeeklyLimitUSD).
113: 		SetNillableMonthlyLimitUsd(groupIn.MonthlyLimitUSD).
```

```text
271: 
272: func (r *groupRepository) Update(ctx context.Context, groupIn *service.Group) error {
273: 	builder := r.client.Group.UpdateOneID(groupIn.ID).
274: 		SetName(groupIn.Name).
275: 		SetDescription(groupIn.Description).
276: 		SetPlatform(groupIn.Platform).
277: 		SetDomesticProvider(groupIn.DomesticProvider).
278: 		SetRateMultiplier(groupIn.RateMultiplier).
279: 		SetIsExclusive(groupIn.IsExclusive).
280: 		SetStatus(groupIn.Status).
281: 		SetSubscriptionType(groupIn.SubscriptionType).
282: 		SetNillableDailyLimitUsd(groupIn.DailyLimitUSD).
283: 		SetNillableWeeklyLimitUsd(groupIn.WeeklyLimitUSD).
284: 		SetNillableMonthlyLimitUsd(groupIn.MonthlyLimitUSD).
285: 		SetAllowImageGeneration(groupIn.AllowImageGeneration).
```

```text
475: 		if strings.EqualFold(platform, "domestic") {
476: 			q = q.Where(
477: 				group.Or(
478: 					group.PlatformEQ(service.PlatformOpenAI),
479: 					group.PlatformEQ(service.PlatformAnthropic),
480: 				),
481: 				group.DomesticProviderNEQ(""),
482: 			)
483: 		} else if strings.EqualFold(platform, service.PlatformOpenAI) {
484: 			// The admin UI exposes ordinary OpenAI and domestic OpenAI-compatible
485: 			// groups as disjoint visual platforms. Keep the OpenAI filter from
486: 			// returning the same domestic rows already covered by `domestic`.
487: 			q = q.Where(
488: 				group.PlatformEQ(service.PlatformOpenAI),
489: 				group.DomesticProviderEQ(""),
490: 			)
491: 		} else {
492: 			q = q.Where(group.PlatformEQ(platform))
493: 		}
494: 	}
495: 	if status != "" {
496: 		q = q.Where(group.StatusEQ(status))
497: 	}
498: 	if search != "" {
499: 		q = q.Where(group.Or(
500: 			group.NameContainsFold(search),
501: 			group.DescriptionContainsFold(search),
502: 			group.DomesticProviderContainsFold(search),
503: 		))
504: 	}
505: 	if isExclusive != nil {
506: 		q = q.Where(group.IsExclusiveEQ(*isExclusive))
507: 	}
508: 
509: 	total, err := q.Clone().Count(ctx)
510: 	if err != nil {
```

```text
767: 	if strings.EqualFold(platform, "domestic") {
768: 		q = q.Where(
769: 			group.Or(
770: 				group.PlatformEQ(service.PlatformOpenAI),
771: 				group.PlatformEQ(service.PlatformAnthropic),
772: 			),
773: 			group.DomesticProviderNEQ(""),
774: 		)
775: 	} else {
776: 		q = q.Where(group.PlatformEQ(platform))
777: 	}
778: 	groups, err := q.
779: 		Order(dbent.Asc(group.FieldSortOrder), dbent.Asc(group.FieldID)).
780: 		All(ctx)
781: 	if err != nil {
```

## backend/internal/repository/api_key_repo.go

[完整参考文件](../reference/shared/backend/internal/repository/api_key_repo.go)

```text
172: 		}).
173: 		WithGroup(func(q *dbent.GroupQuery) {
174: 			q.Select(
175: 				group.FieldID,
176: 				group.FieldName,
177: 				group.FieldPlatform,
178: 				group.FieldDomesticProvider,
179: 				group.FieldIsExclusive,
180: 				group.FieldStatus,
181: 				group.FieldSubscriptionType,
182: 				group.FieldRateMultiplier,
183: 				group.FieldDailyLimitUsd,
184: 				group.FieldWeeklyLimitUsd,
185: 				group.FieldMonthlyLimitUsd,
186: 				group.FieldAllowImageGeneration,
```

```text
950: 	}
951: 	return &service.Group{
952: 		ID:                              g.ID,
953: 		Name:                            g.Name,
954: 		Description:                     derefString(g.Description),
955: 		Platform:                        g.Platform,
956: 		DomesticProvider:                g.DomesticProvider,
957: 		RateMultiplier:                  g.RateMultiplier,
958: 		IsExclusive:                     g.IsExclusive,
959: 		Status:                          g.Status,
960: 		Hydrated:                        true,
961: 		DuplicateOperationID:            derefString(g.DuplicateOperationID),
962: 		SubscriptionType:                g.SubscriptionType,
963: 		DailyLimitUSD:                   g.DailyLimitUsd,
964: 		WeeklyLimitUSD:                  g.WeeklyLimitUsd,
```

## backend/internal/service/api_key_auth_cache.go

[完整参考文件](../reference/shared/backend/internal/service/api_key_auth_cache.go)

```text
54: 	// UserGroupRPMOverride 该 API Key 对应的 (user, group) 专属 RPM 覆盖值。
55: 	// nil = 无 override（回退到 group/user 级）；0 = 不限流；>0 = 专属上限。
56: 	UserGroupRPMOverride *int `json:"user_group_rpm_override,omitempty"`
57: }
58: 
59: // v1.0.34: 分组快照直接内嵌完整 *Group（全字段自动跟随），替代手写字段镜像。
60: // 手写镜像先后漏过 media 计费与 domestic_provider（国模计费为 0）字段。
61: // 字段完整性由 api_key_auth_cache_parity_test.go 反射测试守门。
62: 
63: // APIKeyAuthCacheEntry 缓存条目，支持负缓存
64: type APIKeyAuthCacheEntry struct {
65: 	NotFound bool                `json:"not_found"`
66: 	Snapshot *APIKeyAuthSnapshot `json:"snapshot,omitempty"`
67: }
68: 
```

## backend/internal/service/model_pricing_resolver.go

[完整参考文件](../reference/shared/backend/internal/service/model_pricing_resolver.go)

```text
9: 
10: // PricingSource 定价来源标识
11: const (
12: 	PricingSourceChannel         = "channel"
13: 	PricingSourceLiteLLM         = "litellm"
14: 	PricingSourceFallback        = "fallback"
15: 	PricingSourceDomestic        = "domestic_catalog"
16: 	PricingSourceDomesticPending = "domestic_pending_pricing"
17: )
18: 
19: // ResolvedPricing 统一定价解析结果
20: type ResolvedPricing struct {
21: 	// Err is set when a scope-aware resolver deliberately rejects a generic
22: 	// fallback. Callers must propagate it instead of recording a zero price.
23: 	Err error
24: 
```

```text
35: 	RequestTiers []PricingInterval
36: 
37: 	// 按次/图片模式：默认价格（未命中层级时使用）
38: 	DefaultPerRequestPrice float64
39: 
40: 	// 来源标识
41: 	Source string // "channel", "litellm", "fallback", "domestic_catalog"
42: 
43: 	// 是否支持缓存细分
44: 	SupportsCacheBreakdown bool
45: 
46: 	// 渠道定价原始配置（用于区间模式下获取 ImageOutputPrice）
47: 	channelPricing *ChannelModelPricing
48: }
49: 
50: // ModelPricingResolver 统一模型定价解析器。
51: // 解析链：Channel → LiteLLM → Fallback。
52: type ModelPricingResolver struct {
53: 	channelService  *ChannelService
54: 	billingService  *BillingService
55: 	domesticPricing *DomesticPricingService
56: }
57: 
58: // ProvideModelPricingResolver is the dependency-injection constructor. The
59: // two-argument constructor remains available for focused unit tests and older
60: // call sites.
61: func ProvideModelPricingResolver(channelService *ChannelService, billingService *BillingService, domesticPricing *DomesticPricingService) *ModelPricingResolver {
62: 	r := NewModelPricingResolver(channelService, billingService)
63: 	r.domesticPricing = domesticPricing
64: 	return r
65: }
66: 
67: func (r *ModelPricingResolver) SetDomesticPricingService(domesticPricing *DomesticPricingService) {
68: 	if r != nil {
69: 		r.domesticPricing = domesticPricing
70: 	}
71: }
72: 
73: // NewModelPricingResolver 创建定价解析器实例
74: func NewModelPricingResolver(channelService *ChannelService, billingService *BillingService) *ModelPricingResolver {
75: 	return &ModelPricingResolver{
76: 		channelService: channelService,
77: 		billingService: billingService,
```

```text
79: }
80: 
81: // PricingInput 定价解析输入
82: type PricingInput struct {
83: 	Model            string
84: 	GroupID          *int64 // nil 表示不检查渠道
85: 	DomesticProvider string // 非空时必须走 provider-scoped 国内官方数字目录（货币字段为 USD），禁止通用 fallback
86: }
87: 
88: // Resolve 解析模型定价。
89: // 1. 获取基础定价（LiteLLM → Fallback）
90: // 2. 如果指定了 GroupID，查找渠道定价并覆盖
91: func (r *ModelPricingResolver) Resolve(ctx context.Context, input PricingInput) *ResolvedPricing {
92: 	// Domestic groups are a separate billing namespace. Resolve them before
93: 	// channel/LiteLLM/global fallbacks so a coincidentally identical model name,
94: 	// legacy CNY-converted table entry, or free/zero fallback cannot be billed.
95: 	if provider := normalizeDomesticPricingProvider(input.DomesticProvider); provider != "" {
96: 		resolved := &ResolvedPricing{Mode: BillingModeToken, Source: PricingSourceDomesticPending}
97: 		var catalogErr error
98: 		if r == nil || r.domesticPricing == nil {
99: 			catalogErr = fmt.Errorf("%w: provider=%s model=%s", ErrDomesticPricingUnavailable, provider, input.Model)
100: 		} else {
101: 			entry, err := r.domesticPricing.Resolve(ctx, provider, input.Model)
102: 			if err != nil {
103: 				catalogErr = err
104: 			} else if pricing, pricingErr := r.domesticPricing.modelPricing(entry); pricingErr != nil {
105: 				catalogErr = pricingErr
106: 			} else {
107: 				resolved.BasePricing = pricing
108: 				resolved.Source = PricingSourceDomestic
109: 				resolved.SupportsCacheBreakdown = entry.CacheReadPricePerToken != nil || entry.CacheWritePricePerToken != nil
110: 				resolved.BasePricing.SupportsCacheBreakdown = resolved.SupportsCacheBreakdown
111: 			}
112: 		}
113: 
114: 		// Existing channel pricing remains an allowed administrator override, but
115: 		// only for an exact model entry. Wildcard/family matches are too broad for
116: 		// a provider-scoped, fail-closed domestic billing namespace.
```

```text
131: 			switch resolved.Mode {
132: 			case BillingModeToken:
133: 				r.applyTokenOverrides(chPricing, resolved)
134: 			case BillingModePerRequest, BillingModeImage:
135: 				r.applyRequestTierOverrides(chPricing, resolved)
136: 			}
137: 			if err := validateDomesticResolvedPricing(resolved); err != nil {
138: 				resolved.Err = err
139: 			}
140: 			return resolved
141: 		}
142: 		if catalogErr != nil {
143: 			resolved.Err = catalogErr
144: 		}
145: 		return resolved
```

```text
197: 			return true
198: 		}
199: 	}
200: 	return false
201: }
202: 
203: func validateDomesticResolvedPricing(resolved *ResolvedPricing) error {
204: 	if resolved == nil {
205: 		return ErrDomesticPricingInvalid
206: 	}
207: 	switch resolved.Mode {
208: 	case BillingModePerRequest, BillingModeImage:
209: 		// A default is mandatory because size/context tiers may not cover every
210: 		// possible request. A missing tier must never collapse to $0.
211: 		if !isFinitePositive(resolved.DefaultPerRequestPrice) {
212: 			return fmt.Errorf("%w: domestic per-request channel override requires a positive default price", ErrDomesticPricingInvalid)
213: 		}
214: 		for _, tier := range resolved.RequestTiers {
215: 			if tier.PerRequestPrice == nil || !isFinitePositive(*tier.PerRequestPrice) {
216: 				return fmt.Errorf("%w: domestic request tier requires a positive price", ErrDomesticPricingInvalid)
217: 			}
218: 		}
219: 		return nil
220: 	default:
221: 		if resolved.BasePricing == nil ||
222: 			!isFinitePositive(resolved.BasePricing.InputPricePerToken) ||
223: 			!isFinitePositive(resolved.BasePricing.OutputPricePerToken) {
224: 			return fmt.Errorf("%w: domestic token pricing requires positive input and output prices", ErrDomesticPricingInvalid)
225: 		}
226: 		for _, interval := range resolved.Intervals {
227: 			if interval.InputPrice == nil || interval.OutputPrice == nil ||
228: 				!isFinitePositive(*interval.InputPrice) || !isFinitePositive(*interval.OutputPrice) ||
229: 				!isFiniteOptionalPositive(interval.CacheReadPrice) ||
230: 				!isFiniteOptionalPositive(interval.CacheWritePrice) {
231: 				return fmt.Errorf("%w: domestic token interval requires positive input/output prices", ErrDomesticPricingInvalid)
232: 			}
233: 		}
234: 		return nil
235: 	}
236: }
237: 
238: // resolveBasePricing 从 LiteLLM 或 Fallback 获取基础定价
239: func (r *ModelPricingResolver) resolveBasePricing(model string) (*ModelPricing, string) {
```

## backend/internal/service/billing_service.go

[完整参考文件](../reference/shared/backend/internal/service/billing_service.go)

```text
160: 	CacheCreationCost         float64
161: 	CacheReadCost             float64
162: 	TotalCost                 float64
163: 	ActualCost                float64 // 应用倍率后的实际费用
164: 	BillingMode               string  // 计费模式（"token"/"per_request"/"image"），由 CalculateCostUnified 填充
165: 	LongContextBillingApplied bool
166: 	// DomesticPricingError 非空表示国产 provider 命名空间定价解析失败，
167: 	// 计费方必须拒绝 0 费落账并把错误上抛（v1.0.34 fail-closed 统一收口）。
168: 	DomesticPricingError error
169: }
170: 
171: // ErrModelPricingUnavailable indicates that none of the configured pricing
172: // sources can price the requested model.
173: var ErrModelPricingUnavailable = errors.New("pricing not found")
174: 
175: // BillingService 计费服务
176: type BillingService struct {
```

```text
949: 
950: // CostInput 统一计费输入
951: type CostInput struct {
952: 	Ctx                       context.Context
953: 	Model                     string
954: 	GroupID                   *int64 // 用于渠道定价查找
955: 	DomesticProvider          string // 非空时使用 provider-scoped 国际 API USD 目录
956: 	Tokens                    UsageTokens
957: 	RequestCount              int    // 按次计费时使用
958: 	SizeTier                  string // 按次/图片模式的层级标签（"1K","2K","4K","HD" 等）
959: 	RateMultiplier            float64
960: 	ServiceTier               string                // "priority","flex","" 等
961: 	Resolver                  *ModelPricingResolver // 定价解析器
962: 	Resolved                  *ResolvedPricing      // 可选：预解析的定价结果（避免重复 Resolve 调用）
963: 	LongContextBillingEnabled *bool
964: }
965: 
966: // CalculateCostUnified 统一计费入口，支持三种计费模式。
967: // 使用 ModelPricingResolver 解析定价，然后根据 BillingMode 分发计算。
968: func (s *BillingService) CalculateCostUnified(input CostInput) (*CostBreakdown, error) {
969: 	if input.Resolver == nil {
970: 		if strings.TrimSpace(input.DomesticProvider) != "" {
971: 			return nil, fmt.Errorf("%w: provider=%s model=%s", ErrDomesticPricingUnavailable, input.DomesticProvider, input.Model)
972: 		}
973: 		// 无 Resolver，回退到旧路径
974: 		applyLongContextBilling := true
975: 		if input.LongContextBillingEnabled != nil {
976: 			applyLongContextBilling = *input.LongContextBillingEnabled
977: 		}
978: 		return s.calculateCostInternalWithPolicy(
979: 			input.Model,
```

```text
988: 	// 优先使用预解析结果，避免重复 Resolve 调用
989: 	resolved := input.Resolved
990: 	if resolved == nil {
991: 		resolved = input.Resolver.Resolve(input.Ctx, PricingInput{
992: 			Model:            input.Model,
993: 			GroupID:          input.GroupID,
994: 			DomesticProvider: input.DomesticProvider,
995: 		})
996: 	}
997: 	if resolved == nil {
998: 		return nil, fmt.Errorf("no pricing resolution for model %s: %w", input.Model, ErrModelPricingUnavailable)
999: 	}
1000: 	if resolved.Err != nil {
1001: 		return nil, resolved.Err
1002: 	}
```

## backend/internal/service/openai_gateway_usage.go

[完整参考文件](../reference/shared/backend/internal/service/openai_gateway_usage.go)

```text
114: 	if resolver == nil {
115: 		resolver = newUserGroupRateResolver(nil, nil, resolveUserGroupRateCacheTTL(s.cfg), nil, "service.openai_gateway")
116: 	}
117: 	return resolver.Resolve(ctx, userID, groupID, groupDefaultMultiplier)
118: }
119: 
120: func (s *OpenAIGatewayService) resolveDomesticUserGroupRateMultiplier(ctx context.Context, userID, groupID int64, groupDefaultMultiplier float64) float64 {
121: 	if s == nil {
122: 		return groupDefaultMultiplier
123: 	}
124: 	resolver := s.userGroupRateResolver
125: 	if resolver == nil {
126: 		resolver = newUserGroupRateResolver(nil, nil, resolveUserGroupRateCacheTTL(s.cfg), nil, "service.openai_gateway")
127: 	}
128: 	return resolver.ResolveCombined(ctx, userID, groupID, groupDefaultMultiplier)
129: }
130: 
131: // ValidateDomesticModelPricing is a pre-forward guard for OpenAI-compatible
132: // domestic groups. A successful upstream response must never precede discovery
133: // that its model has no provider-scoped price.
134: func (s *OpenAIGatewayService) ValidateDomesticModelPricing(ctx context.Context, apiKey *APIKey, model string) error {
135: 	if apiKey == nil || apiKey.Group == nil || strings.TrimSpace(apiKey.Group.DomesticProvider) == "" {
136: 		return nil
137: 	}
138: 	model = strings.TrimSpace(model)
139: 	if model == "" {
140: 		return fmt.Errorf("%w: empty model", ErrDomesticPricingPending)
141: 	}
142: 	if s == nil || s.resolver == nil {
143: 		return fmt.Errorf("%w: provider=%s model=%s", ErrDomesticPricingUnavailable, apiKey.Group.DomesticProvider, model)
144: 	}
145: 	groupID := apiKey.Group.ID
146: 	resolved := s.resolver.Resolve(ctx, PricingInput{
147: 		Model:            model,
148: 		GroupID:          &groupID,
149: 		DomesticProvider: apiKey.Group.DomesticProvider,
150: 	})
151: 	if resolved == nil {
152: 		return fmt.Errorf("%w: provider=%s model=%s", ErrDomesticPricingUnavailable, apiKey.Group.DomesticProvider, model)
153: 	}
154: 	if resolved.Err != nil {
155: 		return resolved.Err
156: 	}
157: 	return validateDomesticResolvedPricing(resolved)
158: }
159: 
160: // ValidateDomesticModelPricingForAccount performs the authoritative
161: // pre-forward check after account selection and after account model_mapping
162: // (plus messages/compact mapping where applicable) has resolved the actual
163: // upstream model identity.
164: func (s *OpenAIGatewayService) ValidateDomesticModelPricingForAccount(
165: 	ctx context.Context,
166: 	apiKey *APIKey,
167: 	account *Account,
168: 	requestedModel string,
169: 	messagesDispatchMappedModel string,
170: 	compact bool,
171: ) error {
172: 	finalModel := ResolveOpenAIFinalMappedModel(account, requestedModel, messagesDispatchMappedModel, compact)
173: 	return s.ValidateDomesticModelPricing(ctx, apiKey, finalModel)
174: }
175: 
176: // RecordUsage records usage and deducts balance
177: func (s *OpenAIGatewayService) RecordUsage(ctx context.Context, input *OpenAIRecordUsageInput) error {
178: 	if input == nil {
179: 		return errors.New("openai usage input is nil")
180: 	}
181: 	result := input.Result
```

```text
214: 	// Get rate multiplier
215: 	multiplier := 1.0
216: 	if s.cfg != nil {
217: 		multiplier = s.cfg.Default.RateMultiplier
218: 	}
219: 	if apiKey.GroupID != nil && apiKey.Group != nil {
220: 		if strings.TrimSpace(apiKey.Group.DomesticProvider) != "" {
221: 			multiplier = s.resolveDomesticUserGroupRateMultiplier(ctx, user.ID, *apiKey.GroupID, apiKey.Group.RateMultiplier)
222: 		} else {
223: 			multiplier = s.ResolveUserGroupRateMultiplier(ctx, user.ID, *apiKey.GroupID, apiKey.Group.RateMultiplier)
224: 		}
225: 	}
226: 	// token 倍率叠加高峰因子（token 计费含图片 token，图片按次倍率不受影响）。高峰因子按请求时刻现算，
227: 	// 不并入上面的 Resolve，以免污染 user:group 倍率缓存。
228: 	baseMultiplier := multiplier
229: 	multiplier, imageMultiplier := computePeakAwareMultipliers(apiKey, baseMultiplier, timezone.Now())
```

```text
272: 		baseMultiplier,
273: 		tokens,
274: 		serviceTier,
275: 		longContextBillingEnabled,
276: 	)
277: 	if err != nil {
278: 		// Domestic groups are fail-closed even when an error also resembles the
279: 		// legacy "pricing unavailable" condition. Recording a zero-cost success
280: 		// would turn a provider-scoped catalogue outage into free upstream usage.
281: 		if apiKey != nil && apiKey.Group != nil && strings.TrimSpace(apiKey.Group.DomesticProvider) != "" {
282: 			return err
283: 		}
284: 		if !isUsagePricingUnavailableError(err) {
285: 			return err
286: 		}
287: 		logger.L().With(
288: 			zap.String("component", "service.openai_gateway"),
289: 			zap.Strings("billing_models", billingModels),
```

```text
477: 	webSearchMultiplier float64,
478: 	tokens UsageTokens,
479: 	serviceTier string,
480: 	longContextBillingEnabled bool,
481: ) (*CostBreakdown, error) {
482: 	billingModel := firstUsageBillingModel(billingModels)
483: 	var domesticResolved *ResolvedPricing
484: 	if apiKey != nil && apiKey.Group != nil && strings.TrimSpace(apiKey.Group.DomesticProvider) != "" {
485: 		if billingModel == "" {
486: 			return nil, fmt.Errorf("%w: empty domestic billing model", ErrDomesticPricingPending)
487: 		}
488: 		domesticResolved = s.resolveOpenAIChannelPricing(ctx, billingModel, apiKey)
489: 		if domesticResolved == nil {
490: 			return nil, fmt.Errorf("%w: provider=%s model=%s", ErrDomesticPricingUnavailable, apiKey.Group.DomesticProvider, billingModel)
491: 		}
492: 		if domesticResolved.Err != nil {
493: 			return nil, domesticResolved.Err
494: 		}
495: 		if err := validateDomesticResolvedPricing(domesticResolved); err != nil {
496: 			return nil, err
497: 		}
498: 	}
499: 	if result != nil && result.WebSearchCalls > 0 {
500: 		// Codex alpha/search 网页搜索按次计费：上游不返回 usage/token 字段，单价只取
501: 		// 分组覆盖价（nil 时默认 0.01 = 官方 $10/1000 次），不参与渠道级模型定价。
502: 		// 倍率与 image/video 按次口径一致：使用不含高峰因子的基础倍率
503: 		//（用户专属 > 分组 rate_multiplier > 系统默认），与分组表单的价格预览承诺一致。
504: 		return s.billingService.CalculateWebSearchCost(result.WebSearchCalls, webSearchPricePerCallFromAPIKey(apiKey), webSearchMultiplier), nil
505: 	}
506: 	if isGrokVideoUsageResult(result, billingModels) {
507: 		resolved := domesticResolved
508: 		if resolved == nil {
509: 			resolved = s.resolveOpenAIChannelPricing(ctx, billingModel, apiKey)
510: 		}
511: 		if resolved == nil || resolved.Mode != BillingModeToken {
512: 			return s.calculateOpenAIVideoCost(ctx, billingModel, apiKey, result, videoMultiplier)
513: 		}
514: 	}
515: 	if result != nil && result.ImageCount > 0 {
516: 		// 渠道定价为 token 计费时走 token 路径，否则走图片计费
517: 		resolved := domesticResolved
518: 		if resolved == nil {
519: 			resolved = s.resolveOpenAIChannelPricing(ctx, billingModel, apiKey)
520: 		}
521: 		if resolved == nil || resolved.Mode != BillingModeToken {
522: 			return s.calculateOpenAIImageCost(ctx, billingModel, apiKey, result, imageMultiplier)
523: 		}
524: 	}
525: 	if len(billingModels) == 0 || billingModel == "" {
```

```text
592: 	if s.resolver != nil && apiKey.Group != nil {
593: 		gid := apiKey.Group.ID
594: 		return s.billingService.CalculateCostUnified(CostInput{
595: 			Ctx:                       ctx,
596: 			Model:                     billingModel,
597: 			GroupID:                   &gid,
598: 			DomesticProvider:          apiKey.Group.DomesticProvider,
599: 			Tokens:                    tokens,
600: 			RequestCount:              1,
601: 			RateMultiplier:            multiplier,
602: 			ServiceTier:               serviceTier,
603: 			Resolver:                  s.resolver,
604: 			LongContextBillingEnabled: &longContextBillingEnabled,
605: 		})
606: 	}
```

```text
646: 			Resolver:       s.resolver,
647: 			Resolved:       resolved,
648: 		})
649: 		if err == nil {
650: 			return cost, nil
651: 		}
652: 		if apiKey != nil && apiKey.Group != nil && strings.TrimSpace(apiKey.Group.DomesticProvider) != "" {
653: 			return nil, err
654: 		}
655: 		logger.LegacyPrintf("service.openai_gateway", "Calculate image channel cost failed: %v", err)
656: 	}
657: 	if apiKey != nil && apiKey.Group != nil && strings.TrimSpace(apiKey.Group.DomesticProvider) != "" {
658: 		if resolved != nil && resolved.Err != nil {
659: 			return nil, resolved.Err
660: 		}
661: 		return nil, fmt.Errorf("%w: provider=%s model=%s", ErrDomesticPricingPending, apiKey.Group.DomesticProvider, billingModel)
662: 	}
663: 
664: 	return s.billingService.CalculateImageCost(billingModel, sizeTier, result.ImageCount, groupConfig, multiplier), nil
665: }
666: 
667: func (s *OpenAIGatewayService) calculateOpenAIVideoCost(
668: 	ctx context.Context,
669: 	billingModel string,
```

```text
704: 			Resolved:       resolved,
705: 		})
706: 		if err == nil {
707: 			cost.BillingMode = string(BillingModeVideo)
708: 			return cost, nil
709: 		}
710: 		if apiKey != nil && apiKey.Group != nil && strings.TrimSpace(apiKey.Group.DomesticProvider) != "" {
711: 			return nil, err
712: 		}
713: 		logger.LegacyPrintf("service.openai_gateway", "Calculate video channel cost failed: %v", err)
714: 	}
715: 	if apiKey != nil && apiKey.Group != nil && strings.TrimSpace(apiKey.Group.DomesticProvider) != "" {
716: 		if resolved != nil && resolved.Err != nil {
717: 			return nil, resolved.Err
718: 		}
719: 		return nil, fmt.Errorf("%w: provider=%s model=%s", ErrDomesticPricingPending, apiKey.Group.DomesticProvider, billingModel)
720: 	}
721: 
722: 	return s.billingService.CalculateVideoCost(billingModel, resolution, videoCount, durationSeconds, groupConfig, multiplier), nil
723: }
724: 
725: func (s *OpenAIGatewayService) apiKeyWithFreshGroupMediaPricing(ctx context.Context, apiKey *APIKey) *APIKey {
726: 	if apiKey == nil || apiKey.GroupID == nil || *apiKey.GroupID <= 0 {
727: 		return apiKey
```

```text
764: 		return nil
765: 	}
766: 	gid := apiKey.Group.ID
767: 	resolved := s.resolver.Resolve(ctx, PricingInput{
768: 		Model:            billingModel,
769: 		GroupID:          &gid,
770: 		DomesticProvider: apiKey.Group.DomesticProvider,
771: 	})
772: 	if strings.TrimSpace(apiKey.Group.DomesticProvider) != "" {
773: 		return resolved
774: 	}
775: 	if resolved == nil {
776: 		return nil
777: 	}
778: 	if resolved.Source == PricingSourceChannel {
779: 		return resolved
780: 	}
```

## backend/internal/handler/openai_gateway_handler.go

[完整参考文件](../reference/shared/backend/internal/handler/openai_gateway_handler.go)

```text
150: }
151: 
152: func openAICompatibleTextTargetAllowed(c *gin.Context, apiKey *service.APIKey, model string) bool {
153: 	return compositeTargetPlatformAllowed(c, apiKey, model, service.PlatformOpenAI, service.PlatformGrok)
154: }
155: 
156: func domesticPricingModel(requestedModel string, mapping service.ChannelMappingResult) string {
157: 	if mapping.Mapped {
158: 		if mapped := strings.TrimSpace(mapping.MappedModel); mapped != "" {
159: 			return mapped
160: 		}
161: 	}
162: 	return strings.TrimSpace(requestedModel)
163: }
164: 
165: func domesticPricingWSRequestModel(originalModel string, payload []byte, fallbackModel string) string {
166: 	if model := strings.TrimSpace(originalModel); model != "" {
167: 		return model
168: 	}
169: 	if model := strings.TrimSpace(gjson.GetBytes(payload, "model").String()); model != "" {
170: 		return model
171: 	}
172: 	return strings.TrimSpace(fallbackModel)
173: }
174: 
175: func (h *OpenAIGatewayHandler) rejectDomesticPricingForAccount(
176: 	c *gin.Context,
177: 	apiKey *service.APIKey,
178: 	account *service.Account,
179: 	requestedModel string,
180: 	messagesDispatchMappedModel string,
181: 	compact bool,
182: 	anthropic bool,
183: 	streamStarted bool,
184: ) bool {
185: 	if h == nil || h.gatewayService == nil {
186: 		return false
187: 	}
188: 	if err := h.gatewayService.ValidateDomesticModelPricingForAccount(
189: 		c.Request.Context(),
190: 		apiKey,
191: 		account,
192: 		requestedModel,
193: 		messagesDispatchMappedModel,
194: 		compact,
195: 	); err != nil {
196: 		const message = "Model pricing is pending administrator configuration for this provider/model"
```

```text
525: 		setOpsSelectedAccount(c, account.ID, account.Platform)
526: 
527: 		accountReleaseFunc, acquired := h.acquireResponsesAccountSlot(c, apiKey.GroupID, sessionHash, selection, reqStream, &streamStarted, reqLog)
528: 		if !acquired {
529: 			return
530: 		}
531: 		if h.rejectDomesticPricingForAccount(
532: 			c,
533: 			apiKey,
534: 			account,
535: 			domesticPricingModel(reqModel, channelMapping),
536: 			"",
537: 			requireCompact,
538: 			false,
539: 			streamStarted,
540: 		) {
541: 			if accountReleaseFunc != nil {
542: 				accountReleaseFunc()
543: 			}
```

```text
1079: 
1080: 		accountReleaseFunc, acquired := h.acquireResponsesAccountSlot(c, apiKey.GroupID, sessionHash, selection, reqStream, &streamStarted, reqLog)
1081: 		if !acquired {
1082: 			return
1083: 		}
1084: 		defaultMappedModel := strings.TrimSpace(effectiveMappedModel)
1085: 		if h.rejectDomesticPricingForAccount(
1086: 			c,
1087: 			apiKey,
1088: 			account,
1089: 			domesticPricingModel(reqModel, channelMappingMsg),
1090: 			defaultMappedModel,
1091: 			false,
1092: 			true,
1093: 			streamStarted,
1094: 		) {
1095: 			if accountReleaseFunc != nil {
1096: 				accountReleaseFunc()
1097: 			}
```

```text
1816: 				closeOpenAIClientWS(wsConn, coderws.StatusTryAgainLater, "account is busy, please retry later")
1817: 				return
1818: 			}
1819: 			accountReleaseFunc = fastReleaseFunc
1820: 		}
1821: 		currentAccountRelease = wrapReleaseOnDone(ctx, accountReleaseFunc)
1822: 		if err := h.gatewayService.ValidateDomesticModelPricingForAccount(
1823: 			ctx,
1824: 			apiKey,
1825: 			account,
1826: 			domesticPricingModel(reqModel, channelMappingWS),
1827: 			"",
1828: 			false,
1829: 		); err != nil {
1830: 			writeOpenAIWSError(ctx, wsConn, "pricing_not_configured", "Model pricing is pending administrator configuration for this provider/model")
1831: 			closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, "model pricing pending administrator configuration")
1832: 			return
1833: 		}
1834: 		if err := h.gatewayService.BindStickySession(ctx, apiKey.GroupID, sessionHash, account.ID); err != nil {
```

```text
1874: 				if turn == 1 {
1875: 					return nil
1876: 				}
1877: 				if !gjson.ValidBytes(payload) {
1878: 					return service.NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request payload", errors.New("invalid json"))
1879: 				}
1880: 				model := domesticPricingWSRequestModel(originalModel, payload, reqModel)
1881: 				if err := h.gatewayService.ValidateDomesticModelPricingForAccount(ctx, apiKey, account, model, "", false); err != nil {
1882: 					writeOpenAIWSError(ctx, wsConn, "pricing_not_configured", "Model pricing is pending administrator configuration for this provider/model")
1883: 					return service.NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "model pricing pending administrator configuration", err)
1884: 				}
1885: 				if decision := h.checkSecurityAuditStage(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIResponses, model, payload, "subsequent_turn"); decision != nil && !decision.AllowNextStage {
1886: 					writeSecurityAuditWSError(ctx, wsConn, decision)
1887: 					return service.NewOpenAIWSClientCloseError(securityAuditWSCloseStatus(decision), securityAuditWSCloseReason(decision), nil)
1888: 				}
1889: 				return nil
```

## backend/internal/handler/openai_chat_completions.go

[完整参考文件](../reference/shared/backend/internal/handler/openai_chat_completions.go)

```text
208: 		setOpsSelectedAccount(c, account.ID, account.Platform)
209: 
210: 		accountReleaseFunc, acquired := h.acquireResponsesAccountSlot(c, apiKey.GroupID, sessionHash, selection, reqStream, &streamStarted, reqLog)
211: 		if !acquired {
212: 			return
213: 		}
214: 		if h.rejectDomesticPricingForAccount(
215: 			c,
216: 			apiKey,
217: 			account,
218: 			domesticPricingModel(reqModel, channelMapping),
219: 			"",
220: 			false,
221: 			false,
222: 			streamStarted,
223: 		) {
224: 			if accountReleaseFunc != nil {
225: 				accountReleaseFunc()
226: 			}
```

## backend/internal/handler/openai_embeddings.go

[完整参考文件](../reference/shared/backend/internal/handler/openai_embeddings.go)

```text
166: 		setOpsSelectedAccount(c, account.ID, account.Platform)
167: 
168: 		accountReleaseFunc, accountAcquired := h.acquireResponsesAccountSlot(c, apiKey.GroupID, "", selection, false, &streamStarted, reqLog)
169: 		if !accountAcquired {
170: 			return
171: 		}
172: 		if h.rejectDomesticPricingForAccount(
173: 			c,
174: 			apiKey,
175: 			account,
176: 			domesticPricingModel(reqModel, channelMapping),
177: 			"",
178: 			false,
179: 			false,
180: 			streamStarted,
181: 		) {
182: 			if accountReleaseFunc != nil {
183: 				accountReleaseFunc()
184: 			}
```

## backend/internal/handler/openai_alpha_search.go

[完整参考文件](../reference/shared/backend/internal/handler/openai_alpha_search.go)

```text
152: 		account := selection.Account
153: 		setOpsSelectedAccount(c, account.ID, account.Platform)
154: 		accountRelease, acquired := h.acquireResponsesAccountSlot(c, apiKey.GroupID, sessionHash, selection, false, &streamStarted, reqLog)
155: 		if !acquired {
156: 			return
157: 		}
158: 		if h.rejectDomesticPricingForAccount(
159: 			c,
160: 			apiKey,
161: 			account,
162: 			domesticPricingModel(requestedModel, channelMapping),
163: 			"",
164: 			false,
165: 			false,
166: 			streamStarted,
167: 		) {
168: 			if accountRelease != nil {
169: 				accountRelease()
170: 			}
```

## backend/internal/handler/openai_live.go

[完整参考文件](../reference/shared/backend/internal/handler/openai_live.go)

```text
168: 		InboundEndpoint: GetInboundEndpoint(c),
169: 	}
170: }
171: 
172: func (h *OpenAIGatewayHandler) writeLiveCreateError(c *gin.Context, err error) {
173: 	switch {
174: 	case errors.Is(err, service.ErrDomesticPricingPending),
175: 		errors.Is(err, service.ErrDomesticPricingUnavailable),
176: 		errors.Is(err, service.ErrDomesticPricingInvalid):
177: 		h.errorResponse(c, http.StatusServiceUnavailable, "pricing_not_configured", "Model pricing is pending administrator configuration for this provider/model")
178: 	case errors.Is(err, service.ErrLiveConcurrencyFull):
179: 		h.errorResponse(c, http.StatusTooManyRequests, "rate_limit_error", "Live concurrency limit reached")
180: 	case errors.Is(err, service.ErrLiveUnavailable):
181: 		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Live is unavailable")
182: 	default:
183: 		var attestationErr *service.LiveAttestationUnavailableError
184: 		if errors.As(err, &attestationErr) {
```

## backend/internal/service/gateway_usage_billing.go

[完整参考文件](../reference/shared/backend/internal/service/gateway_usage_billing.go)

```text
874: 	}
875: 
876: 	// 计算费用
877: 	cost := s.calculateRecordUsageCost(ctx, result, apiKey, account, billingModel, multiplier, imageMultiplier, opts)
878: 	// v1.0.34: domestic 命名空间 fail-closed——定价解析失败时拒绝 0 费落账，
879: 	// 错误原样上抛（对齐 OpenAI 网关行为），由上层记录并告警。
880: 	if cost != nil && cost.DomesticPricingError != nil {
881: 		return nil, cost.DomesticPricingError
882: 	}
883: 
884: 	// 判断计费方式：订阅模式 vs 余额模式
885: 	isSubscriptionBilling := subscription != nil && apiKey.Group != nil && apiKey.Group.IsSubscriptionType()
886: 	billingType := BillingTypeBalance
887: 	if isSubscriptionBilling {
888: 		billingType = BillingTypeSubscription
889: 	}
```

```text
1089: 	resolved := s.resolveChannelPricing(ctx, model, apiKey)
1090: 	if resolved != nil && resolved.Err == nil {
1091: 		return true
1092: 	}
1093: 	// domestic 命名空间禁止通用 LiteLLM/fallback 兜底：查不到就是查不到，
1094: 	// 不能拿同名国外模型的价格顶上（provider-scoped fail-closed 原则）。
1095: 	if isDomesticAPIKey(apiKey) {
1096: 		return false
1097: 	}
1098: 	if s.billingService == nil {
1099: 		return false
1100: 	}
1101: 	_, err := s.billingService.GetModelPricing(model)
1102: 	return err == nil
1103: }
1104: 
1105: // resolveChannelPricing 检查指定模型是否存在渠道级别定价。
1106: // 返回非 nil 的 ResolvedPricing 表示有渠道定价，nil 表示走默认定价路径。
1107: // v1.0.34: domestic 域解析结果（含 Err）原样透传给调用方，由统一的
1108: // failDomesticPricing 收口处理——此前 Err 被丢弃导致落通用目录后记 0 费。
1109: func (s *GatewayService) resolveChannelPricing(ctx context.Context, billingModel string, apiKey *APIKey) *ResolvedPricing {
1110: 	if s.resolver == nil || apiKey == nil || apiKey.Group == nil {
1111: 		return nil
1112: 	}
1113: 	gid := apiKey.Group.ID
1114: 	resolved := s.resolver.Resolve(ctx, PricingInput{
1115: 		Model:            billingModel,
1116: 		GroupID:          &gid,
1117: 		DomesticProvider: apiKey.Group.DomesticProvider,
1118: 	})
1119: 	if strings.TrimSpace(apiKey.Group.DomesticProvider) != "" {
1120: 		return resolved // domestic 域：pending/Err 也返回，交给 fail-closed 判定
1121: 	}
1122: 	if resolved.Source == PricingSourceChannel {
1123: 		return resolved
1124: 	}
1125: 	return nil
1126: }
1127: 
1128: // isDomesticAPIKey 报告该 key 所属分组是否为国产 provider 计费命名空间。
1129: func isDomesticAPIKey(apiKey *APIKey) bool {
1130: 	return apiKey != nil && apiKey.Group != nil && strings.TrimSpace(apiKey.Group.DomesticProvider) != ""
1131: }
1132: 
1133: // failDomesticPricing 是 Claude 网关 domestic 计费的统一收口：
1134: // domestic 命名空间内任何定价解析失败都必须失败，绝不允许静默 0 费落账
1135: // （对齐 OpenAI 网关 openai_gateway_usage.go RecordUsage 的 fail-closed 语义）。
1136: func failDomesticPricing(apiKey *APIKey, billingModel string, resolved *ResolvedPricing) error {
1137: 	if !isDomesticAPIKey(apiKey) {
1138: 		return nil
1139: 	}
1140: 	if resolved == nil {
1141: 		return fmt.Errorf("%w: provider=%s model=%s", ErrDomesticPricingUnavailable, apiKey.Group.DomesticProvider, billingModel)
1142: 	}
1143: 	if resolved.Err != nil {
1144: 		return resolved.Err
1145: 	}
1146: 	return nil
1147: }
1148: 
1149: // calculateImageCost 计算图片生成费用：渠道级别定价优先，否则走按次计费。
```

```text
1192: 			Resolver:       s.resolver,
1193: 			Resolved:       resolved,
1194: 		})
1195: 		if err != nil {
1196: 			logger.LegacyPrintf("service.gateway", "Calculate image token cost failed: %v", err)
1197: 			failed := &CostBreakdown{ActualCost: 0}
1198: 			if isDomesticAPIKey(apiKey) {
1199: 				failed.DomesticPricingError = err
1200: 			}
1201: 			return failed
1202: 		}
1203: 		return cost
1204: 	}
1205: 
1206: 	final := s.billingService.CalculateImageCost(billingModel, sizeTier, result.ImageCount, groupConfig, multiplier)
1207: 	// domestic 命名空间：无渠道定价的图片计费同样禁止 0 价兜底。
1208: 	if isDomesticAPIKey(apiKey) {
1209: 		failed := &CostBreakdown{ActualCost: 0}
1210: 		failed.DomesticPricingError = fmt.Errorf("%w: provider=%s model=%s image pricing unresolved",
1211: 			ErrDomesticPricingPending, apiKey.Group.DomesticProvider, billingModel)
1212: 		return failed
1213: 	}
1214: 	return final
1215: }
1216: 
1217: // calculateTokenCost 计算 Token 计费：根据 opts 决定走普通/长上下文/渠道统一计费。
1218: func (s *GatewayService) calculateTokenCost(
1219: 	ctx context.Context,
```

```text
1234: 	}
1235: 
1236: 	var cost *CostBreakdown
1237: 	var err error
1238: 
1239: 	// 优先尝试渠道定价 → CalculateCostUnified
1240: 	if resolved := s.resolveChannelPricing(ctx, billingModel, apiKey); resolved != nil || isDomesticAPIKey(apiKey) {
1241: 		// v1.0.34: domestic 命名空间 fail-closed——解析失败必须报错，
1242: 		// 禁止回落通用目录（同名模型错价）或记 0 费。
1243: 		if err := failDomesticPricing(apiKey, billingModel, resolved); err != nil {
1244: 			logger.LegacyPrintf("service.gateway", "[Billing] domestic pricing unresolved, refusing zero-cost record: model=%s provider=%s err=%v",
1245: 				billingModel, apiKey.Group.DomesticProvider, err)
1246: 			return &CostBreakdown{ActualCost: 0, DomesticPricingError: err}
1247: 		}
1248: 		gid := apiKey.Group.ID
1249: 		cost, err = s.billingService.CalculateCostUnified(CostInput{
1250: 			Ctx:            ctx,
1251: 			Model:          billingModel,
1252: 			GroupID:        &gid,
1253: 			Tokens:         tokens,
1254: 			RequestCount:   1,
```

```text
1263: 		cost, err = s.billingService.CalculateCost(billingModel, tokens, multiplier)
1264: 	}
1265: 	if err != nil {
1266: 		logger.LegacyPrintf("service.gateway", "Calculate cost failed: %v", err)
1267: 		failed := &CostBreakdown{ActualCost: 0}
1268: 		// domestic 命名空间：计费失败标记错误，由 recordUsageCore 拒绝落账。
1269: 		if isDomesticAPIKey(apiKey) {
1270: 			failed.DomesticPricingError = err
1271: 		}
1272: 		return failed
1273: 	}
1274: 	return cost
1275: }
1276: 
1277: // buildRecordUsageLog 构建使用日志并设置计费模式。
1278: func (s *GatewayService) buildRecordUsageLog(
```

## backend/internal/repository/wire.go

[完整参考文件](../reference/shared/backend/internal/repository/wire.go)

```text
89: 	NewOpsRepository,
90: 	NewAuditLogRepository,
91: 	NewUserSubscriptionRepository,
92: 	NewUserAttributeDefinitionRepository,
93: 	NewUserAttributeValueRepository,
94: 	NewUserGroupRateRepository,
95: 	NewDomesticPricingRepository,
96: 	NewErrorPassthroughRepository,
97: 	NewTLSFingerprintProfileRepository,
98: 	NewChannelRepository,
99: 	NewChannelMonitorRepository,
100: 	NewChannelMonitorV2Repository,
101: 	NewChannelIntelligenceRepository,
102: 	NewAccountIntelligenceRepository,
103: 	NewChannelMonitorRequestTemplateRepository,
```

## backend/internal/service/wire.go

[完整参考文件](../reference/shared/backend/internal/service/wire.go)

```text
701: 	NewProxyService,
702: 	NewRedeemService,
703: 	NewPromoService,
704: 	NewUsageService,
705: 	NewDashboardService,
706: 	ProvidePricingService,
707: 	NewDomesticPricingService,
708: 	NewBillingService,
709: 	ProvideBillingCacheService,
710: 	NewAnnouncementService,
711: 	NewAdminService,
712: 	NewGatewayService,
713: 	ProvideMediaGatewayService,
714: 	NewOpenAIGatewayService,
715: 	ProvideBatchImageModelPricingResolver,
```

## backend/internal/handler/wire.go

[完整参考文件](../reference/shared/backend/internal/handler/wire.go)

```text
34: 	userAttributeHandler *admin.UserAttributeHandler,
35: 	errorPassthroughHandler *admin.ErrorPassthroughHandler,
36: 	tlsFingerprintProfileHandler *admin.TLSFingerprintProfileHandler,
37: 	apiKeyHandler *admin.AdminAPIKeyHandler,
38: 	scheduledTestHandler *admin.ScheduledTestHandler,
39: 	channelHandler *admin.ChannelHandler,
40: 	domesticPricingHandler *admin.DomesticPricingHandler,
41: 	channelMonitorHandler *admin.ChannelMonitorHandler,
42: 	channelMonitorTemplateHandler *admin.ChannelMonitorRequestTemplateHandler,
43: 	contentModerationHandler *admin.ContentModerationHandler,
44: 	promptAuditHandler *securityaudit.PromptAdminHandler,
45: 	paymentHandler *admin.PaymentHandler,
46: 	affiliateHandler *admin.AffiliateHandler,
47: 	complianceHandler *admin.ComplianceHandler,
48: 	auditLogHandler *admin.AuditLogHandler,
```

```text
75: 		UserAttribute:          userAttributeHandler,
76: 		ErrorPassthrough:       errorPassthroughHandler,
77: 		TLSFingerprintProfile:  tlsFingerprintProfileHandler,
78: 		APIKey:                 apiKeyHandler,
79: 		ScheduledTest:          scheduledTestHandler,
80: 		Channel:                channelHandler,
81: 		DomesticPricing:        domesticPricingHandler,
82: 		ChannelMonitor:         channelMonitorHandler,
83: 		ChannelMonitorTemplate: channelMonitorTemplateHandler,
84: 		ContentModeration:      contentModerationHandler,
85: 		PromptAudit:            promptAuditHandler,
86: 		Payment:                paymentHandler,
87: 		Affiliate:              affiliateHandler,
88: 		Compliance:             complianceHandler,
89: 		AuditLog:               auditLogHandler,
```

```text
249: 	admin.NewUserAttributeHandler,
250: 	admin.NewErrorPassthroughHandler,
251: 	admin.NewTLSFingerprintProfileHandler,
252: 	admin.NewAdminAPIKeyHandler,
253: 	admin.NewScheduledTestHandler,
254: 	admin.NewChannelHandler,
255: 	admin.NewDomesticPricingHandler,
256: 	admin.NewChannelMonitorHandler,
257: 	admin.NewChannelMonitorRequestTemplateHandler,
258: 	admin.NewContentModerationHandler,
259: 	admin.NewPaymentHandler,
260: 	admin.NewAffiliateHandler,
261: 	admin.NewComplianceHandler,
262: 	admin.NewAuditLogHandler,
263: 
```

## backend/internal/handler/handler.go

[完整参考文件](../reference/shared/backend/internal/handler/handler.go)

```text
30: 	UserAttribute          *admin.UserAttributeHandler
31: 	ErrorPassthrough       *admin.ErrorPassthroughHandler
32: 	TLSFingerprintProfile  *admin.TLSFingerprintProfileHandler
33: 	APIKey                 *admin.AdminAPIKeyHandler
34: 	ScheduledTest          *admin.ScheduledTestHandler
35: 	Channel                *admin.ChannelHandler
36: 	DomesticPricing        *admin.DomesticPricingHandler
37: 	ChannelMonitor         *admin.ChannelMonitorHandler
38: 	ChannelMonitorTemplate *admin.ChannelMonitorRequestTemplateHandler
39: 	ContentModeration      *admin.ContentModerationHandler
40: 	PromptAudit            *securityaudit.PromptAdminHandler
41: 	Payment                *admin.PaymentHandler
42: 	Affiliate              *admin.AffiliateHandler
43: 	Compliance             *admin.ComplianceHandler
44: 	AuditLog               *admin.AuditLogHandler
```

## backend/internal/server/routes/admin.go

[完整参考文件](../reference/shared/backend/internal/server/routes/admin.go)

```text
103: 		// 定时测试计划
104: 		registerScheduledTestRoutes(admin, h)
105: 
106: 		// 渠道管理
107: 		registerChannelRoutes(admin, h)
108: 
109: 		// 国模国际 API 美元定价目录与管理员覆盖
110: 		registerDomesticPricingRoutes(admin, h)
111: 
112: 		// 渠道监控
113: 		registerChannelMonitorRoutes(admin, h)
114: 
115: 		// 风控中心
116: 		registerContentModerationRoutes(admin, h)
117: 
118: 		// 独立提示词输入审计
```

```text
132: 		mediaCatalog.GET("", h.Admin.Setting.GetMediaCatalog)
133: 		mediaCatalog.PUT("/draft", h.Admin.Setting.SaveMediaCatalogDraft)
134: 		mediaCatalog.POST("/publish", h.Admin.Setting.PublishMediaCatalog)
135: 	}
136: }
137: 
138: func registerDomesticPricingRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
139: 	pricing := admin.Group("/domestic-pricing")
140: 	{
141: 		pricing.GET("/catalog", h.Admin.DomesticPricing.GetCatalog)
142: 		pricing.PUT("/catalog", h.Admin.DomesticPricing.PutOverride)
143: 		pricing.PUT("/catalog/:provider/:model", h.Admin.DomesticPricing.PutOverride)
144: 		pricing.DELETE("/catalog/:provider/:model", h.Admin.DomesticPricing.DeleteOverride)
145: 	}
146: }
147: 
148: func registerPromptAuditRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
149: 	promptAudit := admin.Group("/prompt-audit")
150: 	{
151: 		promptAudit.GET("/config", h.Admin.PromptAudit.GetConfig)
152: 		promptAudit.PUT("/config", h.Admin.PromptAudit.UpdateConfig)
```

## backend/internal/pkg/openai_compat/upstream_capability.go

[完整参考文件](../reference/protocol/backend/internal/pkg/openai_compat/upstream_capability.go)

```text
43: 	ResponsesSupportModeAuto ResponsesSupportMode = "auto"
44: 
45: 	// ResponsesSupportModeForceResponses 强制使用 /v1/responses。
46: 	ResponsesSupportModeForceResponses ResponsesSupportMode = "force_responses"
47: 
48: 	// ResponsesSupportModeForceChatCompletions 强制使用 /v1/chat/completions。
49: 	ResponsesSupportModeForceChatCompletions ResponsesSupportMode = "force_chat_completions"
50: )
51: 
52: // ExtraKeyResponsesMode 是 accounts.extra JSON 中存储手动覆盖模式的键名。
53: // 值类型为 string：auto=跟随探测，force_responses=强制 Responses，
54: // force_chat_completions=强制 Chat Completions。
55: const ExtraKeyResponsesMode = "openai_responses_mode"
56: 
57: // ExtraKeyResponsesSupported 是 accounts.extra JSON 中存储自动探测结果的键名。
58: // 值类型为 bool：true=支持、false=不支持、键缺失=未探测。
59: const ExtraKeyResponsesSupported = "openai_responses_supported"
60: 
61: // NormalizeResponsesSupportMode 归一化账号级 Responses API 路由覆盖模式。
62: // 缺失或非法值按 auto 处理，以保持存量行为。
63: func NormalizeResponsesSupportMode(mode string) ResponsesSupportMode {
```

## backend/internal/service/account.go

[完整参考文件](../reference/protocol/backend/internal/service/account.go)

```text
90: 	OpenAIEndpointCapabilityChatCompletions OpenAIEndpointCapability = "chat_completions"
91: 	OpenAIEndpointCapabilityEmbeddings      OpenAIEndpointCapability = "embeddings"
92: 	OpenAIEndpointCapabilityAlphaSearch     OpenAIEndpointCapability = "alpha_search"
93: 	OpenAIEndpointCapabilityLive            OpenAIEndpointCapability = "live"
94: 	// OpenAIEndpointCapabilityResponses 表示上游确实提供 /v1/responses 端点。
95: 	// 与其他能力不同：支持状态来自 accounts.extra 的自动探测标记
96: 	// （openai_responses_supported / openai_responses_mode），而非
97: 	// credentials["openai_capabilities"] 配置集。仅用于生图意图的 /v1/responses
98: 	// 调度，避免把请求调度到会在 forward 阶段被降级为 Chat Completions 的账号（#4417）。
99: 	OpenAIEndpointCapabilityResponses OpenAIEndpointCapability = "responses"
100: )
101: 
102: const openAIEndpointCapabilitiesCredentialKey = "openai_capabilities"
103: 
104: const (
```

