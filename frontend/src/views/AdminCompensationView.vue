<template>
  <AppLayout title="批量补偿" subtitle="按上游用户与订阅状态执行全员补偿并保留审计记录">
    <div class="surface section" style="margin-bottom: 16px">
      <div class="toolbar">
        <div>
          <div style="font-weight: 700">执行补偿</div>
          <div class="muted">可单独选择补订阅或补余额；仅补余额时，有效订阅用户也会获得余额补偿。</div>
        </div>
        <div style="display: flex; gap: 8px">
          <el-button :icon="Refresh" :loading="loading" @click="loadBatches">刷新批次</el-button>
          <el-button type="danger" :icon="Warning" :loading="submitting" @click="submitBatch">立即执行</el-button>
        </div>
      </div>

      <el-alert
        title="风险提示：该操作会面向全量上游用户执行，建议确认补偿天数、补偿金额和排除域名后再提交。"
        type="warning"
        :closable="false"
        style="margin-bottom: 16px"
      />

      <el-form :model="form" label-position="top" class="batch-form">
        <el-form-item label="补偿类型">
          <el-checkbox v-model="form.compensate_subscriptions">补订阅</el-checkbox>
          <el-checkbox v-model="form.compensate_balance">补余额</el-checkbox>
        </el-form-item>
        <el-form-item label="订阅补偿天数">
          <el-input-number v-model="form.subscription_days" :min="1" :step="1" :disabled="!form.compensate_subscriptions" />
        </el-form-item>
        <el-form-item label="余额补偿金额">
          <el-input-number v-model="form.balance_amount" :min="0.01" :step="1" :disabled="!form.compensate_balance" />
        </el-form-item>
        <el-form-item v-if="form.compensate_balance && !form.compensate_subscriptions" label="非正余额用户">
          <el-checkbox v-model="form.compensate_non_positive_balance">补偿余额小于或等于 0 的用户</el-checkbox>
        </el-form-item>
        <el-form-item label="排除邮箱域名">
          <el-input
            v-model="excludedDomainsText"
            type="textarea"
            :rows="3"
            placeholder="支持逗号、空格或换行分隔，例如：example.com, blocked.org"
          />
        </el-form-item>
        <el-form-item label="补偿备注">
          <el-input v-model="form.note" type="textarea" :rows="3" placeholder="将尽力写入上游，并始终保存在本地账本" />
        </el-form-item>
      </el-form>
    </div>

    <div class="grid-stats" style="margin-bottom: 16px" v-if="currentBatch">
      <div class="stat"><div class="label">总用户数</div><div class="value">{{ currentBatch.total_users }}</div></div>
      <div class="stat"><div class="label">域名排除</div><div class="value">{{ currentBatch.excluded_users }}</div></div>
      <div class="stat"><div class="label">订阅补偿成功</div><div class="value">{{ currentBatch.subscription_compensated_users }}</div></div>
      <div class="stat"><div class="label">余额补偿成功</div><div class="value">{{ currentBatch.balance_compensated_users }}</div></div>
      <div class="stat"><div class="label">余额不足跳过</div><div class="value">{{ currentBatch.skipped_zero_balance_users }}</div></div>
      <div class="stat"><div class="label">失败用户</div><div class="value">{{ currentBatch.failed_users }}</div></div>
    </div>

    <div class="surface section" style="margin-bottom: 16px">
      <div class="toolbar">
        <div>
          <div style="font-weight: 700">补偿批次</div>
          <div class="muted">按批次查看汇总结果和输入参数。</div>
        </div>
      </div>
      <el-table :data="batches" stripe size="small" style="width: 100%" @current-change="onCurrentBatchChange" highlight-current-row>
        <el-table-column prop="id" label="批次" width="90" />
        <el-table-column label="状态" width="180">
          <template #default="{ row }">
            <StatusTag :status="row.status" />
          </template>
        </el-table-column>
        <el-table-column label="订阅/余额" min-width="190">
          <template #default="{ row }">
            {{ row.subscription_days }} 天 / {{ row.balance_amount }}
            {{ row.compensate_non_positive_balance ? '（含非正余额）' : '' }}
          </template>
        </el-table-column>
        <el-table-column label="排除域名" min-width="220">
          <template #default="{ row }">{{ row.excluded_domains?.join(', ') || '-' }}</template>
        </el-table-column>
        <el-table-column prop="operator_email" label="操作人" min-width="180" />
        <el-table-column prop="created_at" label="执行时间" min-width="180">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="结果" min-width="220">
          <template #default="{ row }">
            订阅 {{ row.subscription_compensated_users }} / 余额 {{ row.balance_compensated_users }} / 失败 {{ row.failed_users }}
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div class="surface section">
      <div class="toolbar">
        <div>
          <div style="font-weight: 700">批次明细</div>
          <div class="muted">每个用户都有明确处理结果。</div>
        </div>
      </div>
      <el-table :data="details" stripe size="small" style="width: 100%">
        <el-table-column prop="user_email" label="用户" min-width="220" />
        <el-table-column label="判定类型" min-width="140">
          <template #default="{ row }">{{ decisionLabel(row.decision_type) }}</template>
        </el-table-column>
        <el-table-column label="补偿动作" min-width="120">
          <template #default="{ row }">{{ actionLabel(row.action_type) }}</template>
        </el-table-column>
        <el-table-column prop="active_subscription_count" label="订阅数" width="90" />
        <el-table-column label="金额/天数" min-width="140">
          <template #default="{ row }">{{ row.action_type === 'subscription' ? `${row.subscription_days} 天` : row.action_type === 'balance' ? row.balance_amount : '-' }}</template>
        </el-table-column>
        <el-table-column label="备注状态" min-width="160">
          <template #default="{ row }">{{ remarkLabel(row) }}</template>
        </el-table-column>
        <el-table-column label="结果" min-width="220">
          <template #default="{ row }">{{ row.result_reason || '-' }}</template>
        </el-table-column>
      </el-table>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Warning } from '@element-plus/icons-vue'
