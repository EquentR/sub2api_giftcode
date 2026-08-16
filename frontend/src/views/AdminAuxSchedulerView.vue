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
            <el-tag :type="statusTagType(row)" effect="light">
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
        <el-table-column label="期望 / 观测" min-width="220">
          <template #default="{ row }">
            <div class="runtime-prefix">
              <div>期望泳道 {{ row.expected_open_through_lane ?? 1 }} · 已验证 {{ row.verified_open_through_lane ?? 1 }}</div>
              <div class="muted">上游观测 {{ row.observed_open_through_lane ?? 1 }}</div>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="运行状态" min-width="260">
          <template #default="{ row }">
            <div class="runtime-state">
              <el-tag :type="transitionTagType(row)" effect="light" size="small">{{ transitionStatusLabel(row) }}</el-tag>
              <div v-if="row.missing_models?.length" class="runtime-warning">缺失模型: {{ row.missing_models.join(' · ') }}</div>
              <div v-if="row.blocked_reason" class="runtime-warning">{{ row.blocked_reason }}</div>
              <div v-if="row.recovery_candidate_lane != null" class="muted">
                可收缩至泳道 {{ row.recovery_candidate_lane }} · 自 {{ formatTime(row.recovery_candidate_since) }}
              </div>
              <div v-if="row.upgrade_evidence && Object.keys(row.upgrade_evidence).length" class="muted">
                证据: {{ evidenceText(row.upgrade_evidence) }}
              </div>
            </div>
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

    <el-dialog v-model="dialogVisible" :title="editing ? '编辑规则' : '新建规则'" width="min(640px, calc(100vw - 24px))" class="aux-rule-dialog">
      <el-form label-width="90px" @submit.prevent>
        <el-form-item label="规则名称" required>
          <el-input v-model="form.name" maxlength="100" placeholder="例如：主力 OAuth 冷却备援" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="form.enabled" />
        </el-form-item>
        <el-form-item label="模型集合" required>
          <el-select
            v-model="form.model_names"
            multiple
            filterable
            allow-create
            default-first-option
            collapse-tags
            placeholder="选择本规则需要保护的模型"
            style="width: 100%"
          >
            <el-option v-for="model in modelOptions" :key="model" :label="model" :value="model" />
          </el-select>
        </el-form-item>
        <el-form-item label="自动泳道上限" required>
          <el-input-number v-model="form.maximum_auto_lane" :min="1" :max="Math.max(2, form.lanes.length)" />
        </el-form-item>
        <el-form-item label="泳道列表" required>
          <div class="lane-editor">
            <div v-for="(lane, index) in form.lanes" :key="index" class="lane-row">
              <div class="lane-index">泳道 {{ index + 1 }}</div>
              <el-select
                v-model="form.lanes[index]"
                multiple
                filterable
                collapse-tags
                placeholder="选择该泳道账号"
                style="width: 100%"
              >
                <el-option v-for="account in accountOptions" :key="account.id" :label="accountLabel(account)" :value="account.id" />
              </el-select>
              <el-button text type="primary" :disabled="index === 0" @click="moveLaneUp(index)">上移</el-button>
              <el-button text type="primary" :disabled="index === form.lanes.length - 1" @click="moveLaneDown(index)">下移</el-button>
              <el-button text type="danger" @click="removeLane(index)">删除泳道</el-button>
            </div>
            <el-button type="primary" plain :icon="Plus" @click="addLane">添加泳道</el-button>
          </div>
        </el-form-item>
        <div class="muted form-hint">
          泳道按成本从低到高排列，开启时从泳道 1 逐级累积；累积开启不等于严格成本隔离，Sub2API 仍可能在已开启泳道间路由流量。
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
  model_names: [] as string[],
  lanes: [[]] as number[][],
  maximum_auto_lane: 2,
})

const accountOptions = computed(() => {
  const type = (value: string) => value?.toLowerCase?.() ?? ''
  return accounts.value.filter((account) => type(account.type) === 'oauth' || type(account.type) === 'apikey')
})

