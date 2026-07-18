<template>
  <AppLayout title="档位设置" subtitle="调整用户可申请的余额和订阅档位">
    <div class="surface section" style="margin-bottom: 16px">
      <div class="toolbar admin-tier-toolbar">
        <div class="toolbar-copy">
          <div style="font-weight: 700">同步状态</div>
          <div class="muted">最近一次与 {{ branding.title }} 的本地同步时间，订阅分组限额实时读取。</div>
        </div>
        <div class="toolbar-actions">
          <el-button :icon="Refresh" :loading="loading" @click="loadAll">刷新</el-button>
          <el-button :icon="Upload" @click="sync">同步兑换码</el-button>
          <el-button type="primary" :icon="Check" @click="save">保存档位</el-button>
        </div>
      </div>

      <div class="grid-stats" style="margin-bottom: 16px">
        <div class="stat"><div class="label">启用档位</div><div class="value">{{ enabledCount }}</div></div>
        <div class="stat"><div class="label">最近同步</div><div class="value" style="font-size: 16px">{{ formatTime(stats?.last_sync_at) }}</div></div>
      </div>

      <div class="concurrency-monitor" aria-live="polite">
        <div class="monitor-heading">
          <div>
            <div style="font-weight: 700">订阅并发监控</div>
            <div class="muted">回退并发由 sub2api 全局默认值提供</div>
          </div>
          <div class="monitor-actions">
            <span class="monitor-source">来源：sub2api</span>
            <el-button size="small" :icon="List" @click="openMonitorDetails">查看详情</el-button>
          </div>
        </div>
        <template v-if="monitorStatus">
          <div class="monitor-stats">
            <span>默认并发 <strong>{{ monitorStatus.default_concurrency || '-' }}</strong></span>
            <span>最近检查 {{ formatTime(monitorStatus.last_reconciliation_at) }}</span>
            <span>有效 {{ monitorStatus.active_grants }}</span>
            <span>待激活 {{ monitorStatus.pending_grants }}</span>
            <span>已失效 {{ monitorStatus.inactive_grants }}</span>
            <span>失败 {{ monitorStatus.error_grants }}</span>
            <span>人工接管 {{ monitorStatus.manual_override_users }}</span>
          </div>
          <div v-if="monitorStatus.default_concurrency_error || monitorStatus.latest_error" class="monitor-errors">
            <div v-if="monitorStatus.default_concurrency_error" class="monitor-error">
              默认并发读取失败：{{ monitorStatus.default_concurrency_error }}
            </div>
            <div v-if="monitorStatus.latest_error" class="monitor-error">
              最近协调错误：{{ monitorStatus.latest_error }}
              <template v-if="monitorStatus.latest_error_at">（{{ formatTime(monitorStatus.latest_error_at) }}）</template>
            </div>
          </div>
        </template>
        <div v-else-if="monitorError" class="monitor-error">{{ monitorError }}</div>
        <div v-else class="muted">正在读取监控状态</div>
      </div>

      <section class="reset-admin-monitor" aria-live="polite">
        <div class="monitor-heading">
          <div>
            <div style="font-weight: 700">订阅额度重置</div>
            <div class="muted">查看旧订阅补发进度，并处置无法自动确认的重置操作。</div>
          </div>
          <el-button size="small" :icon="Refresh" :loading="resetAdminLoading" @click="loadResetAdminState">刷新</el-button>
        </div>
        <el-alert v-if="resetAdminError" :title="resetAdminError" type="error" :closable="false" show-icon />
        <div class="reset-admin-grid">
          <div class="reset-admin-section">
            <div class="reset-section-title">
              <strong>待确认操作</strong>
              <el-tag type="warning" effect="plain">{{ resetAttempts.length }}</el-tag>
            </div>
            <el-table :data="resetAttempts" size="small" max-height="300" empty-text="暂无待确认操作">
              <el-table-column prop="id" label="操作" width="72" />
              <el-table-column label="用户 / 订阅" min-width="150">
                <template #default="{ row }">
                  <div class="reset-operation-target">
                    <strong>{{ row.username || `用户 ${row.upstream_user_id}` }}</strong>
                    <small>{{ row.email || `用户 ID ${row.upstream_user_id}` }}</small>
                    <small>订阅 {{ row.upstream_subscription_id }}</small>
                  </div>
                </template>
              </el-table-column>
              <el-table-column label="状态" width="100">
                <template #default="{ row }">
                  <el-tag :type="row.status === 'uncertain' ? 'warning' : 'info'" size="small">
                    {{ row.status === 'uncertain' ? '待人工确认' : '执行中' }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column label="预占时间" min-width="160">
                <template #default="{ row }">{{ formatTime(row.reserved_at) }}</template>
              </el-table-column>
              <el-table-column label="操作" width="190" fixed="right">
                <template #default="{ row }">
                  <template v-if="row.status === 'uncertain'">
                    <el-button size="small" type="success" text @click="openResetResolution(row, 'consumed')">确认已消耗</el-button>
                    <el-button size="small" type="danger" text @click="openResetResolution(row, 'released')">释放次数</el-button>
                  </template>
                  <span v-else class="muted">等待上游完成</span>
                </template>
              </el-table-column>
            </el-table>
          </div>

          <div class="reset-admin-section">
            <div class="reset-section-title">
              <strong>历史订阅补发</strong>
              <el-tag effect="plain">{{ resetBackfills.length }}</el-tag>
            </div>
            <el-table :data="resetBackfills" size="small" max-height="300" empty-text="暂无补发任务">
              <el-table-column prop="tier_id" label="档位" width="72" />
              <el-table-column label="进度" min-width="190">
                <template #default="{ row }">
                  <div class="backfill-progress">
                    <el-progress :percentage="backfillPercentage(row)" :stroke-width="7" />
                    <small>已处理 {{ row.processed_records }} / {{ row.total_records }}，已补发 {{ row.granted_records }}</small>
                  </div>
                </template>
              </el-table-column>
              <el-table-column label="状态" width="90">
                <template #default="{ row }">
                  <el-tag :type="backfillTagType(row.status)" size="small">{{ backfillStatusLabel(row.status) }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="retry_count" label="重试" width="72" />
              <el-table-column prop="error_message" label="最近错误" min-width="150" show-overflow-tooltip>
                <template #default="{ row }">
                  <div class="backfill-error">
                    <span>{{ row.error_message || '-' }}</span>
                    <small v-if="row.last_error_at">{{ formatTime(row.last_error_at) }}</small>
                  </div>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </div>
      </section>

      <TierEditor
        v-model="tiers"
        :subscription-groups="subscriptionGroups"
        :groups-loading="groupsLoading"
        :groups-error="groupsError"
        :default-concurrency="monitorStatus?.default_concurrency"
      />
    </div>

    <el-dialog v-model="monitorDetailsVisible" title="订阅并发监控详情" width="min(1120px, 94vw)" top="5vh">
      <div class="monitor-detail-toolbar">
        <el-select v-model="monitorDetailFilter" aria-label="状态筛选" style="width: 150px">
          <el-option label="全部状态" value="all" />
          <el-option label="有效" value="active" />
          <el-option label="待激活" value="pending" />
          <el-option label="已失效" value="inactive" />
          <el-option label="人工接管" value="manual" />
          <el-option label="异常" value="error" />
        </el-select>
        <el-button :icon="Refresh" :loading="monitorDetailsLoading" @click="loadMonitorDetails">刷新</el-button>
      </div>
      <el-alert v-if="monitorDetailsError" :title="monitorDetailsError" type="error" :closable="false" show-icon />
      <el-table
        v-else
        v-loading="monitorDetailsLoading"
        :data="filteredMonitorDetails"
        max-height="560"
        empty-text="暂无符合条件的监控用户"
        table-layout="fixed"
      >
        <el-table-column label="用户" min-width="210">
          <template #default="{ row }">
            <div class="monitor-user">
              <strong>{{ row.username || `用户 ${row.upstream_user_id}` }}</strong>
              <span>{{ row.email || '-' }}</span>
              <span>ID {{ row.upstream_user_id }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="状态" min-width="180">
          <template #default="{ row }">
            <div class="monitor-tags">
              <el-tag v-if="row.manual_override" type="warning" size="small">人工接管</el-tag>
              <el-tag v-if="row.last_error" type="danger" size="small">异常</el-tag>
              <el-tag v-if="row.active_grants" type="success" size="small">有效</el-tag>
              <el-tag v-if="row.pending_grants" type="info" size="small">待激活</el-tag>
              <el-tag v-if="row.inactive_grants" size="small">已失效</el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="当前 / 目标" width="118" align="center">
          <template #default="{ row }">{{ row.current_concurrency ?? '-' }} / {{ row.target_concurrency }}</template>
        </el-table-column>
        <el-table-column label="有效 / 待激活 / 失效" width="156" align="center">
          <template #default="{ row }">{{ row.active_grants }} / {{ row.pending_grants }} / {{ row.inactive_grants }}</template>
        </el-table-column>
        <el-table-column label="最近检查" width="172">
          <template #default="{ row }">{{ formatTime(row.last_synced_at) }}</template>
        </el-table-column>
        <el-table-column prop="last_error" label="最近错误" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">{{ row.last_error || '-' }}</template>
        </el-table-column>
      </el-table>
    </el-dialog>

    <el-dialog
      v-model="resetResolutionVisible"
      :title="resetResolution === 'consumed' ? '确认重置已消耗' : '确认释放重置次数'"
      width="min(500px, 92vw)"
      modal-class="reset-resolution-overlay"
    >
      <div v-if="selectedResetAttempt" class="resolution-copy">
        <el-alert
          :title="resetResolution === 'consumed' ? '确认后保留本次次数消耗，并将操作标记为成功。' : '确认后将操作标记为失败，并归还本次预占次数。'"
          :type="resetResolution === 'consumed' ? 'warning' : 'error'"
          :closable="false"
          show-icon
        />
        <el-descriptions :column="1" border size="small">
          <el-descriptions-item label="操作 ID">{{ selectedResetAttempt.id }}</el-descriptions-item>
          <el-descriptions-item label="请求 ID">{{ selectedResetAttempt.request_id }}</el-descriptions-item>
          <el-descriptions-item label="用户 / 订阅">
            {{ selectedResetAttempt.upstream_user_id }} / {{ selectedResetAttempt.upstream_subscription_id }}
          </el-descriptions-item>
          <el-descriptions-item label="目标窗口">{{ resetAttemptTargets(selectedResetAttempt) }}</el-descriptions-item>
          <el-descriptions-item label="权益周期">
            {{ formatTime(selectedResetAttempt.period?.period_start) }} 至 {{ formatTime(selectedResetAttempt.period?.period_end) }}
          </el-descriptions-item>
        </el-descriptions>
        <div class="snapshot-grid">
          <section class="snapshot-section">
            <h3>操作前快照</h3>
            <div v-if="selectedResetAttempt.snapshot_error" class="snapshot-error">快照数据不可用</div>
            <div v-else-if="selectedResetAttempt.before_snapshot?.length" class="snapshot-list">
              <div v-for="window in selectedResetAttempt.before_snapshot" :key="`before-${window.kind}`" class="snapshot-row">
                <strong>{{ quotaKindLabel(window.kind) }}</strong>
                <span>已用 {{ formatUSD(window.used_usd) }} / {{ formatUSD(window.limit_usd) }}</span>
                <small>窗口开始 {{ formatTime(window.window_start) }}</small>
              </div>
            </div>
            <div v-else class="muted">无操作前快照</div>
          </section>
          <section class="snapshot-section">
            <h3>当前快照</h3>
            <el-alert
              v-if="selectedResetAttempt.current_snapshot_error"
              :title="resetReasonLabel(selectedResetAttempt.current_snapshot_error)"
              type="warning"
              :closable="false"
              show-icon
            />
            <div v-else-if="selectedResetAttempt.current_snapshot?.length" class="snapshot-list">
              <div v-for="window in selectedResetAttempt.current_snapshot" :key="`current-${window.kind}`" class="snapshot-row">
                <strong>{{ quotaKindLabel(window.kind) }}</strong>
                <span>已用 {{ formatUSD(window.used_usd) }} / {{ formatUSD(window.limit_usd) }}</span>
                <small>窗口开始 {{ formatTime(window.window_start) }}</small>
              </div>
            </div>
            <div v-else class="muted">无当前快照</div>
          </section>
        </div>
      </div>
      <template #footer>
        <el-button @click="resetResolutionVisible = false">取消</el-button>
        <el-button
          :type="resetResolution === 'consumed' ? 'warning' : 'danger'"
          :loading="resetResolutionLoading"
          @click="submitResetResolution"
        >
          {{ resetResolution === 'consumed' ? '确认已消耗' : '释放次数' }}
        </el-button>
      </template>
    </el-dialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Check, List, Refresh, Upload } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import AppLayout from '@/components/AppLayout.vue'
import TierEditor from '@/components/TierEditor.vue'
import { listRedeemTiers, listSubscriptionConcurrencyDetails, listSubscriptionConcurrencyStatus, listSubscriptionGroups, listSubscriptionResetAttempts, listSubscriptionResetBackfills, resolveSubscriptionResetAttempt, syncRedeemCodes, updateRedeemTiers, stats as fetchStats } from '@/api/admin'
import { ApiError } from '@/api/http'
import type { DashboardStats, RedeemTier, SubscriptionConcurrencyMonitorDetail, SubscriptionConcurrencyMonitorStatus, SubscriptionGroup, SubscriptionResetAttempt, SubscriptionResetBackfillRun } from '@/api/types'
import { useBrandingStore } from '@/stores/branding'
import { tierCodeType } from '@/utils/tiers'
import { quotaKindLabel, resetReasonLabel } from '@/utils/subscriptions'

const tiers = ref<RedeemTier[]>([])
const subscriptionGroups = ref<SubscriptionGroup[]>([])
const stats = ref<DashboardStats | null>(null)
const monitorStatus = ref<SubscriptionConcurrencyMonitorStatus | null>(null)
const monitorError = ref('')
const monitorDetailsVisible = ref(false)
const monitorDetailsLoading = ref(false)
const monitorDetailsError = ref('')
const monitorDetails = ref<SubscriptionConcurrencyMonitorDetail[]>([])
const monitorDetailFilter = ref('all')
const loading = ref(false)
const groupsLoading = ref(false)
const groupsError = ref('')
const resetAdminLoading = ref(false)
const resetAdminError = ref('')
const resetAttempts = ref<SubscriptionResetAttempt[]>([])
const resetBackfills = ref<SubscriptionResetBackfillRun[]>([])
const resetResolutionVisible = ref(false)
const resetResolutionLoading = ref(false)
const selectedResetAttempt = ref<SubscriptionResetAttempt | null>(null)
const resetResolution = ref<'consumed' | 'released'>('consumed')
const branding = useBrandingStore()

const enabledCount = computed(() => tiers.value.filter((tier) => tier.enabled).length)
const filteredMonitorDetails = computed(() => monitorDetails.value.filter((detail) => {
  switch (monitorDetailFilter.value) {
    case 'active': return detail.active_grants > 0
    case 'pending': return detail.pending_grants > 0
    case 'inactive': return detail.inactive_grants > 0
    case 'manual': return detail.manual_override
    case 'error': return Boolean(detail.last_error)
    default: return true
  }
}))

async function openMonitorDetails() {
  monitorDetailsVisible.value = true
  await loadMonitorDetails()
}

async function loadMonitorDetails() {
  monitorDetailsLoading.value = true
  monitorDetailsError.value = ''
  try {
    monitorDetails.value = await listSubscriptionConcurrencyDetails()
  } catch (error: any) {
    monitorDetails.value = []
    monitorDetailsError.value = error?.message ?? '监控详情加载失败'
  } finally {
    monitorDetailsLoading.value = false
  }
}

async function loadAll() {
  loading.value = true
  groupsLoading.value = true
  groupsError.value = ''
  const monitorLoad = loadMonitorStatus()
  const resetAdminLoad = loadResetAdminState()
  try {
    const [tierData, groupData, statData] = await Promise.all([
      listRedeemTiers(),
      listSubscriptionGroups().catch((error: any) => {
        groupsError.value = error?.message ?? '订阅分组加载失败'
        return []
      }),
      fetchStats(),
    ])
    tiers.value = tierData
    subscriptionGroups.value = groupData
    stats.value = statData
  } catch (error: any) {
    ElMessage.error(error?.message ?? '加载档位失败')
  } finally {
    await monitorLoad
    await resetAdminLoad
    loading.value = false
    groupsLoading.value = false
  }
}

async function save() {
  if (tiers.value.some((tier) => tierCodeType(tier) === 'subscription' && Number(tier.concurrency) <= 0)) {
    ElMessage.warning('请为所有订阅档位设置大于 0 的并发数')
    return
  }
  try {
    tiers.value = await updateRedeemTiers(tiers.value)
    ElMessage.success('已保存')
  } catch (error: any) {
    ElMessage.error(error instanceof ApiError && error.reason ? error.reason : (error?.message ?? '保存失败'))
  }
}

async function sync() {
  try {
    const result = await syncRedeemCodes()
    ElMessage.success(`已同步 ${result.updated} 个兑换码`)
    await loadAll()
  } catch (error: any) {
    ElMessage.error(error?.message ?? '同步失败')
  }
}

async function loadMonitorStatus() {
  monitorStatus.value = null
  monitorError.value = ''
  try {
    monitorStatus.value = await listSubscriptionConcurrencyStatus()
  } catch (error: any) {
    monitorError.value = error?.message ?? '订阅并发监控状态加载失败'
  }
}

async function loadResetAdminState() {
  resetAdminLoading.value = true
  resetAdminError.value = ''
  try {
    const [attempts, backfills] = await Promise.all([
      listSubscriptionResetAttempts(),
      listSubscriptionResetBackfills(),
    ])
    resetAttempts.value = attempts
    resetBackfills.value = backfills
  } catch (error: any) {
    resetAdminError.value = error?.message ?? '订阅重置管理状态加载失败'
  } finally {
    resetAdminLoading.value = false
  }
}

function openResetResolution(attempt: SubscriptionResetAttempt, resolution: 'consumed' | 'released') {
  if (attempt.status !== 'uncertain') return
  selectedResetAttempt.value = attempt
  resetResolution.value = resolution
  resetResolutionVisible.value = true
}

async function submitResetResolution() {
  if (!selectedResetAttempt.value || resetResolutionLoading.value) return
  resetResolutionLoading.value = true
  try {
    await resolveSubscriptionResetAttempt(selectedResetAttempt.value.id, resetResolution.value)
    ElMessage.success(resetResolution.value === 'consumed' ? '已确认次数消耗' : '已释放重置次数')
    resetResolutionVisible.value = false
    await loadResetAdminState()
  } catch (error: any) {
    ElMessage.error(error instanceof ApiError && error.reason ? error.reason : (error?.message ?? '人工决议失败'))
  } finally {
    resetResolutionLoading.value = false
  }
}

function resetAttemptTargets(attempt: SubscriptionResetAttempt) {
  const targets: string[] = []
  if (attempt.reset_daily) targets.push('日限额')
  if (attempt.reset_weekly) targets.push('周限额')
  if (attempt.reset_monthly) targets.push('月限额')
  return targets.join('、') || '-'
}

function formatUSD(value?: number | null) {
  return `$${Number(value || 0).toFixed(2)}`
}

function backfillPercentage(run: SubscriptionResetBackfillRun) {
  if (run.total_records <= 0) return run.status === 'succeeded' ? 100 : 0
  return Math.min(100, Math.round((run.processed_records / run.total_records) * 100))
}

function backfillStatusLabel(status: SubscriptionResetBackfillRun['status']) {
  return { pending: '待执行', running: '执行中', succeeded: '已完成', failed: '失败' }[status]
}

function backfillTagType(status: SubscriptionResetBackfillRun['status']) {
  if (status === 'succeeded') return 'success'
  if (status === 'failed') return 'danger'
  if (status === 'running') return 'warning'
  return 'info'
}

function formatTime(value?: string | null) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

onMounted(loadAll)
</script>

<style scoped>
.toolbar-copy {
  min-width: 0;
}

.toolbar-actions {
  display: flex;
  flex: 0 0 auto;
  flex-wrap: wrap;
  gap: 8px;
}

@media (max-width: 720px) {
  .admin-tier-toolbar {
    align-items: stretch;
    flex-direction: column;
  }

  .toolbar-actions {
    justify-content: flex-start;
  }
}

.concurrency-monitor {
  display: grid;
  gap: 10px;
  margin: 0 0 16px;
  padding: 12px 14px;
  border: 1px solid #dfe7f1;
  border-radius: 8px;
  background: #fbfcfe;
}

.monitor-heading,
.monitor-stats {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px 16px;
}

.monitor-stats {
  justify-content: flex-start;
  color: #475569;
  font-size: 13px;
}

.monitor-source {
  color: #1d6fd0;
  font-size: 12px;
  font-weight: 700;
}

.monitor-actions,
.monitor-detail-toolbar,
.monitor-tags {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.monitor-detail-toolbar {
  justify-content: space-between;
  margin-bottom: 12px;
}

.monitor-user {
  display: grid;
  gap: 2px;
  line-height: 1.35;
}

.monitor-user span {
  color: #64748b;
  font-size: 12px;
  overflow-wrap: anywhere;
}

.monitor-error {
  color: #b42318;
  font-size: 13px;
  overflow-wrap: anywhere;
}

.monitor-errors {
  display: grid;
  gap: 4px;
}

.reset-admin-monitor {
  display: grid;
  gap: 12px;
  margin: 0 0 16px;
  padding: 14px 0;
  border-top: 1px solid #dfe7f1;
  border-bottom: 1px solid #dfe7f1;
}

.reset-admin-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.reset-admin-section {
  min-width: 0;
}

.reset-section-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 8px;
}

.reset-operation-target,
.backfill-progress,
.backfill-error,
.resolution-copy {
  display: grid;
  gap: 4px;
}

.reset-operation-target small,
.backfill-progress small,
.backfill-error small {
  color: #64748b;
}

.resolution-copy {
  gap: 14px;
}

.resolution-copy :deep(.el-descriptions__content) {
  overflow-wrap: anywhere;
}

.snapshot-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.snapshot-section {
  min-width: 0;
  display: grid;
  align-content: start;
  gap: 8px;
  padding: 10px;
  border: 1px solid #dfe7f1;
  border-radius: 8px;
  background: #fbfcfe;
}

.snapshot-section h3 {
  margin: 0;
  font-size: 14px;
  letter-spacing: 0;
}

.snapshot-list,
.snapshot-row {
  display: grid;
  gap: 6px;
}

.snapshot-row {
  padding-bottom: 8px;
  border-bottom: 1px solid #e7edf5;
  font-size: 12px;
}

.snapshot-row:last-child {
  padding-bottom: 0;
  border-bottom: 0;
}

.snapshot-row span,
.snapshot-row small {
  color: #64748b;
  overflow-wrap: anywhere;
}

.snapshot-error {
  color: #b42318;
  font-size: 13px;
}

:global(.reset-resolution-overlay .el-dialog) {
  display: flex;
  flex-direction: column;
  max-height: 90vh;
  margin: 5vh auto !important;
}

:global(.reset-resolution-overlay .el-dialog__body) {
  min-height: 0;
  overflow-y: auto;
}

@media (max-width: 980px) {
  .reset-admin-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 600px) {
  .snapshot-grid {
    grid-template-columns: 1fr;
  }
}
</style>
