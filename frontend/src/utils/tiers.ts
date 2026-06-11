import type { BalanceTier } from '@/api/types'

export function formatTierSummary(tier?: Pick<BalanceTier, 'amount' | 'pay_amount_cny'> | null) {
  if (!tier) {
    return '-'
  }
  return `${Number(tier.amount).toFixed(0)} 美元 / ${Number(tier.pay_amount_cny).toFixed(0)} 人民币`
}

export function formatTierAmount(tier?: Pick<BalanceTier, 'amount'> | null) {
  if (!tier) {
    return '-'
  }
  return `${Number(tier.amount).toFixed(0)} 美元`
}

export function formatTierPayAmount(tier?: Pick<BalanceTier, 'pay_amount_cny'> | null) {
  if (!tier) {
    return '-'
  }
  return `${Number(tier.pay_amount_cny).toFixed(0)} 人民币`
}

export function formatTierDisplay(tier?: Pick<BalanceTier, 'label' | 'amount' | 'pay_amount_cny'> | null) {
  if (!tier) {
    return '-'
  }
  const summary = formatTierSummary(tier)
  const label = tier.label?.trim()
  return label ? `${label} · ${summary}` : summary
}
