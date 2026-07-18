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
import { extensionEventStatusLabel } from '../src/utils/subscription-extension-events.ts'
import {
  filterSubscriptionResetEntitlements,
  resetEntitlementGroupLabel,
  resetEntitlementUserLabel,
} from '../src/utils/reset-entitlements.ts'

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
  assert.match(source, /\.unlimited-placeholder\s*\{[\s\S]*?box-sizing:\s*border-box/)
  assert.match(source, /\.subscription-card\s*\{[\s\S]*?min-height:\s*520px/)
  assert.match(source, /首次使用后开始计时/)
  assert.match(source, /base_reset_remaining/)
  assert.match(source, /bonus_reset_remaining/)
  assert.match(source, /total_reset_remaining/)
  assert.match(source, /bonus_grants/)
  assert.match(source, /next_entitlement/)
})

test('tier editor disables reset count for balance and unlimited tiers', () => {
  const source = readSource('../src/components/TierEditor.vue')
  assert.match(source, /prop="reset_count"/)
  assert.match(source, /:disabled="!tierCanResetQuota\(row\)"/)
  assert.match(source, /reset_count: 0/)
})

test('admin tier page keeps reset attempt resolutions and removes legacy backfill state', () => {
  const source = readSource('../src/views/AdminTiersView.vue')
  assert.match(source, /listSubscriptionResetAttempts/)
  assert.doesNotMatch(source, /listSubscriptionResetBackfills/)
  assert.doesNotMatch(source, /历史订阅补发/)
  assert.match(source, /确认已消耗/)
  assert.match(source, /释放次数/)
  assert.match(source, /resolveSubscriptionResetAttempt/)
  assert.match(source, /操作前快照/)
  assert.match(source, /当前快照/)
  assert.match(source, /before_snapshot/)
  assert.match(source, /current_snapshot/)
  assert.match(source, /current_snapshot_error/)
  assert.match(source, /modal-class="reset-resolution-overlay"/)
  assert.match(source, /reset-resolution-overlay[\s\S]*?max-height:\s*90vh/)
  assert.match(source, /reset-resolution-overlay[\s\S]*?overflow-y:\s*auto/)
})

test('admin bonus page previews scoped grants and resolves extension events', () => {
  const source = readSource('../src/views/AdminResetBonusView.vue')
  assert.match(source, /target_scope/)
  assert.match(source, /selected_user_ids/)
  assert.match(source, /group_ids/)
  assert.match(source, /reset_count/)
  assert.match(source, /previewSubscriptionResetBonus/)
  assert.match(source, /createSubscriptionResetBonusBatch/)
  assert.match(source, /listSubscriptionResetBonusBatchDetails/)
  assert.match(source, /listSubscriptionExtensionEvents/)
  assert.match(source, /resolveSubscriptionExtensionEvent/)
  assert.match(source, /确认已应用/)
  assert.match(source, /释放延期/)
})

test('admin bonus user options bind the upstream user id returned by the API', () => {
  const viewSource = readSource('../src/views/AdminResetBonusView.vue')
  const typeSource = readSource('../src/api/types.ts')
  assert.match(typeSource, /interface UserSummary[^{]*\{[\s\S]*?upstream_user_id:\s*number/)
  assert.doesNotMatch(typeSource, /interface UserSummary extends UserProfile/)
  assert.match(viewSource, /:key="user\.upstream_user_id"/)
  assert.match(viewSource, /:value="user\.upstream_user_id"/)
  assert.doesNotMatch(viewSource, /user\.id/)
})

test('admin reset entitlement filters combine user keyword and group without hiding zero counts', () => {
  const items = [
    {
      upstream_user_id: 1, username: 'Alice', email: 'alice@example.com', upstream_subscription_id: 77,
      sub2api_group_id: 7, group_name: 'Standard', total_reset_remaining: 0,
    },
    {
      upstream_user_id: 2, username: 'Bob', email: 'bob@example.com', upstream_subscription_id: 88,
      sub2api_group_id: 8, group_name: 'Premium', total_reset_remaining: 3,
    },
  ]
  assert.deepEqual(filterSubscriptionResetEntitlements(items as any, 'ALICE', null), [items[0]])
  assert.deepEqual(filterSubscriptionResetEntitlements(items as any, '1', 7), [items[0]])
  assert.deepEqual(filterSubscriptionResetEntitlements(items as any, '', null), items)
})

test('admin reset entitlement labels fall back to stable ids', () => {
  const item = { upstream_user_id: 12, username: '', email: '', sub2api_group_id: 21, group_name: '' } as any
  assert.equal(resetEntitlementUserLabel(item), '用户 12')
  assert.equal(resetEntitlementGroupLabel(item), '分组 21')
})

test('admin bonus page renders reset entitlement overview with isolated refresh state', () => {
  const viewSource = readSource('../src/views/AdminResetBonusView.vue')
  const apiSource = readSource('../src/api/admin.ts')
  const typeSource = readSource('../src/api/types.ts')
  assert.match(typeSource, /interface SubscriptionResetEntitlementAdminView/)
  assert.match(apiSource, /url:\s*'\/admin\/subscription-reset-entitlements'/)
  assert.match(viewSource, /当前订阅权益/)
  assert.match(viewSource, /entitlementLoading/)
  assert.match(viewSource, /entitlementError/)
  assert.match(viewSource, /entitlementStale/)
  assert.match(viewSource, /entitlementsLoaded/)
  assert.match(viewSource, /filteredEntitlements/)
  assert.match(viewSource, /total_reset_remaining/)
  assert.match(viewSource, /refreshEntitlements\(\{ silent: true \}\)/)
})

test('extension event labels use backend status and resolution independently', () => {
  assert.equal(extensionEventStatusLabel('succeeded', 'applied'), '已应用')
  assert.equal(extensionEventStatusLabel('failed', 'released'), '已释放')
  assert.equal(extensionEventStatusLabel('uncertain', ''), '待确认')
  assert.equal(extensionEventStatusLabel('reserved', ''), '执行中')
})
