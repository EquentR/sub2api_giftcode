<template>
  <section class="subscription-management" aria-live="polite">
    <div class="subscription-toolbar">
      <div>
        <h2>订阅管理</h2>
        <p>查看有效订阅、额度使用和当前周期可用的重置次数。</p>
      </div>
      <el-button :icon="Refresh" :loading="loading" @click="() => loadSubscriptions()">刷新</el-button>
    </div>

    <el-alert v-if="loadError" :title="loadError" type="error" :closable="false" show-icon />

    <div v-loading="loading && !subscriptions.length" class="subscription-grid">
      <article v-for="subscription in subscriptions" :key="subscription.id" class="subscription-card">
        <header class="subscription-card-header">
          <div>
            <span class="platform-label">{{ subscription.group_platform || '订阅' }}</span>
            <h3>{{ subscription.group_name || `订阅 #${subscription.id}` }}</h3>
          </div>
          <el-tag :type="subscription.unlimited ? 'success' : 'info'" effect="light">
            {{ subscription.unlimited ? '无限额' : '有效' }}
          </el-tag>
        </header>

        <div class="subscription-meta">
          <span>剩余 <strong>{{ Math.max(0, subscription.remaining_days) }}</strong> 天</span>
          <span>到期 {{ formatDate(subscription.expires_at) }}</span>
        </div>

        <div class="quota-zone">
          <div v-if="subscription.unlimited" class="unlimited-placeholder">
            <el-icon><MagicStick /></el-icon>
            <strong>无限额订阅</strong>
            <span>当前分组未配置日、周或月额度限制</span>
          </div>
          <div v-else class="quota-windows">
            <div v-for="window in subscription.quota_windows" :key="window.kind" class="quota-window">
              <div class="quota-window-heading">
                <strong>{{ quotaKindLabel(window.kind) }}</strong>
                <span>{{ formatUSD(window.used_usd) }} / {{ formatUSD(window.limit_usd) }}</span>
              </div>
              <el-progress
                :percentage="quotaUsagePercentage(window)"
                :stroke-width="9"
                :show-text="false"
                :status="quotaUsagePercentage(window) >= 90 ? 'warning' : undefined"
              />
              <div class="quota-window-meta">
                <span>剩余 {{ formatUSD(window.remaining_usd) }}</span>
                <span v-if="!window.window_start && !window.resets_at">首次使用后开始计时</span>
                <span v-else-if="window.resets_at">{{ formatDateTime(window.resets_at) }} 刷新</span>
              </div>
            </div>
          </div>
        </div>

        <footer class="subscription-card-footer" :class="{ unlimited: subscription.unlimited }">
          <template v-if="!subscription.unlimited">
            <div class="reset-period-summary">
              <div>
                <span>基础次数</span>
                <strong>{{ subscription.base_reset_remaining }} / {{ subscription.base_reset_limit }}</strong>
              </div>
              <div>
                <span>活动赠送</span>
                <strong>{{ subscription.bonus_reset_remaining }} 次</strong>
              </div>
              <div>
                <span>合计可用</span>
                <strong>{{ subscription.total_reset_remaining }} 次</strong>
              </div>
            </div>
            <div class="entitlement-zone">
              <div class="bonus-detail-trigger">
                <span>{{ subscription.bonus_grants.length ? '赠送次数按到期时间依次使用' : '暂无活动赠送次数' }}</span>
                <el-button v-if="subscription.bonus_grants.length" link type="primary" @click="openBonusDetails(subscription)">查看详情</el-button>
              </div>
              <div v-if="subscription.next_entitlement" class="next-entitlement">
                下次消耗：{{ entitlementTypeLabel(subscription.next_entitlement.type) }}，有效至 {{ formatDateTime(subscription.next_entitlement.expires_at) }}
              </div>
              <div v-else-if="subscription.next_period" class="next-entitlement">
                下一基础周期：{{ subscription.next_period.reset_limit }} 次，{{ formatDate(subscription.next_period.period_start) }} 开始
              </div>
            </div>
            <el-tooltip :content="subscription.can_reset ? '重置所有已配置额度窗口' : resetReasonLabel(subscription.disabled_reason)" placement="top">
              <span class="reset-button-wrap">
                <el-button
                  type="primary"
                  :icon="RefreshRight"
                  :loading="pendingSubscriptionIds.has(subscription.id)"
                  :disabled="!subscription.can_reset || pendingSubscriptionIds.has(subscription.id)"
                  @click="confirmReset(subscription)"
                >
                  重置额度
                </el-button>
              </span>
            </el-tooltip>
            <p class="reset-state" :class="{ available: subscription.can_reset }">
              {{ subscription.can_reset ? '可重置当前已使用额度' : resetReasonLabel(subscription.disabled_reason) }}
            </p>
          </template>
        </footer>
      </article>

      <div v-if="!subscriptions.length && !loading && !loadError" class="subscription-empty">
        当前账号没有有效订阅
      </div>
    </div>

    <el-dialog
      v-model="bonusDetailVisible"
      class="bonus-detail-dialog"
      :title="`${selectedBonusSubscription?.group_name || '订阅'} · 赠送详情`"
      width="min(720px, 94vw)"
      @closed="clearBonusDetails"
    >
      <div v-if="selectedBonusSubscription" class="bonus-detail-list">
        <article v-for="grant in selectedBonusSubscription.bonus_grants" :key="grant.id" class="bonus-detail-row">
          <header>
            <strong>{{ bonusGrantNoteLabel(grant) }}</strong>
            <el-tag :type="grant.reset_remaining > 0 ? 'success' : 'info'" effect="light">
              {{ bonusGrantStatusLabel(grant) }}
            </el-tag>
          </header>
          <div class="bonus-detail-metrics">
            <div><span>获赠</span><strong>{{ grant.reset_limit }} 次</strong></div>
            <div><span>已使用</span><strong>{{ grant.reset_used }} 次</strong></div>
            <div><span>剩余</span><strong>{{ grant.reset_remaining }} 次</strong></div>
            <div><span>到期时间</span><strong>{{ formatDateTime(grant.expires_at) }}</strong></div>
          </div>
        </article>
      </div>
    </el-dialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { MagicStick, Refresh, RefreshRight } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { listSubscriptions, resetSubscriptionQuota } from '@/api/subscriptions'