const modelOptions = computed(() => {
  const seen = new Map<string, string>()
  for (const account of accounts.value) {
    const credentials = account.credentials ?? {}
    const mapping = credentials.model_mapping
    if (mapping && typeof mapping === 'object') {
      for (const model of Object.keys(mapping)) {
        if (model && !seen.has(model)) seen.set(model, model)
      }
    }
    const supported = credentials.upstream_supported_models
    if (Array.isArray(supported)) {
      for (const model of supported) {
        if (typeof model === 'string' && model && !seen.has(model)) seen.set(model, model)
      }
    }
  }
  return [...seen.values()].sort()
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
  form.model_names = []
  form.lanes = [[]]
  form.maximum_auto_lane = 2
  dialogVisible.value = true
}

function openEdit(rule: AuxSchedulerRule) {
  editing.value = rule
  form.name = rule.name
  form.enabled = rule.enabled
  form.model_names = [...(rule.model_names ?? [])]
  const lanes = rule.lanes?.length ? rule.lanes : [[]]
  form.lanes = lanes.map((lane) => [...lane])
  form.maximum_auto_lane = rule.maximum_auto_lane || lanes.length
  dialogVisible.value = true
}

function addLane() {
  form.lanes.push([])
}

function removeLane(index: number) {
  if (form.lanes.length <= 2) {
    ElMessage.warning('至少需要两个泳道')
    return
  }
  form.lanes.splice(index, 1)
  if (form.maximum_auto_lane > form.lanes.length) form.maximum_auto_lane = form.lanes.length
}

function moveLaneUp(index: number) {
  if (index <= 0) return
  const lanes = form.lanes
  ;[lanes[index - 1], lanes[index]] = [lanes[index], lanes[index - 1]]
}

function moveLaneDown(index: number) {
  if (index >= form.lanes.length - 1) return
  const lanes = form.lanes
  ;[lanes[index], lanes[index + 1]] = [lanes[index + 1], lanes[index]]
}

async function save() {
  if (!form.name.trim()) {
    ElMessage.warning('请填写规则名称')
    return
  }
  if (form.model_names.length === 0) {
    ElMessage.warning('请选择至少一个模型')
    return
  }
  if (form.lanes.length < 2) {
    ElMessage.warning('至少需要两个泳道')
    return
  }
  if (form.lanes.some((lane) => lane.length === 0)) {
    ElMessage.warning('每个泳道至少需要一个账号')
    return
  }
  const flat = form.lanes.flat()
  if (new Set(flat).size !== flat.length) {
    ElMessage.warning('同一账号不能在多个泳道中重复')
    return
  }
  if (form.maximum_auto_lane < 1 || form.maximum_auto_lane > form.lanes.length) {
    ElMessage.warning('自动泳道上限必须在有效范围内')
    return
  }
  saving.value = true
  try {
    const payload = {
      name: form.name.trim(),
      enabled: form.enabled,
      model_names: form.model_names,
      lanes: form.lanes.map((lane) => [...lane]),
      maximum_auto_lane: form.maximum_auto_lane,
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
      model_names: [...(rule.model_names ?? [])],
      lanes: (rule.lanes?.length ? rule.lanes : [[]]).map((lane) => [...lane]),
      maximum_auto_lane: rule.maximum_auto_lane || (rule.lanes?.length ?? 2),
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
    return infos.map(observedAccountLabel).join(' · ')
  }
  return ids.map((id) => `#${id}`).join(' · ')
}

function statusTagType(rule: AuxSchedulerRule) {
  if (rule.migration_status === 'needs_migration') return 'warning'
  if (!rule.enabled) return 'info'
  if (rule.model_names?.length) return transitionTagType(rule)
  return rule.state === 'backup_active' ? 'warning' : 'success'
}

function statusLabel(rule: AuxSchedulerRule) {
  if (rule.migration_status === 'needs_migration') return '需要迁移'
  if (!rule.enabled) return '已禁用'
  if (rule.transition_status === 'blocked') return '受阻塞'
  return rule.state === 'backup_active' ? '备用启用中' : transitionStatusLabel(rule)
}

function transitionStatusLabel(rule: AuxSchedulerRule) {
  switch (rule.transition_status) {
    case 'stable':
      return '稳定'
    case 'applying':
      return '应用中'
    case 'uncertain':
      return '不确定'
    case 'failed':
      return '失败'
    case 'blocked':
      return '阻塞'
    default:
      return rule.transition_status || '稳定'
  }
}

function transitionTagType(rule: AuxSchedulerRule) {
  switch (rule.transition_status) {
    case 'applying':
      return 'primary'
    case 'uncertain':
    case 'failed':
      return 'danger'
    case 'blocked':
      return 'warning'
    default:
      return 'success'
  }
}

function evidenceText(evidence: Record<string, unknown>) {
  return Object.entries(evidence)
    .map(([key, value]) => `${key}: ${value}`)
    .join(' · ')
}

function laneText(rule: AuxSchedulerRule) {
  if (rule.lane_accounts?.length) {
    return rule.lane_accounts
      .map((lane) => `泳道 ${lane.number}: ${lane.accounts.map(observedAccountLabel).join(' · ')}`)
      .join(' / ')
  }
  const lanes = rule.lanes?.length ? rule.lanes : [[]]
  return lanes
    .map((ids, index) => `泳道 ${index + 1}: ${ids.map((id) => `#${id}`).join(' · ')}`)
    .join(' / ')
}

function observedAccountLabel(item: AuxSchedulerAccountInfo) {
  const status = item.status || 'unknown'
  const scheduling = item.schedulable === true ? '可调度' : item.schedulable === false ? '不可调度' : '未观测'
  const label = item.name || `#${item.id}`
  return `${label} (${status} · ${scheduling})`
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

.runtime-prefix,
.runtime-state {
  line-height: 1.7;
  color: #374151;
}

.runtime-warning {
  color: #b45309;
  word-break: break-word;
}

.lane-editor {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.aux-rule-dialog {
  max-height: calc(100vh - 48px);
  overflow: auto;
}

.lane-row {
  display: grid;
  grid-template-columns: 76px minmax(0, 1fr) auto auto auto;
  align-items: center;
  gap: 6px;
  width: 100%;
}

.lane-index {
  color: #374151;
  font-weight: 600;
  white-space: nowrap;
}

@media (max-width: 760px) {
  .lane-row {
    grid-template-columns: minmax(0, 1fr);
    row-gap: 4px;
    padding: 8px;
    border: 1px solid #e5e7eb;
    border-radius: 6px;
  }

  .lane-editor > .el-button {
    align-self: flex-start;
  }
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
