<template>
  <AppLayout title="档位设置" subtitle="调整用户可申请的余额和订阅档位">
    <div class="surface section" style="margin-bottom: 16px">
      <div class="toolbar">
        <div>
          <div style="font-weight: 700">同步状态</div>
          <div class="muted">最近一次与 {{ branding.title }} 的本地同步时间，订阅分组限额实时读取。</div>
        </div>
        <div style="display: flex; gap: 8px">
          <el-button :icon="Refresh" :loading="loading" @click="loadAll">刷新</el-button>
          <el-button :icon="Upload" @click="sync">同步兑换码</el-button>
          <el-button type="primary" :icon="Check" @click="save">保存档位</el-button>
        </div>
      </div>

      <div class="grid-stats" style="margin-bottom: 16px">
        <div class="stat"><div class="label">启用档位</div><div class="value">{{ enabledCount }}</div></div>
        <div class="stat"><div class="label">最近同步</div><div class="value" style="font-size: 16px">{{ formatTime(stats?.last_sync_at) }}</div></div>
      </div>

      <TierEditor
        v-model="tiers"
        :subscription-groups="subscriptionGroups"
        :groups-loading="groupsLoading"
        :groups-error="groupsError"
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
import { listRedeemTiers, listSubscriptionGroups, syncRedeemCodes, updateRedeemTiers, stats as fetchStats } from '@/api/admin'
import type { DashboardStats, RedeemTier, SubscriptionGroup } from '@/api/types'
import { useBrandingStore } from '@/stores/branding'

const tiers = ref<RedeemTier[]>([])
const subscriptionGroups = ref<SubscriptionGroup[]>([])
const stats = ref<DashboardStats | null>(null)
const loading = ref(false)
const groupsLoading = ref(false)
const groupsError = ref('')
const branding = useBrandingStore()

const enabledCount = computed(() => tiers.value.filter((tier) => tier.enabled).length)

async function loadAll() {
  loading.value = true
  groupsLoading.value = true
  groupsError.value = ''
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
    loading.value = false
    groupsLoading.value = false
  }
}

async function save() {
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

function formatTime(value?: string | null) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

onMounted(loadAll)
</script>
