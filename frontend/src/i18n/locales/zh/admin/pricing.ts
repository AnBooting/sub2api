export default {
  pricing: {
    views: {
      label: '视图',
      channels: '渠道列表',
      table: '价格总表',
      missing: '缺价清单'
    },
    units: {
      token: '每百万 token',
      perRequest: '每次',
      image: '每张',
      video: '每秒'
    },
    sources: {
      manual: '手工',
      default: '缺省',
      card: '价卡'
    },
    fields: {
      inputPrice: '输入价',
      outputPrice: '输出价',
      cacheWritePrice: '缓存写价',
      cacheReadPrice: '缓存读价',
      perRequestPrice: '单次价',
      imagePrice: '图片价'
    },
    table: {
      title: '价格总表',
      description: '汇总当前渠道与分组定价规则中出现过的模型及其已配置价格（只读视图）。',
      limitNotice:
        '限制：默认价卡只提供单模型查询接口，没有任何接口能一次返回全站模型默认价表；本表因此只列出渠道/分组规则里出现过的模型。',
      cardNotice: '「默认价卡」一列只在按当前默认价卡检测之后才有值。',
      columns: {
        model: '模型',
        currency: '币种',
        unit: '计量单位',
        unitPrice: '单价',
        inputPrice: '输入价',
        outputPrice: '输出价',
        cacheReadPrice: '缓存读价',
        cacheWritePrice: '缓存写价',
        imagePrice: '图片价',
        card: '默认价卡',
        source: '价格来源',
        scope: '来源范围'
      },
      scopeChannel: '渠道 {name}',
      scopeGroup: '分组 {name}',
      scopeCard: '默认价卡',
      filterModel: '按模型名过滤',
      filterCurrency: '按币种过滤',
      allCurrencies: '全部币种',
      empty: '暂无定价规则数据',
      totalCount: '共 {count} 条',
      range: '第 {from}-{to} 条，共 {count} 条',
      pageSize: '每页条数',
      prevPage: '上一页',
      nextPage: '下一页',
      pageInfo: '第 {page} / {pages} 页',
      detect: '按当前默认价卡检测',
      stopDetect: '停止检测',
      progress: '已检测 {done} / {total}',
      percent: '{percent}%',
      cardUsable: '可用',
      cardUnusable: '不可用',
      cardNotProbed: '未检测',
      cardFailed: '探测失败',
      loading: '正在加载定价规则…',
      loadFailed: '加载定价规则失败'
    },
    missing: {
      title: '缺价清单',
      description:
        '列出已配置但价格不完整的模型；这类模型的请求会在计费前被拒绝（pricing_unavailable）。',
      wildcardNotice: '通配符规则不参与逐模型检测（单模型探测接口无法探测通配符）。',
      priorityNotice:
        '已命中分组/渠道规则的模型按后端优先级整条规则生效，默认价卡不会覆盖已配置的规则。',
      probeNotice: '结果按当前默认价卡检测，可能与实际计费存在差异。',
      notStarted: '尚未检测',
      notStartedHint: '点击「开始检测」按当前默认价卡逐模型检查缺价情况。',
      noMissing: '没有检测到缺价模型',
      start: '开始检测',
      stop: '停止检测',
      stopped: '检测已停止',
      progress: '已检测 {done} / {total}',
      percent: '{percent}%',
      probeFailed: '探测失败',
      cardNotFound: '默认价卡中没有该模型',
      cardNotReady: '默认价卡尚未就绪',
      allRequired: '全部必需价格字段',
      columns: {
        model: '模型',
        scope: '来源',
        missing: '缺什么',
        probe: '探测',
        action: '操作'
      },
      scopeChannel: '渠道 {name}',
      scopeGroup: '分组 {name}',
      scopeCard: '默认价卡',
      editPricing: '去定价',
      channelNotFound: '找不到对应渠道，无法跳转定价',
      groupFallback: '该模型没有可跳转的手工定价入口（默认价卡未覆盖），请在分组或渠道补齐定价规则'
    }
  }
}
