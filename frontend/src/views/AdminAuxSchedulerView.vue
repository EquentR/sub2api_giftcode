<template>
  <AppLayout title="辅助调度器" subtitle="主力账号临时不可调度时自动启用备用账号">
    <div class="surface section">
      <div class="toolbar aux-toolbar">
        <div>
          <div style="font-weight: 700">调度规则</div>
          <div class="muted">{{ rules.length }} 条规则 · 定时扫描间隔在服务配置中设置</div>
        </div>
        <div class="toolbar-actions">
          <el-button type="primary" :icon="Plus" @click="openCreate">新建规则</el-button>
          <el-button :icon="Refresh" :loading="loading" @click="loadAll">刷新</el-button>
        </div>
      </div>

      <el-alert
        v-if="rules.some((rule) => rule.upstream_error)"
        class="aux-alert"
        type="warning"
        :closable="false"
        title="上游账号列表加载失败，部分账号名称可能未显示"
      />

      <el-alert
        v-if="rules.some((rule) => rule.migration_status === 'needs_migration')"
        class="aux-alert"
        type="warning"
        :closable="false"
        title="存在旧版主/备用规则：请先选择模型集合并保存有效配置，否则不会自动调度"
      />

      <el-table
        v-loading="loading"
        :data="rules"
        stripe
        size="small"
        empty-text="暂无辅助调度规则"
        style="width: 100%"
      >
        <el-table-column prop="name" label="规则名称" min-width="160" />
        <el-table-column label="状态" width="110">
          <template #default="{ row }">
            <el-tag
              :type="row.migration_status === 'needs_migration' ? 'warning' : row.state === 'backup_active' ? 'warning' : 'success'"
              effect="light"
            >
              {{ statusLabel(row) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="模型 / 泳道" min-width="260">
          <template #default="{ row }">
            <div class="model-lane-text">
              <div v-if="row.model_names?.length" class="muted">{{ row.model_names.join(' · ') }}</div>
              <div v-else class="muted">未配置模型集合</div>
              <div class="account-tags">{{ laneText(row) }}</div>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="主力账号" min-width="220">
          <template #default="{ row }">
            <div class="account-tags">{{ accountText(row.primary_accounts, row.primary_account_ids) }}</div>
          </template>
        </el-table-column>
        <el-table-column label="备用账号" min-width="220">
          <template #default="{ row }">
            <div class="account-tags">{{ accountText(row.backup_accounts, row.backup_account_ids) }}</div>
          </template>
        </el-table-column>
        <el-table-column label="启用" width="80">
          <template #default="{ row }">
            <el-switch :model-value="row.enabled" @change="(value: boolean | string | number) => toggleEnabled(row, value === true)" />
          </template>
        </el-table-column>
        <el-table-column label="上次检查" min-width="160">
          <template #default="{ row }">{{ formatTime(row.last_checked_at) }}</template>
        </el-table-column>
        <el-table-column label="最近错误" min-width="180">
          <template #default="{ row }">
            <el-tooltip v-if="row.last_error" :content="row.last_error" placement="top">
              <span class="error-text">{{ row.last_error }}</span>
            </el-tooltip>
            <span v-else class="muted">-</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="200" fixed="right">
          <template #default="{ row }">
            <el-button text type="primary" :icon="Edit" @click="openEdit(row)">编辑</el-button>
            <el-button text type="primary" :icon="Search" @click="checkRule(row)">检查</el-button>
            <el-button text type="danger" :icon="Delete" @click="removeRule(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog v-model="dialogVisible" :title="editing ? '编辑规则' : '新建规则'" width="640px">
      <el-form label-width="90px" @submit.prevent>
        <el-form-item label="规则名称" required>
          <el-input v-model="form.name" maxlength="100" placeholder="例如：主力 OAuth 冷却备援" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="form.enabled" />
        </el-form-item>
        <el-form-item label="主力账号" required>
          <el-select
            v-model="form.primary_account_ids"
            multiple
            filterable
            collapse-tags
            placeholder="选择主力 OpenAI 账号"
            style="width: 100%"
          >
            <el-option v-for="account in backupOptions" :key="account.id" :label="accountLabel(account)" :value="account.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="备用账号" required>
          <el-select
            v-model="form.backup_account_ids"
            multiple
            filterable
            collapse-tags
            placeholder="选择备用 OpenAI 账号"
            style="width: 100%"
          >
            <el-option v-for="account in accountOptions" :key="account.id" :label="accountLabel(account)" :value="account.id" />
          </el-select>
        </el-form-item>
        <div class="muted form-hint">
          任一主力账号出现临时不可调度或模型冷却时，该规则内所有备用账号会启用调度；备用账号为 active 或 error 时可选，error 不会启用并会在错误信息列提示。备用账号不能被其他规则复用。
        </div>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Delete, Edit, Plus, Refresh, Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import AppLayout from '@/components/AppLayout.vue'
import {
  checkAuxSchedulerRule,
  createAuxSchedulerRule,
  deleteAuxSchedulerRule,
  listAuxSchedulerRules,
  listOpenAIAccounts,
  updateAuxSchedulerRule,
} from '@/api/admin'
import type { AuxSchedulerAccountInfo, AuxSchedulerRule, OpenAIAccount } from '@/api/types'

const rules = ref<AuxSchedulerRule[]>([])
const accounts = ref<OpenAIAccount[]>([])
const loading = ref(false)
const saving = ref(false)
const dialogVisible = ref(false)
const editing = ref<AuxSchedulerRule | null>(null)
const form = reactive({
  name: '',
  enabled: true,
  primary_account_ids: [] as number[],
  backup_account_ids: [] as number[],
})

const accountOptions = computed(() => {
  const type = (value: string) => value?.toLowerCase?.() ?? ''
  return accounts.value.filter((account) => type(account.type) === 'oauth' || type(account.type) === 'apikey')
})

const backupOptions = computed(() => {
  const status = (value: string) => value?.toLowerCase?.() ?? ''
  return accountOptions.value.filter((account) => status(account.status) === 'active' || status(account.status) === 'error')
})

async function loadAll() {
  loading.value = true
  try {
    rules.value = await listAuxSchedulerRules()
  } catch (error: any) {
    ElMessage.error(error?.message ?? '加载辅助调度规则失败')
    return
  } finally {
    loading.value = false
  }
  try {
    accounts.value = await listOpenAIAccounts()
  } catch {
    accounts.value = []
  }
}

function openCreate() {
  editing.value = null
  form.name = ''
  form.enabled = true
  form.primary_account_ids = []
  form.backup_account_ids = []
  dialogVisible.value = true
}

function openEdit(rule: AuxSchedulerRule) {
  editing.value = rule
  form.name = rule.name
  form.enabled = rule.enabled
  form.primary_account_ids = [...rule.primary_account_ids]
  form.backup_account_ids = [...rule.backup_account_ids]
  dialogVisible.value = true
}

async function save() {
  if (!form.name.trim()) {
    ElMessage.warning('请填写规则名称')
    return
  }
  if (form.primary_account_ids.length === 0 || form.backup_account_ids.length === 0) {
    ElMessage.warning('主力账号和备用账号都不能为空')
    return
  }
  if (form.primary_account_ids.some((id) => form.backup_account_ids.includes(id))) {
    ElMessage.warning('同一账号不能同时作为主力与备用')
    return
  }
  saving.value = true
  try {
    const payload = {
      name: form.name.trim(),
      enabled: form.enabled,
      primary_account_ids: form.primary_account_ids,
      backup_account_ids: form.backup_account_ids,
    }
    if (editing.value) {
      await updateAuxSchedulerRule(editing.value.id, payload)
    } else {
      await createAuxSchedulerRule(payload)
    }
    ElMessage.success('已保存')
    dialogVisible.value = false
    await loadAll()
  } catch (error: any) {
    ElMessage.error(error?.message ?? '保存失败')
  } finally {
    saving.value = false
  }
}

async function toggleEnabled(rule: AuxSchedulerRule, enabled: boolean) {
  try {
    await updateAuxSchedulerRule(rule.id, {
      name: rule.name,
      enabled,
      primary_account_ids: rule.primary_account_ids,
      backup_account_ids: rule.backup_account_ids,
    })
    ElMessage.success(enabled ? '规则已启用' : '规则已停用')
    await loadAll()
  } catch (error: any) {
    ElMessage.error(error?.message ?? '切换失败')
    await loadAll()
  }
}

async function checkRule(rule: AuxSchedulerRule) {
  try {
    await checkAuxSchedulerRule(rule.id)
    ElMessage.success('检查完成')
    await loadAll()
  } catch (error: any) {
    ElMessage.error(error?.message ?? '检查失败')
    await loadAll()
  }
}

async function removeRule(rule: AuxSchedulerRule) {
  try {
    await ElMessageBox.confirm(`确认删除规则「${rule.name}」？`, '删除确认', { type: 'warning' })
  } catch {
    return
  }
  try {
    await deleteAuxSchedulerRule(rule.id)
    ElMessage.success('已删除')
    await loadAll()
  } catch (error: any) {
    ElMessage.error(error?.message ?? '删除失败')
  }
}

function accountText(infos: AuxSchedulerAccountInfo[], ids: number[]) {
  if (infos.length > 0) {
    return infos.map((item) => item.name || `#${item.id}`).join(' · ')
  }
  return ids.map((id) => `#${id}`).join(' · ')
}

function statusLabel(rule: AuxSchedulerRule) {
  if (rule.migration_status === 'needs_migration') return '需要迁移'
  if (!rule.enabled) return '已禁用'
  return rule.state === 'backup_active' ? '备用启用中' : '正常'
}

function laneText(rule: AuxSchedulerRule) {
  if (rule.lane_accounts?.length) {
    return rule.lane_accounts
      .map((lane) => `泳道 ${lane.number}: ${lane.accounts.map((item) => item.name || `#${item.id}`).join(' · ')}`)
      .join(' / ')
  }
  const lanes = rule.lanes?.length ? rule.lanes : [rule.primary_account_ids, rule.backup_account_ids]
  return lanes
    .map((ids, index) => `泳道 ${index + 1}: ${ids.map((id) => `#${id}`).join(' · ')}`)
    .join(' / ')
}

function accountLabel(account: OpenAIAccount) {
  const type = account.type?.toLowerCase?.() ?? ''
  const typeLabel = type === 'oauth' ? 'OAuth' : type === 'apikey' ? 'API Key' : account.type
  const statusLabel = account.status || 'unknown'
  return `${account.name} (#${account.id} · ${typeLabel} · ${statusLabel})`
}

function formatTime(value?: string | null) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

onMounted(loadAll)
</script>

<style scoped>
.aux-toolbar {
  align-items: flex-start;
}

.toolbar-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 8px;
}

.aux-alert {
  margin-bottom: 12px;
}

.account-tags {
  color: #374151;
  line-height: 1.7;
  word-break: break-all;
}

.model-lane-text {
  line-height: 1.7;
}

.error-text {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  color: #b91c1c;
  text-overflow: ellipsis;
  vertical-align: middle;
  white-space: nowrap;
}

.form-hint {
  margin-top: -6px;
  padding-left: 90px;
}

@media (max-width: 760px) {
  .toolbar-actions {
    justify-content: flex-start;
    width: 100%;
  }

  .form-hint {
    padding-left: 0;
  }
}
</style>
