const resetReasonLabels: Record<string, string> = {
  unlimited: '无限额订阅',
  external_period: '当前订阅不在本地重置周期内',
  period_scheduled: '重置周期尚未开始',
  zero_reset_limit: '当前周期未配置重置次数',
  reset_exhausted: '当前周期重置次数已用完',
  no_usage: '当前额度尚未产生用量',
  operation_pending: '重置结果确认中',
  subscription_inactive: '订阅当前不可用',
  upstream_unavailable: '订阅状态暂时不可用',
}

const quotaKindLabels: Record<string, string> = {
  daily: '日限额',
  weekly: '周限额',
  monthly: '月限额',
}

export function resetReasonLabel(reason?: string | null) {
  if (!reason) return ''
  return resetReasonLabels[reason] ?? '当前无法重置'
}

export function quotaKindLabel(kind?: string | null) {
  return kind ? (quotaKindLabels[kind] ?? kind) : '-'
}

export function quotaUsagePercentage(window: { limit_usd: number; used_usd: number }) {
  const limit = Number(window.limit_usd)
  if (!Number.isFinite(limit) || limit <= 0) return 0
  const used = Math.max(0, Number(window.used_usd) || 0)
  return Math.min(100, Math.round((used / limit) * 1000) / 10)
}

export function resetTargetSummaries(windows: Array<{ kind: string; used_usd: number }>) {
	return windows.map((window) => `${quotaKindLabel(window.kind)}（当前已用 $${Number(window.used_usd || 0).toFixed(2)}）`)
}