import type { SubscriptionCard } from '@/api/types'
import {
  bonusGrantNoteLabel,
  bonusGrantStatusLabel,
  quotaKindLabel,
  quotaUsagePercentage,
  resetReasonLabel,
  resetTargetSummaries,
} from '@/utils/subscriptions'

const props = defineProps<{
  active: boolean
}>()

const subscriptions = ref<SubscriptionCard[]>([])
const loading = ref(false)
const loadError = ref('')
const pendingSubscriptionIds = ref(new Set<number>())
const bonusDetailVisible = ref(false)
const bonusDetailSubscriptionID = ref<number | null>(null)
const selectedBonusSubscription = computed(() => subscriptions.value.find(
  (subscription) => subscription.id === bonusDetailSubscriptionID.value,
) ?? null)
let pollTimer: number | undefined

async function loadSubscriptions(options: { silent?: boolean } = {}) {
  if (!props.active) return
  if (!options.silent) loading.value = true
  try {
    const nextSubscriptions = await listSubscriptions()
    subscriptions.value = nextSubscriptions
    syncBonusDetailSubscription(nextSubscriptions)
    loadError.value = ''
  } catch (error: any) {
    loadError.value = error?.message ?? '订阅信息加载失败'
  } finally {
    if (!options.silent) loading.value = false
  }
}

function openBonusDetails(subscription: SubscriptionCard) {
  bonusDetailSubscriptionID.value = subscription.id
  bonusDetailVisible.value = true
}

function clearBonusDetails() {
  bonusDetailSubscriptionID.value = null
}

