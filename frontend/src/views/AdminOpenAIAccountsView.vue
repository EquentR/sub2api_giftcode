<template>
  <AppLayout title="OpenAI UA" subtitle="固定上游账号的 User-Agent">
    <div class="surface section">
      <div class="toolbar openai-toolbar">
        <div>
          <div style="font-weight: 700">OpenAI 账号</div>
          <div class="muted">{{ rows.length }} 个账号 · {{ selectedCount }} 个已选中 · {{ dirtyCount }} 个待保存</div>
        </div>
        <div class="toolbar-actions">
          <el-input v-model="batchUserAgent" class="batch-input" clearable placeholder="固定 User-Agent">
            <template #prefix>
              <el-icon><Monitor /></el-icon>
            </template>
          </el-input>
          <el-button :icon="CopyDocument" :disabled="selectedCount === 0" @click="applyBatch">填入选中</el-button>
          <el-button type="primary" :icon="Check" :disabled="selectedCount === 0" :loading="savingBatch" @click="saveSelected">保存选中</el-button>
          <el-button :icon="Refresh" :loading="loading" @click="loadAll">刷新</el-button>
        </div>
      </div>

      <el-table
        v-loading="loading"
        :data="rows"
        stripe
        size="small"
        empty-text="暂无 OpenAI 账号"
        style="width: 100%"
        @selection-change="onSelectionChange"
      >
        <el-table-column type="selection" width="48" />
        <el-table-column prop="name" label="账号" min-width="180" />
        <el-table-column prop="type" label="类型" width="120">
          <template #default="{ row }">{{ typeLabel(row.type) }}</template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="120">
          <template #default="{ row }"><StatusTag :status="row.status" /></template>
        </el-table-column>
        <el-table-column label="当前 UA" min-width="260">
          <template #default="{ row }">
            <span class="ua-text">{{ row.original_user_agent || '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="设置 UA" min-width="320">
          <template #default="{ row }">
            <el-input v-model="row.draft_user_agent" clearable placeholder="留空则清空固定 UA" />
          </template>
        </el-table-column>
        <el-table-column prop="updated_at" label="更新时间" min-width="180">
          <template #default="{ row }">{{ formatTime(row.updated_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="170" fixed="right">
          <template #default="{ row }">
            <el-button text type="primary" :loading="row.saving" :disabled="!isDirty(row)" @click="saveRow(row)">保存</el-button>
            <el-button text :icon="Close" :disabled="!isDirty(row)" @click="resetRow(row)">重置</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Check, Close, CopyDocument, Monitor, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import AppLayout from '@/components/AppLayout.vue'
import StatusTag from '@/components/StatusTag.vue'
import { listOpenAIAccounts, updateOpenAIAccountUserAgent } from '@/api/admin'
import type { OpenAIAccount } from '@/api/types'

type AccountRow = OpenAIAccount & {
  original_user_agent: string
  draft_user_agent: string
  saving: boolean
}

const rows = ref<AccountRow[]>([])
const selectedRows = ref<AccountRow[]>([])
const loading = ref(false)
const savingBatch = ref(false)
const batchUserAgent = ref('')

const dirtyCount = computed(() => rows.value.filter((row) => isDirty(row)).length)
const selectedCount = computed(() => selectedRows.value.length)

function credentialUserAgent(account: OpenAIAccount) {
  const value = account.credentials?.user_agent
  return typeof value === 'string' ? value : ''
}

function toRow(account: OpenAIAccount): AccountRow {
  const userAgent = credentialUserAgent(account)
  return {
    ...account,
    original_user_agent: userAgent,
    draft_user_agent: userAgent,
    saving: false,
  }
}

async function loadAll() {
  loading.value = true
  try {
    rows.value = (await listOpenAIAccounts()).map(toRow)
  } catch (error: any) {
    ElMessage.error(error?.message ?? '加载 OpenAI 账号失败')
  } finally {
    loading.value = false
  }
}

function isDirty(row: AccountRow) {
  return row.draft_user_agent.trim() !== row.original_user_agent.trim()
}

function applyBatch() {
  if (selectedRows.value.length === 0) {
    ElMessage.info('请先选择要修改的账号')
    return
  }
  const value = batchUserAgent.value.trim()
  selectedRows.value.forEach((row) => {
    row.draft_user_agent = value
  })
}

function resetRow(row: AccountRow) {
  row.draft_user_agent = row.original_user_agent
}

async function saveRow(row: AccountRow, showMessage = true) {
  row.saving = true
  try {
    const updated = await updateOpenAIAccountUserAgent(row.id, row.draft_user_agent.trim())
    Object.assign(row, toRow(updated))
    if (showMessage) {
      ElMessage.success('已保存')
    }
  } catch (error: any) {
    ElMessage.error(error?.message ?? '保存失败')
    throw error
  } finally {
    row.saving = false
  }
}

async function saveSelected() {
  if (selectedRows.value.length === 0) {
    ElMessage.info('请先选择要保存的账号')
    return
  }
  const targets = selectedRows.value.filter((row) => isDirty(row))
  if (targets.length === 0) {
    ElMessage.info('选中账号没有待保存的修改')
    return
  }
  savingBatch.value = true
  try {
    for (const row of targets) {
      await saveRow(row, false)
    }
    ElMessage.success(`已保存 ${targets.length} 个账号`)
  } finally {
    savingBatch.value = false
  }
}

function onSelectionChange(selection: AccountRow[]) {
  selectedRows.value = selection
}

function typeLabel(value: string) {
  const key = value?.toLowerCase?.() ?? ''
  const map: Record<string, string> = {
    api_key: 'API Key',
    oauth: 'OAuth',
    session: 'Session',
  }
  return map[key] ?? value ?? '-'
}

function formatTime(value?: string | null) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

onMounted(loadAll)
</script>

<style scoped>
.openai-toolbar {
  align-items: flex-start;
}

.toolbar-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 8px;
}

.batch-input {
  width: min(420px, 100%);
}

.ua-text {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  color: #374151;
  text-overflow: ellipsis;
  vertical-align: middle;
  white-space: nowrap;
}

@media (max-width: 760px) {
  .toolbar-actions {
    justify-content: flex-start;
    width: 100%;
  }

  .batch-input {
    width: 100%;
  }
}
</style>
