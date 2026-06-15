import type { AccessRequest } from '../api/types'

export type ApprovalDetailRow = {
  label: string
  value: string
}

export function approvalRequestDetailRows(request: AccessRequest): ApprovalDetailRow[] {
  const rows: ApprovalDetailRow[] = [
    { label: '申请编号', value: `#${request.id}` },
    { label: '申请人', value: `${request.requestor_username} (${request.requestor_email})` },
    { label: '兑换类型', value: approvalCodeTypeLabel(request.code_type) },
    { label: '档位', value: approvalTierLabel(request) },
  ]

  if (approvalCodeType(request.code_type) === 'subscription') {
    rows.push(
      { label: '订阅分组', value: approvalGroupLabel(request) },
      { label: '生效天数', value: `${Number(request.validity_days ?? 0).toFixed(0)} 天` },
      { label: '订阅限额', value: approvalLimitTriplet(request) },
    )
  } else {
    rows.push({ label: '到账金额', value: `${Number(request.amount).toFixed(0)} 美元` })
  }

  rows.push({ label: '实付金额', value: `${Number(request.pay_amount_cny).toFixed(0)} 人民币` })
  return rows
}

function approvalTierLabel(request: AccessRequest) {
  const label = request.tier_label?.trim()
  return label || `#${request.tier_id}`
}

function approvalGroupLabel(request: AccessRequest) {
  const name = request.sub2api_group_name?.trim() || '-'
  const platform = request.sub2api_group_platform?.trim()
  return platform ? `${name} (${platform})` : name
}

function approvalCodeType(codeType?: string | null) {
  return codeType === 'subscription' ? 'subscription' : 'balance'
}

function approvalCodeTypeLabel(codeType?: string | null) {
  return approvalCodeType(codeType) === 'subscription' ? '订阅' : '余额'
}

function approvalLimitTriplet(request: AccessRequest) {
  return `日限 ${approvalLimitValue(request.sub2api_daily_limit_usd)} / 周限 ${approvalLimitValue(request.sub2api_weekly_limit_usd)} / 月限 ${approvalLimitValue(request.sub2api_monthly_limit_usd)}`
}

function approvalLimitValue(value?: number | null) {
  if (value === undefined || value === null) {
    return '-'
  }
  if (Number(value) === 0) {
    return '无限制'
  }
  return `${Number(value).toFixed(0)} USD`
}
