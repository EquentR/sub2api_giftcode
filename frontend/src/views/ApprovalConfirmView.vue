<template>
  <div class="approval-confirm-page">
    <div class="approval-card">
      <div class="approval-mark" :class="state">
        <el-icon v-if="state === 'success'"><CircleCheck /></el-icon>
        <el-icon v-else-if="state === 'error'"><CircleClose /></el-icon>
        <el-icon v-else><Loading /></el-icon>
      </div>
      <h1>{{ title }}</h1>
      <p>{{ detail }}</p>
      <div v-if="request" class="approval-detail">
        <div>
          <span>申请编号</span>
          <strong>#{{ request.id }}</strong>
        </div>
        <div>
          <span>申请人</span>
          <strong>{{ request.requestor_username }}</strong>
        </div>
        <div>
          <span>到账金额</span>
          <strong>{{ Number(request.amount).toFixed(0) }} 美元</strong>
        </div>
        <div>
          <span>实付金额</span>
          <strong>{{ Number(request.pay_amount_cny).toFixed(0) }} 人民币</strong>
        </div>
      </div>
      <div class="approval-actions">
        <el-button @click="router.push('/recharge-request')">打开充值兑换申请</el-button>
        <el-button
          v-if="state === 'preview'"
          type="primary"
          :loading="confirming"
          @click="confirmApproval"
        >
          确认审批并发码
        </el-button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { CircleCheck, CircleClose, Loading } from '@element-plus/icons-vue'
import { confirmAccessRequest, previewAccessRequest } from '@/api/access'
import type { AccessRequest } from '@/api/types'

type ConfirmState = 'loading' | 'preview' | 'success' | 'error'

const route = useRoute()
const router = useRouter()
const state = ref<ConfirmState>('loading')
const request = ref<AccessRequest | null>(null)
const errorMessage = ref('')
const token = ref('')
const confirming = ref(false)

const title = computed(() => {
  if (state.value === 'success') return '审批已完成'
  if (state.value === 'error') return '审批没有完成'
  if (state.value === 'preview') return '确认审批并发码'
  return '正在加载审批申请'
})

const detail = computed(() => {
  if (state.value === 'success') return '兑换码已经下发，用户可以回到应用查看并复制。'
  if (state.value === 'error') return errorMessage.value || '链接可能已过期或已经处理，请返回应用查看最新状态。'
  if (state.value === 'preview') return '请核对申请人、金额和备注。点击确认后会立即审批并下发兑换码。'
  return '请稍等，系统正在读取这条审批链接。'
})

onMounted(async () => {
  token.value = String(route.query.token ?? '').trim()
  if (!token.value) {
    state.value = 'error'
    errorMessage.value = '审批链接缺少 token。'
    return
  }
  try {
    request.value = await previewAccessRequest(token.value)
    state.value = request.value.status === 'pending' ? 'preview' : 'error'
    if (state.value === 'error') {
      errorMessage.value = '这条审批申请已经处理或不可继续审批。'
    }
  } catch (error: any) {
    state.value = 'error'
    errorMessage.value = error?.message ?? '审批失败。'
  }
})

async function confirmApproval() {
  if (!token.value) return
  confirming.value = true
  try {
    request.value = await confirmAccessRequest(token.value)
    state.value = 'success'
  } catch (error: any) {
    state.value = 'error'
    errorMessage.value = error?.message ?? '审批失败。'
  } finally {
    confirming.value = false
  }
}
</script>

<style scoped>
.approval-confirm-page {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 24px;
  background: #eef3f8;
}

.approval-card {
  width: min(100%, 520px);
  padding: 28px;
  border: 1px solid #dfe7f1;
  border-radius: 10px;
  background: #fff;
  text-align: center;
  box-shadow: 0 18px 44px rgba(15, 23, 42, 0.1);
}

.approval-mark {
  width: 54px;
  height: 54px;
  margin: 0 auto 14px;
  display: grid;
  place-items: center;
  border-radius: 50%;
  font-size: 28px;
  background: #eaf3ff;
  color: #1d6fd0;
}

.approval-mark.success {
  background: #ecfdf5;
  color: #047857;
}

.approval-mark.error {
  background: #fef2f2;
  color: #b91c1c;
}

h1 {
  margin: 0 0 8px;
  font-size: 24px;
  letter-spacing: 0;
}

p {
  margin: 0 0 18px;
  color: #5f6b7a;
}

.approval-detail {
  margin: 0 0 18px;
  border: 1px solid #e3eaf3;
  border-radius: 8px;
  text-align: left;
  overflow: hidden;
}

.approval-detail div {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 12px;
  border-bottom: 1px solid #edf2f7;
}

.approval-detail div:last-child {
  border-bottom: 0;
}

.approval-detail span {
  color: #64748b;
}

.approval-actions {
  display: flex;
  justify-content: center;
  gap: 10px;
  flex-wrap: wrap;
}
</style>