import AppLayout from '@/components/AppLayout.vue'
import StatusTag from '@/components/StatusTag.vue'
import { createCompensationBatch, listCompensationBatchDetails, listCompensationBatches } from '@/api/admin'
import type { CompensationBatch, CompensationBatchDetail } from '@/api/types'

const loading = ref(false)
const submitting = ref(false)
const batches = ref<CompensationBatch[]>([])
const details = ref<CompensationBatchDetail[]>([])
const selectedBatchId = ref<number | null>(null)
const excludedDomainsText = ref('')
const form = reactive({
  compensate_subscriptions: true,
  compensate_balance: true,
  compensate_non_positive_balance: false,
  subscription_days: 30,
  balance_amount: 10,
  note: '',
})

const currentBatch = computed(() => batches.value.find((item) => item.id === selectedBatchId.value) ?? batches.value[0] ?? null)

async function loadBatches() {
  loading.value = true
  try {
    batches.value = await listCompensationBatches()
    if (!selectedBatchId.value && batches.value.length > 0) {
      selectedBatchId.value = batches.value[0].id
    }
    if (selectedBatchId.value) {
      await loadDetails(selectedBatchId.value)
    } else {
      details.value = []
    }
  } catch (error: any) {
    ElMessage.error(error?.message ?? '加载补偿批次失败')
  } finally {
    loading.value = false
  }
}

async function loadDetails(batchId: number) {
  selectedBatchId.value = batchId
  try {
    details.value = await listCompensationBatchDetails(batchId)
  } catch (error: any) {
    details.value = []
    ElMessage.error(error?.message ?? '加载补偿明细失败')
  }
}

async function submitBatch() {
  submitting.value = true
  try {
    if (!form.compensate_subscriptions && !form.compensate_balance) {
      ElMessage.error('请至少选择一种补偿类型')
      return
    }
    const created = await createCompensationBatch({
      compensate_subscriptions: form.compensate_subscriptions,
      compensate_balance: form.compensate_balance,
      compensate_non_positive_balance: form.compensate_non_positive_balance,
      subscription_days: form.subscription_days,
      balance_amount: form.balance_amount,
      excluded_domains: parseExcludedDomains(excludedDomainsText.value),
      note: form.note.trim(),
    })
    ElMessage.success(`补偿已执行，批次 #${created.id}`)
    await loadBatches()
    await loadDetails(created.id)
  } catch (error: any) {
    ElMessage.error(error?.message ?? '执行补偿失败')
  } finally {
    submitting.value = false
  }
}

function parseExcludedDomains(value: string) {
  const parts = value.split(/[\s,]+/)
  const normalized = parts
    .map((item) => item.trim())
    .filter(Boolean)
    .map((item) => item.replace(/^@/, '').toLowerCase())
  return Array.from(new Set(normalized))
}

function onCurrentBatchChange(batch?: CompensationBatch) {
  if (batch?.id) {
    void loadDetails(batch.id)
  }
}

function formatTime(value?: string | null) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

function decisionLabel(value: string) {
  switch (value) {
    case 'excluded_domain':
      return '域名排除'
    case 'active_subscription':
      return '有效订阅'
    case 'positive_balance':
      return '正余额'
    case 'non_positive_balance':
      return '无订阅且余额<=0'
    default:
      return value || '-'
  }
}

function actionLabel(value: string) {
  switch (value) {
    case 'subscription':
      return '补订阅'
    case 'balance':
      return '补余额'
    case 'skip':
      return '跳过'
    default:
      return value || '-'
  }
}

function remarkLabel(row: CompensationBatchDetail) {
  if (!row.remark_requested) return '未请求'
  if (row.remark_applied) return '已写入'
  if (row.remark_error) return `未写入：${row.remark_error}`
  return '未写入'
}

onMounted(loadBatches)
</script>

<style scoped>
.batch-form {
  display: grid;
  gap: 8px;
}

.batch-form :deep(.el-form-item) {
  margin-bottom: 0;
}
</style>
