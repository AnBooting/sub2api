/**
 * 支付跳转的统一出口。
 *
 * 单独成文件是为了让「是否发生跳转」这件事可以被测试断言，
 * 同时把浏览器跳转这一处副作用收敛到一个函数里。
 */
export function navigateToPaymentPage(url: string): void {
  if (typeof window === 'undefined') return
  window.location.assign(url)
}
