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

      <div v-loading="loading" class="rule-cards-container">
        <el-empty v-if="!loading && rules.length === 0" description="暂无辅助调度规则" />

        <div v-for="rule in rules" :key="rule.id" class="rule-card" :class="{ 'rule-card--disabled': !rule.enabled }">
          <!-- Card Header -->
          <div class="rule-card__header">
            <div class="rule-card__title-row">
              <span class="rule-card__name">{{ rule.name }}</span>
              <el-tag :type="statusTagType(rule)" effect="light" size="small">{{ statusLabel(rule) }}</el-tag>
              <el-tag :type="transitionTagType(rule)" effect="plain" size="small">{{ transitionStatusLabel(rule) }}</el-tag>
            </div>
            <div class="rule-card__actions">
              <el-switch :model-value="rule.enabled" size="small" @change="(value: boolean | string | number) => toggleEnabled(rule, value === true)" />
              <el-button text type="primary" :icon="Edit" size="small" @click="openEdit(rule)">编辑</el-button>
              <el-button text type="primary" :icon="Search" size="small" @click="checkRule(rule)">检查</el-button>
              <el-button text type="danger" :icon="Delete" size="small" @click="removeRule(rule)">删除</el-button>
            </div>
          </div>

          <!-- Models -->
          <div class="rule-card__models">
            <span class="rule-card__label">模型集合</span>
            <div v-if="rule.model_names?.length" class="rule-card__model-tags">
              <el-tag v-for="model in rule.model_names" :key="model" size="small" effect="plain" type="info">{{ model }}</el-tag>
            </div>
            <span v-else class="muted">未配置</span>
          </div>

          <!-- Swimlane Visualization -->
          <div class="rule-card__swimlanes">
            <div class="swimlane-header">
              <span class="rule-card__label">泳道</span>
              <span class="muted swimlane-meta">
                期望 {{ rule.expected_open_through_lane ?? 1 }} · 已验证 {{ rule.verified_open_through_lane ?? 1 }} · 上游观测 {{ rule.observed_open_through_lane ?? 1 }}
                · 自动上限 {{ rule.maximum_auto_lane }}
              </span>
            </div>
            <div class="swimlane-track">
              <div
                v-for="(lane, laneIndex) in resolveLanes(rule)"
                :key="laneIndex"
                class="swimlane-card"
                :class="{
                  'swimlane-card--active': laneIndex < (rule.verified_open_through_lane ?? 1),
                  'swimlane-card--pending': laneIndex >= (rule.verified_open_through_lane ?? 1) && laneIndex < (rule.expected_open_through_lane ?? 1),
                }"
              >
                <div class="swimlane-card__number">泳道 {{ laneIndex + 1 }}</div>
                <div class="swimlane-card__accounts">
                  <div v-for="account in lane.accounts" :key="account.id" class="swimlane-account-chip" :class="'swimlane-account-chip--' + (account.status || 'unknown')">
                    <span class="swimlane-account-chip__dot" />
                    <span class="swimlane-account-chip__name">{{ account.name || `#${account.id}` }}</span>
                    <span class="swimlane-account-chip__status">{{ account.schedulable === true ? '可调度' : account.schedulable === false ? '不可调度' : '' }}</span>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <!-- Footer: runtime info -->
          <div class="rule-card__footer">
            <div v-if="rule.missing_models?.length" class="runtime-warning">缺失模型: {{ rule.missing_models.join(' · ') }}</div>
            <div v-if="rule.blocked_reason" class="runtime-warning">{{ rule.blocked_reason }}</div>
            <div v-if="rule.recovery_candidate_lane != null" class="muted">
              可收缩至泳道 {{ rule.recovery_candidate_lane }} · 自 {{ formatTime(rule.recovery_candidate_since) }}
            </div>
            <div v-if="rule.upgrade_evidence && Object.keys(rule.upgrade_evidence).length" class="muted">
              证据: {{ evidenceText(rule.upgrade_evidence) }}
            </div>
            <div class="rule-card__meta">
              <span class="muted">上次检查: {{ formatTime(rule.last_checked_at) }}</span>
              <el-tooltip v-if="rule.last_error" :content="rule.last_error" placement="top">
                <span class="error-text">错误: {{ rule.last_error }}</span>
              </el-tooltip>
            </div>
          </div>
        </div>
      </div>
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

