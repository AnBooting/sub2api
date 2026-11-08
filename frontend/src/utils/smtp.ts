/**
 * SMTP 可用性判定（I-01 契约）—— 前端单一事实源。
 *
 * 后端在 `GET /admin/settings` 上以只读字段 `smtp_configured` 下发
 * 「服务端当前是否认为 SMTP 可用」。前端**不猜**、不从 host/from 是否有值推断：
 * 判定依据是服务端状态，而不是管理员还没保存的表单草稿。
 *
 * 降级口径：字段缺失（后端未上线 / 旧版本）一律视为**不可用（false）**。
 * 理由：此时发码链路会以 EMAIL_NOT_CONFIGURED 失败；与其让管理员点一次
 * 「测试连接」才看到错误，不如直接在前端阻断。
 */

export interface SmtpConfiguredSource {
  smtp_configured?: boolean | null
}

/** 服务端是否声明 SMTP 已配置。缺省 / 非法值 → false。 */
export function isSmtpConfigured(settings: SmtpConfiguredSource | null | undefined): boolean {
  return settings?.smtp_configured === true
}
