import type { RedeemCode, RedeemRequest, RedeemTier, SubscriptionGroup } from '@/api/types'

type TierLike = Pick<
  RedeemTier,
  | 'code_type'
  | 'amount'
  | 'pay_amount_cny'
  | 'label'
  | 'sub2api_group_name'
  | 'sub2api_group_platform'
  | 'sub2api_daily_limit_usd'
  | 'sub2api_weekly_limit_usd'
  | 'sub2api_monthly_limit_usd'
  | 'validity_days'
>

type GroupLike = Pick<SubscriptionGroup, 'name' | 'platform' | 'daily_limit_usd' | 'weekly_limit_usd' | 'monthly_limit_usd'>
type LimitsLike = Pick<TierLike, 'sub2api_daily_limit_usd' | 'sub2api_weekly_limit_usd' | 'sub2api_monthly_limit_usd'>

export function tierCodeType(tier?: Pick<RedeemTier, 'code_type'> | Pick<RedeemRequest, 'code_type'> | Pick<RedeemCode, 'code_type'> | null) {
  return tier?.code_type === 'subscription' ? 'subscription' : 'balance'
}

export function formatCodeTypeLabel(value?: string | null) {
  return value === 'subscription' ? '订阅' : '余额'
}

export function formatTierSummary(tier?: Pick<RedeemTier, 'code_type' | 'amount' | 'pay_amount_cny' | 'validity_days'> | null) {
  if (!tier) {
    return '-'
  }
  if (tierCodeType(tier) === 'subscription') {
    const days = Number(tier.validity_days ?? 0)
    return `${days > 0 ? `${days} 天` : '订阅'} / ${Number(tier.pay_amount_cny).toFixed(0)} 人民币`
  }
  return `${Number(tier.amount).toFixed(0)} 美元 / ${Number(tier.pay_amount_cny).toFixed(0)} 人民币`
}

export function formatTierAmount(tier?: Pick<RedeemTier, 'code_type' | 'amount' | 'validity_days'> | null) {
  if (!tier) {
    return '-'
  }
  if (tierCodeType(tier) === 'subscription') {
    const days = Number(tier.validity_days ?? 0)
    return days > 0 ? `${days} 天` : '订阅'
  }
  return `${Number(tier.amount).toFixed(0)} 美元`
}

export function formatTierPayAmount(tier?: Pick<RedeemTier, 'pay_amount_cny'> | null) {
  if (!tier) {
    return '-'
  }
  return `${Number(tier.pay_amount_cny).toFixed(0)} 人民币`
}

export function formatTierDisplay(tier?: TierLike | null) {
  if (!tier) {
    return '-'
  }
  const summary = formatTierSummary(tier)
  const label = tier.label?.trim()
  return label ? `${label} · ${summary}` : summary
}

export function formatLimitValue(value?: number | null) {
  if (value === undefined || value === null) {
    return '不限'
  }
  return `${Number(value).toFixed(0)} USD`
}

export function formatLimitTriplet(item?: GroupLike | TierLike | LimitsLike | null) {
  if (!item) {
    return '日限 - / 周限 - / 月限 -'
  }
  const limits = item as Partial<GroupLike & LimitsLike>
  const daily = limits.daily_limit_usd ?? limits.sub2api_daily_limit_usd
  const weekly = limits.weekly_limit_usd ?? limits.sub2api_weekly_limit_usd
  const monthly = limits.monthly_limit_usd ?? limits.sub2api_monthly_limit_usd
  return `日限 ${formatLimitValue(daily)} / 周限 ${formatLimitValue(weekly)} / 月限 ${formatLimitValue(monthly)}`
}

export function tierGroupLabel(tier?: TierLike | null) {
  if (!tier) {
    return '-'
  }
  const name = tier.sub2api_group_name?.trim() || '-'
  const platform = tier.sub2api_group_platform?.trim()
  return platform ? `${name} (${platform})` : name
}

export function groupLabel(group?: SubscriptionGroup | null) {
  if (!group) {
    return '-'
  }
  return group.platform ? `${group.name} (${group.platform})` : group.name
}

export function isSubscriptionTierAvailable(tier?: RedeemTier | null) {
  if (!tier || tierCodeType(tier) !== 'subscription') {
    return true
  }
  return tier.upstream_available !== false && !!tier.sub2api_group_id
}

export function formatCodeValue(item?: Pick<RedeemCode, 'code_type' | 'value' | 'validity_days'> | Pick<RedeemRequest, 'code_type' | 'value' | 'validity_days'> | null) {
  if (!item) {
    return '-'
  }
  if (tierCodeType(item) === 'subscription') {
    const days = Number(item.validity_days ?? 0)
    return days > 0 ? `${days} 天订阅` : '订阅'
  }
  return `${Number(item.value).toFixed(0)} 美元`
}
