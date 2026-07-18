export type SubscriptionExtensionEventStatus = 'reserved' | 'succeeded' | 'failed' | 'uncertain'
export type SubscriptionExtensionEventResolution = '' | 'applied' | 'released'

export function extensionEventStatusLabel(
  status: SubscriptionExtensionEventStatus,
  resolution: SubscriptionExtensionEventResolution | string,
) {
  if (resolution === 'applied') return '已应用'
  if (resolution === 'released') return '已释放'
  return {
    reserved: '执行中',
    succeeded: '已完成',
    failed: '失败',
    uncertain: '待确认',
  }[status]
}
