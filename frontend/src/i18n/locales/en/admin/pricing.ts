export default {
  pricing: {
    views: {
      label: 'View',
      channels: 'Channel list',
      table: 'Price table',
      missing: 'Missing prices'
    },
    units: {
      token: 'per million tokens',
      perRequest: 'per request',
      image: 'per image',
      video: 'per second'
    },
    sources: {
      manual: 'Manual',
      default: 'Unset',
      card: 'Price card'
    },
    fields: {
      inputPrice: 'Input price',
      outputPrice: 'Output price',
      cacheWritePrice: 'Cache write price',
      cacheReadPrice: 'Cache read price',
      perRequestPrice: 'Per-request price',
      imagePrice: 'Image price'
    },
    table: {
      title: 'Price table',
      description:
        'Read-only summary of the models that appear in current channel and group pricing rules, with the prices already configured.',
      limitNotice:
        'Limitation: the default price card only exposes a single-model lookup, and no endpoint returns the full-site default price table; this table therefore only lists models that appear in channel or group pricing rules.',
      cardNotice: 'The "Default price card" column only has values after a detection run against the current default price card.',
      columns: {
        model: 'Model',
        currency: 'Currency',
        unit: 'Unit',
        unitPrice: 'Unit price',
        inputPrice: 'Input price',
        outputPrice: 'Output price',
        cacheReadPrice: 'Cache read price',
        cacheWritePrice: 'Cache write price',
        imagePrice: 'Image price',
        card: 'Default price card',
        source: 'Price source',
        scope: 'Source scope'
      },
      scopeChannel: 'Channel {name}',
      scopeGroup: 'Group {name}',
      scopeCard: 'Default price card',
      filterModel: 'Filter by model name',
      filterCurrency: 'Filter by currency',
      allCurrencies: 'All currencies',
      empty: 'No pricing rule data',
      totalCount: '{count} rows',
      range: 'Rows {from}-{to} of {count}',
      pageSize: 'Rows per page',
      prevPage: 'Previous',
      nextPage: 'Next',
      pageInfo: 'Page {page} of {pages}',
      detect: 'Detect with current price card',
      stopDetect: 'Stop detection',
      progress: 'Detected {done} / {total}',
      percent: '{percent}%',
      cardUsable: 'Available',
      cardUnusable: 'Unavailable',
      cardNotProbed: 'Not detected',
      cardFailed: 'Detection failed',
      loading: 'Loading pricing rules…',
      loadFailed: 'Failed to load pricing rules'
    },
    missing: {
      title: 'Missing prices',
      description:
        'Lists models that are configured but whose prices are incomplete; requests for them are rejected before billing (pricing_unavailable).',
      wildcardNotice: 'Wildcard rules are not checked per model (the single-model probe cannot probe wildcards).',
      priorityNotice:
        'Once a group or channel rule matches a model, that whole rule applies; the default price card never overrides a configured rule.',
      probeNotice: 'Results are detected against the current default price card and may differ from real billing.',
      notStarted: 'Not detected yet',
      notStartedHint: 'Click "Start detection" to check every model against the current default price card.',
      noMissing: 'No models with missing prices were detected',
      start: 'Start detection',
      stop: 'Stop detection',
      stopped: 'Detection stopped',
      progress: 'Detected {done} / {total}',
      percent: '{percent}%',
      probeFailed: 'Detection failed',
      cardNotFound: 'The default price card has no entry for this model',
      cardNotReady: 'The default price card is not ready yet',
      allRequired: 'All required price fields',
      columns: {
        model: 'Model',
        scope: 'Source',
        missing: 'Missing',
        probe: 'Detection',
        action: 'Action'
      },
      scopeChannel: 'Channel {name}',
      scopeGroup: 'Group {name}',
      scopeCard: 'Default price card',
      editPricing: 'Set price',
      channelNotFound: 'Channel not found, cannot open its pricing',
      groupFallback: 'No manual pricing entry point to jump to (the default price card does not cover this model); add a pricing rule in the group or channel'
    }
  }
}