function syncBonusDetailSubscription(nextSubscriptions: SubscriptionCard[]) {
  if (!bonusDetailVisible.value || bonusDetailSubscriptionID.value === null) return
  if (nextSubscriptions.some((subscription) => subscription.id === bonusDetailSubscriptionID.value)) return
  bonusDetailVisible.value = false
  bonusDetailSubscriptionID.value = null
  ElMessage.warning('订阅状态已经变化，赠送详情已关闭')
}

async function confirmReset(subscription: SubscriptionCard) {
  if (pendingSubscriptionIds.value.has(subscription.id)) return
  if (!subscription.can_reset) return
  pendingSubscriptionIds.value = new Set(pendingSubscriptionIds.value).add(subscription.id)
  const targets = resetTargetSummaries(subscription.quota_windows)
  try {
    await ElMessageBox.confirm(
      `将重置 ${targets.join('、')}，并消耗 1 次${entitlementConfirmation(subscription)}。`,
      '确认重置额度',
      {
        confirmButtonText: '确认重置',
        cancelButtonText: '取消',
        type: 'warning',
      },
    )
  } catch {
    clearPendingSubscription(subscription.id)
    return
  }

  try {
    const requestId = crypto.randomUUID()
    const result = await resetSubscriptionQuota(subscription.id, requestId)
    if (result.operation.status === 'uncertain' || result.operation.status === 'reserved') {
      ElMessage.warning('重置结果正在确认，确认前不会再次扣减次数')
    } else {
      ElMessage.success('额度已重置')
    }
    await loadSubscriptions({ silent: true })
  } catch (error: any) {
    ElMessage.error(error?.message ?? '额度重置失败')
    await loadSubscriptions({ silent: true })
  } finally {
    clearPendingSubscription(subscription.id)
  }
}

function clearPendingSubscription(subscriptionId: number) {
  const next = new Set(pendingSubscriptionIds.value)
  next.delete(subscriptionId)
  pendingSubscriptionIds.value = next
}

function startPolling() {
  stopPolling()
  void loadSubscriptions()
  pollTimer = window.setInterval(() => {
    void loadSubscriptions({ silent: true })
  }, 20000)
}

function stopPolling() {
  if (pollTimer !== undefined) {
    window.clearInterval(pollTimer)
    pollTimer = undefined
  }
}

function formatUSD(value: number) {
  return `$${Number(value || 0).toFixed(2)}`
}

function formatDate(value?: string | null) {
  if (!value) return '-'
  return new Date(value).toLocaleDateString()
}

function formatDateTime(value?: string | null) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

function entitlementTypeLabel(type: 'base_period' | 'bonus_grant') {
  return type === 'bonus_grant' ? '活动赠送次数' : '基础周期次数'
}

function entitlementConfirmation(subscription: SubscriptionCard) {
  const entitlement = subscription.next_entitlement
  if (!entitlement) return '重置机会'
  return `${entitlementTypeLabel(entitlement.type)}（有效至 ${formatDateTime(entitlement.expires_at)}）`
}

watch(
  () => props.active,
  (active) => {
    if (active) startPolling()
    else stopPolling()
  },
  { immediate: true },
)

onBeforeUnmount(stopPolling)
</script>

<style scoped>
.subscription-management {
  display: grid;
  gap: 16px;
}

.subscription-toolbar,
.subscription-card-header,
.subscription-meta,
.quota-window-heading,
.quota-window-meta,
.reset-period-summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.subscription-toolbar h2,
.subscription-card h3,
.subscription-toolbar p,
.reset-state {
  margin: 0;
  letter-spacing: 0;
}

.subscription-toolbar h2 {
  font-size: 18px;
}

.subscription-toolbar p {
  margin-top: 4px;
  color: #64748b;
}

.subscription-grid {
  min-height: 260px;
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 330px), 630px));
  gap: 14px;
  align-items: stretch;
  justify-content: center;
}

.subscription-card {
  box-sizing: border-box;
  width: 100%;
  max-width: 630px;
  min-width: 0;
  min-height: 520px;
  display: grid;
  grid-template-rows: auto auto 1fr auto;
  gap: 14px;
  padding: 18px;
  border: 1px solid #dfe7f1;
  border-radius: 8px;
  background: #fff;
}

