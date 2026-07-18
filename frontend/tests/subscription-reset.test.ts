import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { test } from 'node:test'

import {
  quotaUsagePercentage,
  resetReasonLabel,
  resetTargetSummaries,
} from '../src/utils/subscriptions.ts'

const __dirname = dirname(fileURLToPath(import.meta.url))
const readSource = (path: string) => readFileSync(resolve(__dirname, path), 'utf8')

test('subscription reset reason labels cover stable backend reasons', () => {
  assert.equal(resetReasonLabel('unlimited'), '无限额订阅')
  assert.equal(resetReasonLabel('period_scheduled'), '重置周期尚未开始')
  assert.equal(resetReasonLabel('operation_pending'), '重置结果确认中')
  assert.equal(resetReasonLabel('upstream_unavailable'), '订阅状态暂时不可用')
})

test('quota progress and reset targets are stable for partial limits', () => {
  assert.equal(quotaUsagePercentage({ limit_usd: 10, used_usd: 3 }), 30)
  assert.equal(quotaUsagePercentage({ limit_usd: 0, used_usd: 3 }), 0)
  assert.deepEqual(
    resetTargetSummaries([
      { kind: 'daily', used_usd: 3 },
      { kind: 'monthly', used_usd: 20.5 },
    ]),
    ['日限额（当前已用 $3.00）', '月限额（当前已用 $20.50）'],
  )
})

test('recharge view keeps request tab default and lazily activates subscription management', () => {
  const source = readSource('../src/views/RechargeRequestView.vue')
  assert.match(source, /const activeTab = ref\('request'\)/)
  assert.match(source, /<el-tab-pane[^>]+name="request"/)
  assert.match(source, /<el-tab-pane[^>]+name="subscriptions"[^>]+lazy/)
  assert.match(source, /<SubscriptionManagement :active="activeTab === 'subscriptions'"/)
})

test('subscription management uses UUID idempotency and blocks duplicate submission', () => {
  const source = readSource('../src/components/SubscriptionManagement.vue')
  assert.match(source, /crypto\.randomUUID\(\)/)
  assert.match(source, /if \(pendingSubscriptionIds\.value\.has\(subscription\.id\)\) return/)
  assert.match(source, /watch\(\s*\(\) => props\.active/)
  assert.match(source, /class="unlimited-placeholder"/)
  assert.match(source, /\.subscription-card\s*\{[\s\S]*?min-height:\s*520px/)
  assert.match(source, /首次使用后开始计时/)
})

test('tier editor disables reset count for balance and unlimited tiers', () => {
  const source = readSource('../src/components/TierEditor.vue')
  assert.match(source, /prop="reset_count"/)
  assert.match(source, /:disabled="!tierCanResetQuota\(row\)"/)
  assert.match(source, /reset_count: 0/)
})

test('admin tier page exposes backfill state and explicit manual resolutions', () => {
  const source = readSource('../src/views/AdminTiersView.vue')
  assert.match(source, /listSubscriptionResetAttempts/)
  assert.match(source, /listSubscriptionResetBackfills/)
  assert.match(source, /确认已消耗/)
  assert.match(source, /释放次数/)
  assert.match(source, /resolveSubscriptionResetAttempt/)
  assert.match(source, /操作前快照/)
  assert.match(source, /当前快照/)
  assert.match(source, /before_snapshot/)
  assert.match(source, /current_snapshot/)
  assert.match(source, /current_snapshot_error/)
  assert.match(source, /retry_count/)
  assert.match(source, /last_error_at/)
  assert.match(source, /modal-class="reset-resolution-overlay"/)
  assert.match(source, /reset-resolution-overlay[\s\S]*?max-height:\s*90vh/)
  assert.match(source, /reset-resolution-overlay[\s\S]*?overflow-y:\s*auto/)
})