function resolveLanes(rule: AuxSchedulerRule): { number: number; accounts: AuxSchedulerAccountInfo[] }[] {
  if (rule.lane_accounts?.length) {
    return rule.lane_accounts.map((lane) => ({ number: lane.number, accounts: lane.accounts }))
  }
  const lanes = rule.lanes?.length ? rule.lanes : [[]]
  return lanes.map((ids, index) => ({
    number: index + 1,
    accounts: ids.map((id) => ({ id, name: `#${id}` })),
  }))
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

/* Rule Cards */
.rule-cards-container {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-height: 100px;
}

.rule-card {
  border: 1px solid #e5e7eb;
  border-radius: 10px;
  background: #fafbfc;
  padding: 16px 20px;
  transition: box-shadow 0.2s, border-color 0.2s;
}

.rule-card:hover {
  border-color: #c6d0dc;
  box-shadow: 0 2px 8px rgb(0 0 0 / 0.04);
}

.rule-card--disabled {
  opacity: 0.6;
}

.rule-card__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.rule-card__title-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.rule-card__name {
  font-size: 15px;
  font-weight: 600;
  color: #1f2937;
}

.rule-card__actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

.rule-card__models {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
  flex-wrap: wrap;
}

.rule-card__model-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.rule-card__label {
  font-size: 12px;
  font-weight: 600;
  color: #6b7280;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  flex-shrink: 0;
}

/* Swimlane Visualization */
.rule-card__swimlanes {
  margin-bottom: 12px;
}

.swimlane-header {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
}

.swimlane-meta {
  font-size: 12px;
}

.swimlane-track {
  display: flex;
  gap: 10px;
  overflow-x: auto;
  padding: 4px 0;
}

.swimlane-card {
  min-width: 160px;
  max-width: 240px;
  flex: 1 0 160px;
  border: 1.5px solid #e5e7eb;
  border-radius: 8px;
  padding: 10px 12px;
  background: #fff;
  transition: border-color 0.2s, background 0.2s;
}

.swimlane-card--active {
  border-color: #10b981;
  background: #ecfdf5;
}

.swimlane-card--pending {
  border-color: #f59e0b;
  background: #fffbeb;
}

.swimlane-card__number {
  font-size: 11px;
  font-weight: 700;
  color: #6b7280;
  margin-bottom: 6px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.swimlane-card--active .swimlane-card__number {
  color: #059669;
}

.swimlane-card--pending .swimlane-card__number {
  color: #d97706;
}

.swimlane-card__accounts {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.swimlane-account-chip {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 3px 8px;
  border-radius: 4px;
  background: #f3f4f6;
  font-size: 12px;
  line-height: 1.4;
}

.swimlane-account-chip--active {
  background: #d1fae5;
}

.swimlane-account-chip--error,
.swimlane-account-chip--disabled {
  background: #fee2e2;
}

.swimlane-account-chip__dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #9ca3af;
  flex-shrink: 0;
}

.swimlane-account-chip--active .swimlane-account-chip__dot {
  background: #10b981;
}

.swimlane-account-chip--error .swimlane-account-chip__dot,
.swimlane-account-chip--disabled .swimlane-account-chip__dot {
  background: #ef4444;
}

.swimlane-account-chip__name {
  font-weight: 500;
  color: #374151;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.swimlane-account-chip__status {
  color: #6b7280;
  font-size: 11px;
  margin-left: auto;
  white-space: nowrap;
}

/* Footer */
.rule-card__footer {
  border-top: 1px solid #f3f4f6;
  padding-top: 8px;
  line-height: 1.7;
}

.rule-card__meta {
  display: flex;
  align-items: center;
  gap: 16px;
  flex-wrap: wrap;
}

.runtime-warning {
  color: #b45309;
  word-break: break-word;
}

/* Dialog Lane Editor */
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
  .rule-card__header {
    flex-direction: column;
    align-items: flex-start;
  }

  .rule-card__actions {
    width: 100%;
    flex-wrap: wrap;
  }

  .swimlane-track {
    flex-direction: column;
  }

  .swimlane-card {
    max-width: none;
  }

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

  .toolbar-actions {
    justify-content: flex-start;
    width: 100%;
  }

  .form-hint {
    padding-left: 0;
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
</style>
