export default {
  shopSync: {
    title: 'Product Sync Status',
    description:
      'Sync status of the white-label recharge product catalog: a failed sync keeps the last successful products visible but pauses new order creation.',
    stateOk: 'Sync healthy',
    stateFailed: 'Sync failed',
    stateNotConfigured: 'Not configured',
    stateUnknown: 'Status unknown',
    loadFailed: 'Could not load the product sync status',
    lastSuccessAt: 'Last successful sync',
    lastAttemptAt: 'Last sync attempt',
    neverSynced: 'Never synced successfully',
    neverAttempted: 'Never attempted',
    productCount: 'Products',
    paused: 'Purchasing is paused (new orders cannot be created)',
    errors: {
      notConfigured: 'The recharge service is not configured',
      productSyncUnavailable: 'No product has been synced successfully yet',
      serviceUnavailable: 'The upstream service is unreachable',
      serviceRejected: 'The upstream service rejected the request',
      serviceResponseInvalid: 'The upstream response could not be parsed',
      unknown: 'Product sync failed'
    }
  }
}