.subscription-card-header > div {
  min-width: 0;
}

.subscription-card h3 {
  margin-top: 2px;
  font-size: 17px;
  overflow-wrap: anywhere;
}

.platform-label {
  color: #1d6fd0;
  font-size: 11px;
  font-weight: 800;
  text-transform: uppercase;
}

.subscription-meta {
  color: #64748b;
  font-size: 13px;
}

.subscription-meta strong {
  color: #0f172a;
  font-size: 22px;
}

.quota-zone {
  min-height: 210px;
}

.quota-windows {
  display: grid;
  gap: 16px;
}

.quota-window {
  display: grid;
  gap: 8px;
}

.quota-window-heading,
.quota-window-meta {
  font-size: 13px;
}

.quota-window-heading span,
.quota-window-meta {
  color: #64748b;
}

.unlimited-placeholder {
  box-sizing: border-box;
  min-height: 210px;
  display: grid;
  place-content: center;
  justify-items: center;
  gap: 8px;
  padding: 18px;
  border: 1px dashed #86b7a2;
  border-radius: 8px;
  color: #31624e;
  background: #f1f8f4;
  text-align: center;
}

.unlimited-placeholder :deep(.el-icon) {
  font-size: 34px;
}

.unlimited-placeholder strong {
  font-size: 20px;
}

.unlimited-placeholder span {
  color: #587a6c;
  font-size: 13px;
}

.subscription-card-footer {
  min-height: 222px;
  display: grid;
  align-content: end;
  gap: 10px;
  padding-top: 12px;
  border-top: 1px solid #edf2f7;
}

.subscription-card-footer.unlimited {
  border-top-color: transparent;
}

.reset-period-summary > div {
  display: grid;
  gap: 2px;
}

.reset-period-summary span {
  color: #64748b;
  font-size: 12px;
}

.reset-period-summary strong {
  font-size: 15px;
}

.entitlement-zone {
  min-height: 96px;
  display: grid;
  align-content: start;
  gap: 7px;
}

.bonus-empty,
.next-entitlement {
  color: #64748b;
  font-size: 12px;
  overflow-wrap: anywhere;
}

.bonus-detail-trigger {
  min-height: 28px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  color: #64748b;
  font-size: 12px;
}

.bonus-detail-trigger span {
  overflow-wrap: anywhere;
}

.bonus-detail-list {
  max-height: min(620px, 68vh);
  overflow-y: auto;
  display: grid;
  gap: 10px;
  padding-right: 4px;
}

.bonus-detail-row {
  display: grid;
  gap: 12px;
  padding: 14px;
  border: 1px solid #dfe7f1;
  border-radius: 8px;
  background: #fbfcfe;
}

.bonus-detail-row header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.bonus-detail-row header strong {
  overflow-wrap: anywhere;
}

.bonus-detail-metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
}

.bonus-detail-metrics > div {
  min-width: 0;
  display: grid;
  gap: 3px;
}

.bonus-detail-metrics span {
  color: #64748b;
  font-size: 11px;
}

.bonus-detail-metrics strong {
  font-size: 13px;
  overflow-wrap: anywhere;
}

.next-entitlement {
  color: #315f8a;
}

.reset-button-wrap,
.reset-button-wrap :deep(.el-button) {
  width: 100%;
}

.reset-state {
  min-height: 20px;
  color: #9a5b13;
  font-size: 12px;
  text-align: center;
}

.reset-state.available {
  color: #34785e;
}

.subscription-empty {
  grid-column: 1 / -1;
  min-height: 260px;
  display: grid;
  place-items: center;
  border: 1px dashed #cfd9e6;
  border-radius: 8px;
  color: #64748b;
  background: #fbfcfe;
}

@media (max-width: 600px) {
  .subscription-toolbar {
    align-items: stretch;
    flex-direction: column;
  }

  .subscription-toolbar :deep(.el-button) {
    width: 100%;
  }

  .subscription-grid {
    grid-template-columns: 1fr;
  }

  .subscription-card {
    min-height: 520px;
    padding: 15px;
  }

  .bonus-detail-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
