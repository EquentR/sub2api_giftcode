<template>
  <AppLayout title="档位设置" subtitle="调整用户可申请的余额金额">
    <div class="surface section" style="margin-bottom: 16px">
      <div class="toolbar">
        <div>
          <div style="font-weight: 700">同步状态</div>
          <div class="muted">最近一次与 {{ branding.title }} 的本地同步时间。</div>
        </div>
        <div style="display: flex; gap: 8px">
          <el-button :icon="Refresh" @click="loadAll">刷新</el-button>
          <el-button :icon="Upload" @click="sync">同步兑换码</el-button>
          <el-button type="primary" :icon="Check" @click="save">保存档位</el-button>
        </div>
      </div>

      <div class="grid-stats" style="margin-bottom: 16px">
        <div class="stat"><div class="label">启用档位</div><div class="value">{{ enabledCount }}</div></div>
        <div class="stat"><div class="label">最近同步</div><div class="value" style="font-size: 16px">{{ formatTime(stats?.last_sync_at) }}</div></div>
      </div>

      <TierEditor v-model="tiers" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Check, Refresh, Upload } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import AppLayout from '@/components/AppLayout.vue'
import TierEditor from '@/components/TierEditor.vue'
import { listBalanceTiers, syncRedeemCodes, updateBalanceTiers, stats as fetchStats } from '@/api/admin'
import type { BalanceTier, DashboardStats } from '@/api/types'
import { useBrandingStore } from '@/stores/branding'

const tiers = ref<BalanceTier[]>([])
const stats = ref<DashboardStats | null>(null)
const branding = useBrandingStore()

const enabledCount = computed(() => tiers.value.filter((tier) => tier.enabled).length)

async function loadAll() {
  try {
    const [tierData, statData] = await Promise.all([listBalanceTiers(), fetchStats()])
    tiers.value = tierData
    stats.value = statData
  } catch (error: any) {
    ElMessage.error(error?.message ?? '加载档位失败')
  }
}

async function save() {
  try {
    tiers.value = await updateBalanceTiers(tiers.value)
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
