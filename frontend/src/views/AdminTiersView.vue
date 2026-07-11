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
          <span class="monitor-source">来源：sub2api</span>
        </div>
        <template v-if="monitorStatus">
          <div class="monitor-stats">
            <span>默认并发 <strong>{{ monitorStatus.default_concurrency || '-' }}</strong></span>
            <span>最近检查 {{ formatTime(monitorStatus.last_reconciliation_at) }}</span>
            <span>有效 {{ monitorStatus.active_grants }}</span>
            <span>待激活 {{ monitorStatus.pending_grants }}</span>
            <span>已失效 {{ monitorStatus.inactive_grants }}</span>
            <span>失败 {{ monitorStatus.error_grants }}</span>
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
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Check, Refresh, Upload } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import AppLayout from '@/components/AppLayout.vue'
import TierEditor from '@/components/TierEditor.vue'
import { listRedeemTiers, listSubscriptionConcurrencyStatus, listSubscriptionGroups, syncRedeemCodes, updateRedeemTiers, stats as fetchStats } from '@/api/admin'
import type { DashboardStats, RedeemTier, SubscriptionConcurrencyMonitorStatus, SubscriptionGroup } from '@/api/types'
import { useBrandingStore } from '@/stores/branding'
import { tierCodeType } from '@/utils/tiers'

const tiers = ref<RedeemTier[]>([])
const subscriptionGroups = ref<SubscriptionGroup[]>([])
const stats = ref<DashboardStats | null>(null)
const monitorStatus = ref<SubscriptionConcurrencyMonitorStatus | null>(null)
const monitorError = ref('')
const loading = ref(false)
const groupsLoading = ref(false)
const groupsError = ref('')
const branding = useBrandingStore()

const enabledCount = computed(() => tiers.value.filter((tier) => tier.enabled).length)

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
    ElMessage.error(error?.message ?? '保存失败')
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
