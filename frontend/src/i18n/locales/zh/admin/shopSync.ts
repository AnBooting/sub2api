export default {
  shopSync: {
    title: '商品同步状态',
    description: '白标充值商品目录的同步状态：同步失败时商品仍展示，但服务端会暂停新订单创建。',
    stateOk: '同步正常',
    stateFailed: '同步失败',
    stateNotConfigured: '未配置',
    stateUnknown: '状态未知',
    loadFailed: '无法获取商品同步状态',
    lastSuccessAt: '最近成功同步',
    lastAttemptAt: '最近同步尝试',
    neverSynced: '尚未成功同步',
    neverAttempted: '尚未尝试同步',
    productCount: '商品数',
    paused: '已暂停购买（无法创建新订单）',
    errors: {
      notConfigured: '未配置充值服务',
      productSyncUnavailable: '尚未成功同步过商品',
      serviceUnavailable: '上游不可达',
      serviceRejected: '上游拒绝请求',
      serviceResponseInvalid: '上游响应无法解析',
      unknown: '商品同步失败'
    }
  }
}
