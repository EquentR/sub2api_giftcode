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
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Check, List, Refresh, Upload } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import AppLayout from '@/components/AppLayout.vue'
import TierEditor from '@/components/TierEditor.vue'
import { listRedeemTiers, listSubscriptionConcurrencyDetails, listSubscriptionConcurrencyStatus, listSubscriptionGroups, syncRedeemCodes, updateRedeemTiers, stats as fetchStats } from '@/api/admin'
import { ApiError } from '@/api/http'
import type { DashboardStats, RedeemTier, SubscriptionConcurrencyMonitorDetail, SubscriptionConcurrencyMonitorStatus, SubscriptionGroup } from '@/api/types'
import { useBrandingStore } from '@/stores/branding'
import { tierCodeType } from '@/utils/tiers'

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
</style>
