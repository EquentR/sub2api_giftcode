<template>
  <AppLayout title="申请审批" subtitle="提交申请时先选好余额档位">
    <div class="surface section" style="margin-bottom: 16px">
      <div class="toolbar">
        <div>
          <div style="font-weight: 700">提交申请</div>
          <div class="muted">申请会直接带上你选择的档位。</div>
        </div>
        <div style="display: flex; gap: 8px">
          <el-button :icon="Refresh" :loading="loadingData" @click="loadAll">刷新</el-button>
          <el-button type="primary" :icon="Document" :disabled="!enabledTiers.length" @click="openDialog">
            新建申请
          </el-button>
        </div>
      </div>

      <el-table v-loading="loadingCore" :data="enabledTiers" stripe size="small" style="width: 100%">
        <el-table-column prop="label" label="标签" min-width="180">
          <template #default="{ row }">{{ row.label || '-' }}</template>
        </el-table-column>
        <el-table-column prop="amount" label="到账金额" width="140">
          <template #default="{ row }">{{ Number(row.amount).toFixed(0) }} 美元</template>
        </el-table-column>
        <el-table-column prop="pay_amount_cny" label="实付金额" width="140">
          <template #default="{ row }">{{ Number(row.pay_amount_cny).toFixed(0) }} 人民币</template>
        </el-table-column>
        <el-table-column prop="enabled" label="启用" width="100">
          <template #default="{ row }"><StatusTag :status="row.enabled ? 'enabled' : 'disabled'" /></template>
        </el-table-column>
      </el-table>
    </div>

    <div class="surface section" style="margin-bottom: 16px">
      <div class="toolbar">
        <div>
          <div style="font-weight: 700">申请记录</div>
          <div class="muted">记录里会显示你选过的档位。</div>
        </div>
      </div>
      <el-table v-loading="loadingCore" :data="items" stripe size="small" style="width: 100%">
        <el-table-column prop="id" label="编号" width="80" />
        <el-table-column label="档位" min-width="160">
          <template #default="{ row }">{{ tierNameById(row.tier_id) }}</template>
        </el-table-column>
        <el-table-column label="到账金额" width="130">
          <template #default="{ row }">{{ Number(row.amount).toFixed(0) }} 美元</template>
        </el-table-column>
        <el-table-column label="实付金额" width="130">
          <template #default="{ row }">{{ Number(row.pay_amount_cny).toFixed(0) }} 人民币</template>
        </el-table-column>
        <el-table-column prop="note" label="备注" min-width="240" />
        <el-table-column prop="status" label="状态" width="120">
          <template #default="{ row }"><StatusTag :status="row.status" /></template>
        </el-table-column>
        <el-table-column prop="notification_status" label="邮件" width="120">
          <template #default="{ row }"><StatusTag :status="row.notification_status" /></template>
        </el-table-column>
        <el-table-column prop="notification_error" label="邮件错误" min-width="200" />
        <el-table-column prop="created_at" label="创建时间" min-width="180">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
      </el-table>
    </div>

    <div class="surface section">
      <div class="toolbar">
        <div>
          <div style="font-weight: 700">下发兑换码</div>
          <div class="muted">这里会自动展示你已批准后的兑换码。</div>
        </div>
      </div>
      <div v-if="latestCode" style="margin-bottom: 12px">
        <el-input :model-value="latestCode.code" readonly>
          <template #prepend>最新兑换码</template>
          <template #append>
            <el-button :icon="CopyDocument" type="primary" @click="copyLatestCode">复制</el-button>
          </template>
        </el-input>
      </div>
      <div v-loading="loadingCodes">
        <CodeTable :codes="codes" />
      </div>
    </div>

    <el-dialog v-model="dialogVisible" title="提交申请" width="560px">
      <el-form :model="form" label-position="top" @submit.prevent="submit">
        <el-form-item label="余额档位">
          <el-select v-model="form.tierId" placeholder="请选择档位" style="width: 100%">
            <el-option
              v-for="tier in enabledTiers"
              :key="tier.id"
              :label="formatTierDisplay(tier)"
              :value="tier.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.note" type="textarea" :rows="4" maxlength="500" show-word-limit />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="loading" :disabled="!enabledTiers.length" @click="submit">
          提交申请
        </el-button>
      </template>
    </el-dialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { CopyDocument, Document, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import AppLayout from '@/components/AppLayout.vue'
import CodeTable from '@/components/CodeTable.vue'
import StatusTag from '@/components/StatusTag.vue'
import { createAccessRequest, listAccessRequests } from '@/api/access'
import { listBalanceTiers, listRedeemCodes } from '@/api/redeem'
import type { AccessRequest, BalanceTier, RedeemCode } from '@/api/types'
import { formatTierDisplay } from '@/utils/tiers'

const loading = ref(false)
const loadingCore = ref(false)
const loadingCodes = ref(false)
const dialogVisible = ref(false)
const tiers = ref<BalanceTier[]>([])
const items = ref<AccessRequest[]>([])
const codes = ref<RedeemCode[]>([])
let refreshTimer: number | undefined

const route = useRoute()
const form = reactive({
  tierId: 0,
  note: '',
})

const enabledTiers = computed(() => tiers.value.filter((tier) => tier.enabled))
const tierMap = computed(() => new Map(tiers.value.map((tier) => [tier.id, tier])))
const latestCode = computed(() => codes.value[0] ?? null)
const loadingData = computed(() => loadingCore.value || loadingCodes.value)

async function loadAll() {
  await Promise.all([loadCoreData(), loadCodes()])
}

async function loadCoreData() {
  loadingCore.value = true
  try {
    const [tierData, requestData] = await Promise.all([
      listBalanceTiers(),
      listAccessRequests(),
    ])
    tiers.value = tierData
    items.value = requestData
    syncDefaultTier()
  } catch (error: any) {
    ElMessage.error(error?.message ?? '加载申请失败')
  } finally {
    loadingCore.value = false
  }
}

async function loadCodes() {
  loadingCodes.value = true
  try {
    codes.value = await listRedeemCodes()
  } catch (error: any) {
    ElMessage.error(error?.message ?? '加载兑换码失败')
  } finally {
    loadingCodes.value = false
  }
}

function tierById(id: number) {
  return tierMap.value.get(id)
}

function tierNameById(id: number) {
  const tier = tierById(id)
  if (!tier) return `#${id}`
  return tier.label?.trim() || `#${id}`
}

function syncDefaultTier() {
  if (!enabledTiers.value.length) {
    form.tierId = 0
    return
  }
  if (!enabledTiers.value.some((tier) => tier.id === form.tierId)) {
    form.tierId = enabledTiers.value[0].id
  }
}

function openDialog() {
  syncDefaultTier()
  dialogVisible.value = true
}

async function submit() {
  if (!form.tierId) {
    ElMessage.warning('请选择档位')
    return
  }
  loading.value = true
  try {
    await createAccessRequest(form.tierId, form.note)
    ElMessage.success('申请已提交')
    form.note = ''
    dialogVisible.value = false
    await loadAll()
  } catch (error: any) {
    ElMessage.error(error?.message ?? '提交申请失败')
  } finally {
    loading.value = false
  }
}

async function copyLatestCode() {
  if (!latestCode.value) return
  await navigator.clipboard.writeText(latestCode.value.code)
  ElMessage.success('已复制')
}

function refreshWhenVisible() {
  if (document.visibilityState === 'hidden') return
  void loadAll()
}

function refreshFromMenu(event: Event) {
  const targetPath = event instanceof CustomEvent ? event.detail?.path : ''
  if (targetPath && targetPath !== route.path) return
  void loadAll()
}

function formatTime(value?: string | null) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

watch(
  () => route.fullPath,
  () => {
    void loadAll()
  },
  { immediate: true },
)

onMounted(() => {
  window.addEventListener('focus', refreshWhenVisible)
  window.addEventListener('giftcode:refresh-current-view', refreshFromMenu)
  document.addEventListener('visibilitychange', refreshWhenVisible)
  refreshTimer = window.setInterval(() => {
    void loadAll()
  }, 15000)
})

onBeforeUnmount(() => {
  window.removeEventListener('focus', refreshWhenVisible)
  window.removeEventListener('giftcode:refresh-current-view', refreshFromMenu)
  document.removeEventListener('visibilitychange', refreshWhenVisible)
  if (refreshTimer !== undefined) {
    window.clearInterval(refreshTimer)
  }
})
</script>
