<template>
  <AppLayout title="用户总览" subtitle="当前的申请和已发放兑换码">
    <div class="grid-stats" style="margin-bottom: 16px">
      <div class="stat">
        <div class="label">审批申请</div>
        <div class="value">{{ accessRequests.length }}</div>
      </div>
      <div class="stat">
        <div class="label">直充完成</div>
        <div class="value">{{ directChargeCount }}</div>
      </div>
      <div class="stat">
        <div class="label">已发放兑换码</div>
        <div class="value">{{ redeemCodes.length }}</div>
      </div>
    </div>

    <div class="surface section" style="margin-bottom: 16px">
      <div class="toolbar">
        <div>
          <div style="font-weight: 700">快捷操作</div>
          <div class="muted">进入申请页面提交新档位申请。</div>
        </div>
        <div style="display: flex; gap: 8px">
          <el-button :icon="Document" @click="router.push('/recharge-request')">充值兑换申请</el-button>
        </div>
      </div>
    </div>

    <div class="surface section" style="margin-bottom: 16px">
      <div class="toolbar">
        <div>
          <div style="font-weight: 700">可选档位</div>
          <div class="muted">这里同时显示余额金额、订阅内容和实付金额。</div>
        </div>
      </div>
      <el-table :data="enabledTiers" stripe size="small" style="width: 100%">
        <el-table-column prop="label" label="档位" min-width="180">
          <template #default="{ row }">{{ row.label || '-' }}</template>
        </el-table-column>
        <el-table-column label="内容" width="160">
          <template #default="{ row }">{{ tierContent(row) }}</template>
        </el-table-column>
        <el-table-column prop="pay_amount_cny" label="实付金额" width="140">
          <template #default="{ row }">{{ Number(row.pay_amount_cny).toFixed(0) }} 人民币</template>
        </el-table-column>
      </el-table>
    </div>

    <div class="surface section" style="margin-bottom: 16px">
      <div class="toolbar">
        <div>
          <div style="font-weight: 700">最近申请</div>
          <div class="muted">每个已完成申请会以直充或兑换码的形式交付。</div>
        </div>
      </div>
      <el-table :data="paginatedAccessRequests.items" stripe size="small" style="width: 100%">
        <el-table-column prop="id" label="编号" width="80" />
        <el-table-column label="档位" min-width="160">
          <template #default="{ row }">{{ tierNameById(row.tier_id) }}</template>
        </el-table-column>
        <el-table-column label="内容" width="130">
          <template #default="{ row }">{{ requestContent(row) }}</template>
        </el-table-column>
        <el-table-column label="交付结果" min-width="180">
          <template #default="{ row }">{{ requestDeliverySummary(row) }}</template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="120">
          <template #default="{ row }">
            <StatusTag :status="row.status" />
          </template>
        </el-table-column>
        <el-table-column prop="created_at" label="创建时间" min-width="180">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
      </el-table>
      <el-pagination
        v-if="paginatedAccessRequests.total > accessRequestPageSize"
        v-model:current-page="accessRequestPage"
        class="list-pagination"
        layout="prev, pager, next, total"
        :page-size="accessRequestPageSize"
        :total="paginatedAccessRequests.total"
      />
    </div>

    <div class="surface section">
      <div class="toolbar">
        <div>
          <div style="font-weight: 700">最近兑换码</div>
          <div class="muted">实际发放或直充使用的兑换码会出现在这里。</div>
        </div>
      </div>
      <CodeTable :codes="paginatedRedeemCodes.items" />
      <el-pagination
        v-if="paginatedRedeemCodes.total > redeemCodePageSize"
        v-model:current-page="redeemCodePage"
        class="list-pagination"
        layout="prev, pager, next, total"
        :page-size="redeemCodePageSize"
        :total="paginatedRedeemCodes.total"
      />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Document } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import AppLayout from '@/components/AppLayout.vue'
import CodeTable from '@/components/CodeTable.vue'
import StatusTag from '@/components/StatusTag.vue'
import { listAccessRequests } from '@/api/access'
import { listRedeemCodes, listRedeemTiers } from '@/api/redeem'
import type { AccessRequest, RedeemCode, RedeemTier } from '@/api/types'
import { paginateItems } from '@/utils/pagination'
import { tierCodeType } from '@/utils/tiers'

const router = useRouter()
const accessRequests = ref<AccessRequest[]>([])
const tiers = ref<RedeemTier[]>([])
const redeemCodes = ref<RedeemCode[]>([])
const accessRequestPage = ref(1)
const redeemCodePage = ref(1)
const accessRequestPageSize = 10
const redeemCodePageSize = 10
let refreshTimer: number | undefined

const enabledTiers = computed(() => tiers.value.filter((tier) => tier.enabled))
const tierMap = computed(() => new Map(tiers.value.map((tier) => [tier.id, tier])))
const directChargeCount = computed(() => accessRequests.value.filter((item) => item.fulfilled_via === 'direct_charge').length)
const paginatedAccessRequests = computed(() => paginateItems(accessRequests.value, accessRequestPage.value, accessRequestPageSize))
const paginatedRedeemCodes = computed(() => paginateItems(redeemCodes.value, redeemCodePage.value, redeemCodePageSize))

async function loadAll() {
  try {
    const [access, codes, tierData] = await Promise.all([
      listAccessRequests(),
      listRedeemCodes(),
      listRedeemTiers(),
    ])
    accessRequests.value = access
    redeemCodes.value = codes
    tiers.value = tierData
  } catch (error: any) {
    ElMessage.error(error?.message ?? '加载总览失败')
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

function tierContent(tier: RedeemTier) {
  if (tierCodeType(tier) === 'subscription') {
    const days = Number(tier.validity_days ?? 0)
    return days > 0 ? `${days} 天订阅` : '订阅'
  }
  return `${Number(tier.amount).toFixed(0)} 美元`
}

function requestContent(request: AccessRequest) {
  if (request.code_type === 'subscription') {
    const days = Number(request.validity_days ?? 0)
    return days > 0 ? `${days} 天订阅` : '订阅'
  }
  return `${Number(request.amount).toFixed(0)} 美元`
}

function requestDeliverySummary(request: AccessRequest) {
  if (request.fulfilled_via === 'direct_charge') {
    return '已直充到账'
  }
  if (request.fulfilled_via === 'redeem_code_fallback') {
    return '直充失败，已改为发码'
  }
  if (request.fulfilled_via === 'redeem_code') {
    return '已发放兑换码'
  }
  if (request.fulfillment_mode === 'redeem_code') {
    return '待发放兑换码'
  }
  return '待直充或发码'
}

function formatTime(value?: string | null) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

function refreshWhenVisible() {
  if (document.visibilityState === 'hidden') return
  void loadAll()
}

onMounted(async () => {
  await loadAll()
  window.addEventListener('focus', refreshWhenVisible)
  document.addEventListener('visibilitychange', refreshWhenVisible)
  refreshTimer = window.setInterval(() => {
    void loadAll()
  }, 15000)
})

onBeforeUnmount(() => {
  window.removeEventListener('focus', refreshWhenVisible)
  document.removeEventListener('visibilitychange', refreshWhenVisible)
  if (refreshTimer !== undefined) {
    window.clearInterval(refreshTimer)
  }
})
</script>
