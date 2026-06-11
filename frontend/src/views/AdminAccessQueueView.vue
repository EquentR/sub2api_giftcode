<template>
  <AppLayout title="审批队列" subtitle="审批时可直接发放对应档位的兑换码">
    <div class="surface section" style="margin-bottom: 16px">
      <div class="toolbar">
        <div>
          <div style="font-weight: 700">待处理申请</div>
          <div class="muted">点击“审批”会打开确认窗口并返回兑换码。</div>
        </div>
        <el-button :icon="Refresh" :loading="loading" @click="loadAll">刷新</el-button>
      </div>
      <el-table v-loading="loading" :data="requests" stripe size="small" style="width: 100%">
        <el-table-column prop="id" label="编号" width="80" />
        <el-table-column prop="requestor_username" label="用户" min-width="160" />
        <el-table-column prop="requestor_email" label="邮箱" min-width="220" />
        <el-table-column prop="tier_id" label="档位" min-width="160">
          <template #default="{ row }">{{ tierNameById(row.tier_id) }}</template>
        </el-table-column>
        <el-table-column label="到账金额" width="130">
          <template #default="{ row }">{{ Number(row.amount).toFixed(0) }} 美元</template>
        </el-table-column>
        <el-table-column label="实付金额" width="130">
          <template #default="{ row }">{{ Number(row.pay_amount_cny).toFixed(0) }} 人民币</template>
        </el-table-column>
        <el-table-column prop="note" label="备注" min-width="220" />
        <el-table-column prop="status" label="状态" width="120">
          <template #default="{ row }"><StatusTag :status="row.status" /></template>
        </el-table-column>
        <el-table-column prop="created_at" label="创建时间" min-width="180">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="220" fixed="right">
          <template #default="{ row }">
            <el-button text type="success" :icon="Check" :disabled="row.status !== 'pending'" @click="openReview(row)">
              审批
            </el-button>
            <el-button text type="danger" :icon="Close" :disabled="row.status !== 'pending'" @click="reject(row.id)">
              拒绝
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div class="surface section" style="margin-bottom: 16px">
      <div class="toolbar">
        <div>
          <div style="font-weight: 700">全局兑换码列表</div>
          <div class="muted">本地账本记录的全部已发放兑换码。</div>
        </div>
      </div>
      <div v-loading="loading">
        <CodeTable :codes="codes" />
      </div>
    </div>

    <el-dialog v-model="reviewDialogVisible" title="审批申请" width="720px">
      <div v-if="selectedRequest">
        <el-descriptions :column="2" border size="small" style="margin-bottom: 16px">
          <el-descriptions-item label="用户">{{ selectedRequest.requestor_username }}</el-descriptions-item>
          <el-descriptions-item label="邮箱">{{ selectedRequest.requestor_email }}</el-descriptions-item>
          <el-descriptions-item label="档位">{{ tierNameById(selectedRequest.tier_id) }}</el-descriptions-item>
          <el-descriptions-item label="到账金额">{{ Number(selectedRequest.amount).toFixed(0) }} 美元</el-descriptions-item>
          <el-descriptions-item label="实付金额">{{ Number(selectedRequest.pay_amount_cny).toFixed(0) }} 人民币</el-descriptions-item>
          <el-descriptions-item label="备注">{{ selectedRequest.note || '-' }}</el-descriptions-item>
        </el-descriptions>

        <el-alert
          v-if="issuedCode"
          type="success"
          :closable="false"
          title="兑换码已发放"
          style="margin-bottom: 12px"
        />
        <div v-if="issuedCode" style="margin-bottom: 12px">
          <el-input :model-value="issuedCode.code" readonly>
            <template #append>
              <el-button :icon="CopyDocument" type="primary" @click="copyIssued">复制兑换码</el-button>
            </template>
          </el-input>
        </div>
      </div>

      <template #footer>
        <el-button @click="closeReview">关闭</el-button>
        <el-button
          type="danger"
          :disabled="!selectedRequest || selectedRequest.status !== 'pending' || approving"
          @click="rejectSelected"
        >
          拒绝
        </el-button>
        <el-button
          type="primary"
          :loading="approving"
          :disabled="!selectedRequest || selectedRequest.status !== 'pending'"
          @click="approveSelected"
        >
          审批并发码
        </el-button>
      </template>
    </el-dialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { Check, Close, CopyDocument, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import AppLayout from '@/components/AppLayout.vue'
import CodeTable from '@/components/CodeTable.vue'
import StatusTag from '@/components/StatusTag.vue'
import { approveAccessRequest, listAccessRequests, listBalanceTiers, listRedeemCodes, listUserRedeemCodes, rejectAccessRequest } from '@/api/admin'
import type { AccessRequest, BalanceTier, RedeemCode } from '@/api/types'


const route = useRoute()
const requests = ref<AccessRequest[]>([])
const codes = ref<RedeemCode[]>([])
const tiers = ref<BalanceTier[]>([])
const selectedRequest = ref<AccessRequest | null>(null)
const issuedCode = ref<RedeemCode | null>(null)
const reviewDialogVisible = ref(false)
const approving = ref(false)
const loading = ref(false)
let refreshTimer: number | undefined

const tierMap = computed(() => new Map(tiers.value.map((tier) => [tier.id, tier])))

type LoadAllOptions = {
  silent?: boolean
}

async function loadAll(options: LoadAllOptions = {}) {
  const silent = options.silent === true
  if (!silent) loading.value = true
  try {
    const userId = Number(route.query.user_id ?? 0)
    const [requestData, codeData, tierData] = await Promise.all([
      listAccessRequests(),
      Number.isFinite(userId) && userId > 0 ? listUserRedeemCodes(userId) : listRedeemCodes(),
      listBalanceTiers(),
    ])
    requests.value = requestData
    codes.value = codeData
    tiers.value = tierData
  } catch (error: any) {
    if (!silent) ElMessage.error(error?.message ?? '加载失败')
  } finally {
    if (!silent) loading.value = false
  }
}

function openReview(row: AccessRequest) {
  selectedRequest.value = row
  issuedCode.value = null
  reviewDialogVisible.value = true
}

function tierById(id: number) {
  return tierMap.value.get(id)
}

async function approveSelected() {
  if (!selectedRequest.value) return
  approving.value = true
  try {
    const result = await approveAccessRequest(selectedRequest.value.id)
    selectedRequest.value = result.request
    issuedCode.value = result.code ?? null
    ElMessage.success('已审批并发码')
    await loadAll()
  } catch (error: any) {
    ElMessage.error(error?.message ?? '审批失败')
  } finally {
    approving.value = false
  }
}

async function rejectSelected() {
  if (!selectedRequest.value) return
  await reject(selectedRequest.value.id)
  closeReview()
}

async function reject(id: number) {
  try {
    await rejectAccessRequest(id)
    ElMessage.success('已拒绝')
    await loadAll()
  } catch (error: any) {
    ElMessage.error(error?.message ?? '拒绝失败')
  }
}

function closeReview() {
  reviewDialogVisible.value = false
  selectedRequest.value = null
  issuedCode.value = null
}

async function copyIssued() {
  if (!issuedCode.value) return
  await navigator.clipboard.writeText(issuedCode.value.code)
  ElMessage.success('已复制')
}

function tierNameById(id: number) {
  const tier = tierById(id)
  if (!tier) return `#${id}`
  return tier.label?.trim() || `#${id}`
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
  refreshTimer = window.setInterval(() => {
    void loadAll({ silent: true })
  }, 15000)
})

onBeforeUnmount(() => {
  if (refreshTimer !== undefined) {
    window.clearInterval(refreshTimer)
  }
})
</script>
