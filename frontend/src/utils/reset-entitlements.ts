import type { SubscriptionResetEntitlementAdminView } from '@/api/types'

export function filterSubscriptionResetEntitlements(
  items: SubscriptionResetEntitlementAdminView[],
  keyword: string,
  groupID: number | null | undefined,
) {
  const normalizedKeyword = keyword.trim().toLocaleLowerCase()
  return items.filter((item) => {
    if (groupID != null && item.sub2api_group_id !== groupID) return false
    if (!normalizedKeyword) return true
    return [item.username, item.email, String(item.upstream_user_id)]
      .some((value) => value.trim().toLocaleLowerCase().includes(normalizedKeyword))
  })
}

export function resetEntitlementUserLabel(item: Pick<SubscriptionResetEntitlementAdminView, 'upstream_user_id' | 'username' | 'email'>) {
  return item.username.trim() || item.email.trim() || `用户 ${item.upstream_user_id}`
}

export function resetEntitlementGroupLabel(item: Pick<SubscriptionResetEntitlementAdminView, 'sub2api_group_id' | 'group_name'>) {
  return item.group_name.trim() || `分组 ${item.sub2api_group_id}`
}
