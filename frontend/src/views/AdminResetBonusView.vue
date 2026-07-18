<template>
  <AppLayout title="重置次数赠送" subtitle="向指定订阅分组发放独立、可到期的额度重置次数">
    <section class="surface section grant-section">
      <div class="section-heading">
        <div>
          <h2>创建赠送批次</h2>
          <p>先预览实际命中的有效订阅，确认后再创建批次。</p>
        </div>
        <el-button :icon="Refresh" :loading="historyLoading" @click="refreshOperationalState">刷新状态</el-button>
      </div>

      <el-form label-position="top" class="grant-form" @submit.prevent>
        <el-form-item label="用户范围" required>
          <el-radio-group v-model="form.target_scope" @change="clearPreview">
            <el-radio-button value="all">所有用户</el-radio-button>
            <el-radio-button value="selected">指定用户</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="form.target_scope === 'selected'" label="指定用户" required>
          <el-select
            v-model="form.selected_user_ids"
            multiple
            filterable
            collapse-tags
            collapse-tags-tooltip
            placeholder="选择一个或多个用户"
            @change="clearPreview"
          >
            <el-option
              v-for="user in users"
              :key="user.upstream_user_id"
              :label="`${user.username || user.email || `用户 ${user.upstream_user_id}`} · ${user.email || `ID ${user.upstream_user_id}`}`"
              :value="user.upstream_user_id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="订阅分组" required>
          <el-select
            v-model="form.group_ids"
            multiple
            collapse-tags
            collapse-tags-tooltip
            placeholder="至少选择一个有限额分组"
            @change="clearPreview"
          >
            <el-option
              v-for="group in finiteGroups"
              :key="group.id"
              :label="group.name"
              :value="group.id"
            />
          </el-select>
          <div class="field-help">无限额分组不会显示在这里，也不能获赠重置次数。</div>
        </el-form-item>
        <el-form-item label="每个订阅赠送次数" required>
          <el-input-number v-model="form.reset_count" :min="1" :max="10000" @change="clearPreview" />
        </el-form-item>
        <el-form-item label="活动备注">
          <el-input
            v-model="form.note"
            type="textarea"
            :rows="3"
            maxlength="200"
            show-word-limit
            placeholder="例如：周年活动赠送"
            @input="clearPreview"
          />
        </el-form-item>
      </el-form>

      <div class="form-actions">
        <el-button type="primary" :icon="View" :loading="previewLoading" @click="runPreview">预览发放范围</el-button>
      </div>

      <div v-if="preview" class="preview-panel" aria-live="polite">
        <div class="preview-summary">
          <div><span>命中用户</span><strong>{{ preview.user_count }}</strong></div>
          <div><span>命中订阅</span><strong>{{ preview.subscription_count }}</strong></div>
          <div><span>每个订阅</span><strong>{{ form.reset_count }} 次</strong></div>
          <div><span>预览有效至</span><strong>{{ formatTime(preview.expires_at) }}</strong></div>
        </div>
        <div v-if="Object.keys(preview.group_counts).length" class="preview-lines">
          <span v-for="(count, groupID) in preview.group_counts" :key="groupID">
            {{ groupName(Number(groupID)) }}：{{ count }} 个订阅
          </span>
        </div>
        <el-alert
          v-if="preview.missing_user_ids.length"
          :title="`未找到用户：${preview.missing_user_ids.join('、')}`"
          type="warning"
          :closable="false"
          show-icon
        />
        <div class="confirm-row">
          <span>确认后批次会在后台逐条核验并发放。</span>
          <el-button
            type="success"
            :icon="Check"
            :loading="createLoading"
            :disabled="preview.subscription_count <= 0"
            @click="createBatch"
          >
            确认创建批次
          </el-button>
        </div>
      </div>
    </section>

    <section class="surface section history-section">
      <div class="section-heading compact">
        <div>
          <h2>赠送批次</h2>
          <p>查看后台处理进度，并展开每个订阅的结果。</p>
        </div>
      </div>
      <el-alert v-if="historyError" :title="historyError" type="error" :closable="false" show-icon />
      <el-table v-loading="historyLoading" :data="batches" empty-text="暂无赠送批次" table-layout="fixed">
        <el-table-column prop="id" label="批次" width="72" />
        <el-table-column label="范围" min-width="170">
          <template #default="{ row }">
            <div class="cell-stack">
              <strong>{{ row.target_scope === 'all' ? '所有用户' : `${row.selected_user_ids.length} 个指定用户` }}</strong>
              <small>{{ row.group_ids.map(groupName).join('、') }}</small>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="赠送" width="88" align="center">
          <template #default="{ row }">{{ row.reset_count }} 次</template>
        </el-table-column>
        <el-table-column label="进度" min-width="190">
          <template #default="{ row }">
            <div class="cell-stack">
              <el-progress :percentage="batchPercentage(row)" :stroke-width="7" />
              <small>成功 {{ row.granted_subscriptions }}，跳过 {{ row.skipped_subscriptions }}，失败 {{ row.failed_subscriptions }}</small>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="96">
          <template #default="{ row }"><el-tag :type="batchTagType(row.status)">{{ batchStatusLabel(row.status) }}</el-tag></template>
        </el-table-column>
        <el-table-column label="创建时间" width="170">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="92">
          <template #default="{ row }">
            <el-button text type="primary" @click="openBatchDetails(row)">查看明细</el-button>
          </template>
        </el-table-column>
      </el-table>
    </section>

    <section class="surface section history-section">
      <div class="section-heading compact">
        <div>
          <h2>订阅延期确认</h2>
          <p>仅处理上游结果未知的延期。确认已应用会同步滚动基础周期和赠送次数有效期。</p>
        </div>
        <el-tag type="warning" effect="plain">待确认 {{ pendingExtensionEvents.length }}</el-tag>
      </div>
      <el-table :data="extensionEvents" empty-text="暂无延期事件" table-layout="fixed">
        <el-table-column prop="id" label="事件" width="72" />
        <el-table-column label="用户 / 订阅" min-width="150">
          <template #default="{ row }">
            <div class="cell-stack"><strong>用户 {{ row.upstream_user_id }}</strong><small>订阅 {{ row.upstream_subscription_id }}</small></div>
          </template>
        </el-table-column>
        <el-table-column label="延期" width="86" align="center">
          <template #default="{ row }">{{ row.extension_days }} 天</template>
        </el-table-column>
        <el-table-column label="到期变化" min-width="220">
          <template #default="{ row }">
            <div class="cell-stack"><span>{{ formatTime(row.before_expires_at) }}</span><small>至 {{ formatTime(row.after_expires_at) }}</small></div>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="110">
          <template #default="{ row }">
            <el-tag :type="extensionTagType(row.status)">{{ extensionEventStatusLabel(row.status, row.resolution) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="190">
          <template #default="{ row }">
            <template v-if="row.status === 'uncertain'">
              <el-button text type="success" @click="resolveExtension(row, 'applied')">确认已应用</el-button>
              <el-button text type="danger" @click="resolveExtension(row, 'released')">释放延期</el-button>
            </template>
            <span v-else class="muted">{{ row.resolution || '-' }}</span>
          </template>
        </el-table-column>
      </el-table>
    </section>

    <el-dialog v-model="detailVisible" :title="`赠送批次 #${selectedBatch?.id ?? ''}`" width="min(1080px, 94vw)" top="5vh">
      <el-table v-loading="detailLoading" :data="batchDetails" max-height="620" empty-text="暂无批次明细" table-layout="fixed">
        <el-table-column prop="upstream_user_id" label="用户" width="90" />
        <el-table-column prop="sub2api_group_id" label="分组" width="90" />
        <el-table-column prop="upstream_subscription_id" label="订阅" width="100" />
        <el-table-column label="订阅有效期" min-width="220">
          <template #default="{ row }">{{ formatTime(row.subscription_starts_at) }} 至 {{ formatTime(row.subscription_expires_at) }}</template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="100" />
        <el-table-column prop="reason" label="原因" min-width="140" show-overflow-tooltip />
        <el-table-column prop="error_message" label="错误" min-width="160" show-overflow-tooltip />
      </el-table>
    </el-dialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { Check, Refresh, View } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import AppLayout from '@/components/AppLayout.vue'
import {
  createSubscriptionResetBonusBatch,
  listSubscriptionExtensionEvents,
  listSubscriptionGroups,
  listSubscriptionResetBonusBatchDetails,
  listSubscriptionResetBonusBatches,
  listUsers,
  previewSubscriptionResetBonus,
  resolveSubscriptionExtensionEvent,
} from '@/api/admin'
import { ApiError } from '@/api/http'
import { extensionEventStatusLabel } from '@/utils/subscription-extension-events'
import type {
  SubscriptionExtensionEvent,
  SubscriptionGroup,
  SubscriptionResetBonusBatch,
  SubscriptionResetBonusBatchDetail,
  SubscriptionResetBonusPreview,
  UserSummary,
} from '@/api/types'

const form = reactive({
  target_scope: 'all' as 'all' | 'selected',
  selected_user_ids: [] as number[],
  group_ids: [] as number[],
  reset_count: 1,
  note: '',
})
const users = ref<UserSummary[]>([])
const groups = ref<SubscriptionGroup[]>([])
const preview = ref<SubscriptionResetBonusPreview | null>(null)
const batches = ref<SubscriptionResetBonusBatch[]>([])
const extensionEvents = ref<SubscriptionExtensionEvent[]>([])
const batchDetails = ref<SubscriptionResetBonusBatchDetail[]>([])
const selectedBatch = ref<SubscriptionResetBonusBatch | null>(null)
const previewLoading = ref(false)
const createLoading = ref(false)
const historyLoading = ref(false)
const historyError = ref('')
const detailLoading = ref(false)
const detailVisible = ref(false)
let pollTimer: number | undefined

const finiteGroups = computed(() => groups.value.filter((group) =>
  Number(group.daily_limit_usd || 0) > 0 || Number(group.weekly_limit_usd || 0) > 0 || Number(group.monthly_limit_usd || 0) > 0,
))
const pendingExtensionEvents = computed(() => extensionEvents.value.filter((event) => event.status === 'uncertain'))

function clearPreview() {
  preview.value = null
}

function validateForm() {
  if (form.target_scope === 'selected' && !form.selected_user_ids.length) return '请至少选择一个用户'
  if (!form.group_ids.length) return '请至少选择一个订阅分组'
  if (!Number.isInteger(form.reset_count) || form.reset_count <= 0) return '赠送次数必须是正整数'
  return ''
}

async function runPreview() {
  const validationError = validateForm()
  if (validationError) {
    ElMessage.warning(validationError)
    return
  }
  previewLoading.value = true
  try {
    preview.value = await previewSubscriptionResetBonus({
      target_scope: form.target_scope,
      selected_user_ids: form.target_scope === 'selected' ? [...form.selected_user_ids] : [],
      group_ids: [...form.group_ids],
      reset_count: form.reset_count,
      note: form.note.trim(),
    })
  } catch (error: any) {
    ElMessage.error(error instanceof ApiError && error.reason ? error.reason : (error?.message ?? '预览失败'))
  } finally {
    previewLoading.value = false
  }
}

async function createBatch() {
  if (!preview.value || createLoading.value) return
  createLoading.value = true
  try {
    const batch = await createSubscriptionResetBonusBatch({ preview_token: preview.value.preview_token })
    ElMessage.success(`赠送批次 #${batch.id} 已创建`)
    preview.value = null
    await refreshOperationalState()
  } catch (error: any) {
    if (error instanceof ApiError && error.reason === 'preview_stale') {
      preview.value = null
      ElMessage.warning('订阅状态已变化，请重新预览')
    } else {
      ElMessage.error(error instanceof ApiError && error.reason ? error.reason : (error?.message ?? '创建批次失败'))
    }
  } finally {
    createLoading.value = false
  }
}

async function refreshOperationalState(options: { silent?: boolean } = {}) {
  if (!options.silent) historyLoading.value = true
  try {
    const [batchData, eventData] = await Promise.all([
      listSubscriptionResetBonusBatches(),
      listSubscriptionExtensionEvents(),
    ])
    batches.value = batchData
    extensionEvents.value = eventData
    historyError.value = ''
  } catch (error: any) {
    historyError.value = error?.message ?? '运营状态加载失败'
  } finally {
    if (!options.silent) historyLoading.value = false
  }
}

async function openBatchDetails(batch: SubscriptionResetBonusBatch) {
  selectedBatch.value = batch
  detailVisible.value = true
  detailLoading.value = true
  try {
    batchDetails.value = await listSubscriptionResetBonusBatchDetails(batch.id)
  } catch (error: any) {
    batchDetails.value = []
    ElMessage.error(error?.message ?? '批次明细加载失败')
  } finally {
    detailLoading.value = false
  }
}

async function resolveExtension(event: SubscriptionExtensionEvent, resolution: 'applied' | 'released') {
  const applied = resolution === 'applied'
  try {
    await ElMessageBox.confirm(
      applied
        ? `确认订阅 #${event.upstream_subscription_id} 已延期 ${event.extension_days} 天？基础周期和既有赠送次数的有效期会同步滚动。`
        : `确认上游未完成订阅 #${event.upstream_subscription_id} 的延期？本地不会滚动任何有效期。`,
      applied ? '确认已应用' : '释放延期',
      { type: applied ? 'warning' : 'error', confirmButtonText: applied ? '确认已应用' : '释放延期', cancelButtonText: '取消' },
    )
    await resolveSubscriptionExtensionEvent(event.id, resolution)
    ElMessage.success(applied ? '延期已确认并完成本地滚动' : '延期事件已释放')
    await refreshOperationalState({ silent: true })
  } catch (error: any) {
    if (error === 'cancel' || error === 'close') return
    ElMessage.error(error instanceof ApiError && error.reason ? error.reason : (error?.message ?? '延期决议失败'))
  }
}

function groupName(groupID: number) {
  return groups.value.find((group) => group.id === groupID)?.name || `分组 ${groupID}`
}

function batchPercentage(batch: SubscriptionResetBonusBatch) {
  if (batch.total_candidates <= 0) return batch.status === 'succeeded' ? 100 : 0
  return Math.min(100, Math.round((batch.processed_candidates / batch.total_candidates) * 100))
}

function batchTagType(status: string) {
  if (status === 'succeeded') return 'success'
  if (status === 'failed') return 'danger'
  if (status === 'running') return 'warning'
  return 'info'
}

function batchStatusLabel(status: string) {
  return { pending: '待处理', running: '处理中', succeeded: '已完成', failed: '失败' }[status] || status
}

function extensionTagType(status: SubscriptionExtensionEvent['status']) {
  if (status === 'succeeded') return 'success'
  if (status === 'failed') return 'danger'
  if (status === 'uncertain') return 'warning'
  return 'info'
}

function formatTime(value?: string | null) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

async function loadInitialData() {
  historyLoading.value = true
  try {
    const [userData, groupData, batchData, eventData] = await Promise.all([
      listUsers(),
      listSubscriptionGroups(),
      listSubscriptionResetBonusBatches(),
      listSubscriptionExtensionEvents(),
    ])
    users.value = userData
    groups.value = groupData
    batches.value = batchData
    extensionEvents.value = eventData
    historyError.value = ''
  } catch (error: any) {
    historyError.value = error?.message ?? '页面数据加载失败'
  } finally {
    historyLoading.value = false
  }
}

onMounted(() => {
  void loadInitialData()
  pollTimer = window.setInterval(() => void refreshOperationalState({ silent: true }), 15000)
})

onBeforeUnmount(() => {
  if (pollTimer !== undefined) window.clearInterval(pollTimer)
})
</script>

<style scoped>
.grant-section,
.history-section {
  display: grid;
  gap: 16px;
  margin-bottom: 16px;
}

.section-heading,
.confirm-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.section-heading h2,
.section-heading p {
  margin: 0;
  letter-spacing: 0;
}

.section-heading h2 {
  font-size: 18px;
}

.section-heading p,
.field-help,
.confirm-row,
.cell-stack small {
  color: #64748b;
  font-size: 12px;
}

.section-heading p {
  margin-top: 4px;
}

.grant-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 20px;
}

.grant-form :deep(.el-select),
.grant-form :deep(.el-textarea) {
  width: 100%;
}

.field-help {
  width: 100%;
  margin-top: 5px;
}

.form-actions {
  display: flex;
  justify-content: flex-end;
}

.preview-panel {
  display: grid;
  gap: 12px;
  padding: 14px;
  border: 1px solid #cbd8e7;
  border-radius: 8px;
  background: #f8fafc;
}

.preview-summary {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

.preview-summary > div,
.cell-stack {
  min-width: 0;
  display: grid;
  gap: 3px;
}

.preview-summary span {
  color: #64748b;
  font-size: 12px;
}

.preview-summary strong {
  overflow-wrap: anywhere;
}

.preview-lines {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 16px;
  color: #315f8a;
  font-size: 13px;
}

.cell-stack span,
.cell-stack strong,
.cell-stack small {
  overflow-wrap: anywhere;
}

@media (max-width: 760px) {
  .section-heading,
  .confirm-row {
    align-items: stretch;
    flex-direction: column;
  }

  .grant-form,
  .preview-summary {
    grid-template-columns: 1fr;
  }

  .form-actions :deep(.el-button),
  .confirm-row :deep(.el-button) {
    width: 100%;
  }
}
</style>
