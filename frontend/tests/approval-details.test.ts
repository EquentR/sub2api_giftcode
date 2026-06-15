import assert from 'node:assert/strict'
import { test } from 'node:test'

import { approvalRequestDetailRows } from '../src/utils/approval-details.ts'

test('approvalRequestDetailRows includes subscription snapshot details', () => {
  const rows = approvalRequestDetailRows({
    id: 12,
    requestor_upstream_user_id: 1,
    requestor_email: 'alice@example.com',
    requestor_username: 'alice',
    tier_id: 3,
    code_type: 'subscription',
    tier_label: 'Claude 30 days',
    amount: 0,
    pay_amount_cny: 88,
    sub2api_group_id: 2,
    sub2api_group_name: 'Claude monthly',
    sub2api_group_platform: 'anthropic',
    sub2api_daily_limit_usd: 0,
    sub2api_weekly_limit_usd: 50,
    sub2api_monthly_limit_usd: 120,
    validity_days: 30,
    note: 'need sub',
    status: 'pending',
    approval_token_hash: '',
    approval_token_expires_at: '',
    notification_status: 'sent',
    notification_error: '',
    created_at: '',
    updated_at: '',
  })

  assert.deepEqual(rows, [
    { label: '申请编号', value: '#12' },
    { label: '申请人', value: 'alice (alice@example.com)' },
    { label: '兑换类型', value: '订阅' },
    { label: '档位', value: 'Claude 30 days' },
    { label: '订阅分组', value: 'Claude monthly (anthropic)' },
    { label: '生效天数', value: '30 天' },
    { label: '订阅限额', value: '日限 无限制 / 周限 50 USD / 月限 120 USD' },
    { label: '实付金额', value: '88 人民币' },
  ])
})
