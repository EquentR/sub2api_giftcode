<template>
  <AppLayout title="兑换申请" subtitle="选择余额或订阅档位，审批通过后自动下发兑换码">
    <div class="recharge-page">
      <section class="recharge-hero">
        <div>
          <div class="eyebrow">Recharge</div>
          <h1>兑换申请</h1>
          <p>选择合适的兑换档位，提交后由管理员审批并发放兑换码。提交审批后请主动联系管理员付款！</p>
        </div>
        <div class="wallet-card">
          <span>本次预计获得</span>
          <strong>{{ selectedTier ? selectedTierHeadline : '-' }}</strong>
          <small>{{ selectedTier ? `实付 ${formatCny(selectedTier.pay_amount_cny)}` : '请选择兑换档位' }}</small>
        </div>
      </section>

      <div class="recharge-layout">
        <section class="tier-section">
          <div class="section-heading">
            <div>
              <h2>选择兑换档位</h2>
              <p>订阅档位的分组和限额来自 sub2api 实时数据。</p>
            </div>
            <el-button :icon="Refresh" :loading="loadingData" @click="() => loadAll()">刷新</el-button>
          </div>

          <div v-loading="loadingCore" class="tier-grid">
            <button
              v-for="tier in enabledTiers"
              :key="tier.id"
              class="tier-card"
              :class="{
                active: form.tierId === tier.id,
                disabled: !tierSelectable(tier),
                subscription: tierCodeType(tier) === 'subscription',
              }"
              :disabled="!tierSelectable(tier)"
              type="button"
              @click="selectTier(tier)"
            >
              <span class="tier-kind">{{ formatCodeTypeLabel(tier.code_type) }}</span>
              <span class="tier-name">{{ tierDisplayName(tier) }}</span>
              <strong>{{ formatCny(tier.pay_amount_cny) }}</strong>
              <span v-if="tierCodeType(tier) === 'balance'">到账 {{ formatMoney(tier.amount) }} 美元</span>
              <template v-else>
                <span>{{ tierGroupLabel(tier) }} · {{ validityDaysText(tier) }}</span>
                <small>{{ formatLimitTriplet(tier) }}</small>
                <small v-if="!tierSelectable(tier)" class="tier-error">{{ tier.upstream_error || '订阅分组不可用' }}</small>
              </template>
            </button>
            <div v-if="!enabledTiers.length && !loadingCore" class="empty-state">暂无可选兑换档位</div>
          </div>

          <div class="status-panel">
            <div class="section-heading compact">
              <div>
                <h2>申请状态</h2>
                <p>最近的申请和发码结果会显示在这里。</p>
              </div>
            </div>
            <div v-if="accessRequestRows.length" class="request-list">
              <div v-for="row in accessRequestRows" :key="row.request.id" class="request-item">
                <div class="request-main">
                  <div>
                    <strong>{{ requestTierName(row.request) }}</strong>
                    <span>{{ requestSummary(row.request) }}</span>
                    <small v-if="row.request.code_type === 'subscription'">
                      {{ requestLimitSummary(row.request) }}
                    </small>
                    <small>{{ formatTime(row.request.created_at) }}</small>
                  </div>
                  <StatusTag :status="row.request.status" />
                </div>

                <div v-if="row.codes.length" class="request-codes">
                  <div
                    v-for="code in row.codes"
                    :key="code.id"
                    class="code-item"
                    :class="{ used: isCodeUsed(code) }"
                  >
                    <div class="code-meta">
                      <code>{{ code.code }}</code>
                      <span>{{ formatCodeValue(code) }} · {{ formatTime(code.created_at) }}</span>
                    </div>
                    <div class="code-actions">
                      <StatusTag :status="code.status" />
                      <el-button text :icon="CopyDocument" @click="copyCode(code.code)">复制</el-button>
                    </div>
                  </div>
                </div>
                <div v-else class="request-code-empty">{{ codeEmptyText(row.request.status) }}</div>
              </div>
            </div>
            <div v-else class="empty-state small">还没有提交过充值申请</div>

            <div v-if="unlinkedCodes.length" class="request-list legacy-codes">
              <div class="request-item">
                <div class="request-main">
                  <div>
                    <strong>历史兑换码</strong>
                    <span>未找到关联申请的兑换码</span>
                  </div>
                  <StatusTag status="issued" />
                </div>
                <div class="request-codes">
                  <div
                    v-for="code in unlinkedCodes"
                    :key="code.id"
                    class="code-item"
                    :class="{ used: isCodeUsed(code) }"
                  >
                    <div class="code-meta">
                      <code>{{ code.code }}</code>
                      <span>{{ formatCodeValue(code) }} · {{ formatTime(code.created_at) }}</span>
                    </div>
                    <div class="code-actions">
                      <StatusTag :status="code.status" />
                      <el-button text :icon="CopyDocument" @click="copyCode(code.code)">复制</el-button>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </section>

        <aside class="submit-panel">
          <div class="summary-card">
            <h2>申请确认</h2>
            <div class="summary-line">
              <span>当前选择</span>
              <strong>{{ selectedTierName }}</strong>
            </div>
            <div class="summary-line">
              <span>兑换类型</span>
              <strong>{{ selectedTier ? formatCodeTypeLabel(selectedTier.code_type) : '-' }}</strong>
            </div>
            <div class="summary-line">
              <span>{{ selectedTier && tierCodeType(selectedTier) === 'subscription' ? '订阅内容' : '到账金额' }}</span>
              <strong>{{ selectedTier ? selectedTierHeadline : '-' }}</strong>
            </div>
            <div class="summary-line">
              <span>实付金额</span>
              <strong>{{ selectedTier ? formatCny(selectedTier.pay_amount_cny) : '-' }}</strong>
            </div>
            <div v-if="selectedTier && tierCodeType(selectedTier) === 'subscription'" class="summary-detail">
              <span>{{ tierGroupLabel(selectedTier) }}</span>
              <small>{{ formatLimitTriplet(selectedTier) }}</small>
              <small v-if="!selectedTierSubmittable" class="tier-error">
                {{ selectedTier.upstream_error || '订阅分组不可用，请刷新后重试' }}
              </small>
            </div>
            <label class="note-label" for="recharge-note">备注</label>
            <el-input
              id="recharge-note"
              v-model="form.note"
              type="textarea"
              :rows="4"
              maxlength="500"
              show-word-limit
              placeholder="可填写付款信息、订单号或其他补充说明"
            />
            <el-button
              class="submit-button"
              type="primary"
              :loading="loading"
              :disabled="!submittableTiers.length || !form.tierId || !selectedTierSubmittable"
              @click="submit"
            >
              提交兑换申请
            </el-button>
          </div>
        </aside>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { CopyDocument, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import AppLayout from '@/components/AppLayout.vue'
import StatusTag from '@/components/StatusTag.vue'
import { createAccessRequest, listAccessRequests } from '@/api/access'
import { listRedeemCodes, listRedeemRequests, listRedeemTiers } from '@/api/redeem'
import type { AccessRequest, RedeemCode, RedeemRequest, RedeemTier } from '@/api/types'
import {
  formatCodeTypeLabel,
  formatCodeValue,
  formatLimitTriplet,
  isSubscriptionTierAvailable,
  tierCodeType,
  tierGroupLabel,
} from '@/utils/tiers'
import { copyText } from '@/utils/clipboard'

const loading = ref(false)
const loadingCore = ref(false)
const loadingCodes = ref(false)
const tiers = ref<RedeemTier[]>([])
const items = ref<AccessRequest[]>([])
const redeemRequests = ref<RedeemRequest[]>([])
const codes = ref<RedeemCode[]>([])
let refreshTimer: number | undefined

const route = useRoute()
const form = reactive({
  tierId: 0,
  note: '',
})

const enabledTiers = computed(() => tiers.value.filter((tier) => tier.enabled))
const submittableTiers = computed(() => enabledTiers.value.filter((tier) => tierSelectable(tier)))
const tierMap = computed(() => new Map(tiers.value.map((tier) => [tier.id, tier])))
const selectedTier = computed(() => tierMap.value.get(form.tierId) ?? null)
const selectedTierName = computed(() => selectedTier.value ? tierDisplayName(selectedTier.value) : '-')
const selectedTierHeadline = computed(() => {
  if (!selectedTier.value) return '-'
  if (tierCodeType(selectedTier.value) === 'subscription') {
    return validityDaysText(selectedTier.value)
  }
  return `${formatMoney(selectedTier.value.amount)} USD`
})
const selectedTierSubmittable = computed(() => selectedTier.value ? tierSelectable(selectedTier.value) : false)
const redeemRequestMap = computed(() => new Map(redeemRequests.value.map((request) => [request.id, request])))
const codesByAccessRequestId = computed(() => {
  const groups = new Map<number, RedeemCode[]>()
  for (const code of codes.value) {
    const accessRequestId = redeemRequestMap.value.get(code.request_id)?.access_request_id
    if (!accessRequestId) continue
    const group = groups.get(accessRequestId) ?? []
    group.push(code)
    groups.set(accessRequestId, group)
  }
  return groups
})
const accessRequestRows = computed(() => items.value.slice(0, 5).map((request) => ({
  request,
  codes: codesByAccessRequestId.value.get(request.id) ?? [],
})))
const unlinkedCodes = computed(() => codes.value.filter((code) => !redeemRequestMap.value.get(code.request_id)?.access_request_id))
const loadingData = computed(() => loadingCore.value || loadingCodes.value)

type LoadAllOptions = {
  silent?: boolean
}

async function loadAll(options: LoadAllOptions = {}) {
  await Promise.all([loadCoreData(options), loadCodes(options)])
}

async function loadCoreData(options: LoadAllOptions = {}) {
  if (!options.silent) {
    loadingCore.value = true
  }
  try {
    const [tierData, requestData] = await Promise.all([
      listRedeemTiers(),
      listAccessRequests(),
    ])
    tiers.value = tierData
    items.value = requestData
    syncDefaultTier()
  } catch (error: any) {
    if (!options.silent) {
      ElMessage.error(error?.message ?? '加载充值申请失败')
    }
  } finally {
    if (!options.silent) {
      loadingCore.value = false
    }
  }
}

async function loadCodes(options: LoadAllOptions = {}) {
  if (!options.silent) {
    loadingCodes.value = true
  }
  try {
    const [requestData, codeData] = await Promise.all([
      listRedeemRequests(),
      listRedeemCodes(),
    ])
    redeemRequests.value = requestData
    codes.value = codeData
  } catch (error: any) {
    if (!options.silent) {
      ElMessage.error(error?.message ?? '加载兑换码失败')
    }
  } finally {
    if (!options.silent) {
      loadingCodes.value = false
    }
  }
}

function selectTier(tier: RedeemTier) {
  if (!tierSelectable(tier)) {
    return
  }
  form.tierId = tier.id
}

function tierById(id: number) {
  return tierMap.value.get(id)
}

function tierNameById(id: number) {
  const tier = tierById(id)
  if (!tier) return `#${id}`
  return tierDisplayName(tier)
}

function syncDefaultTier() {
  if (!submittableTiers.value.length) {
    form.tierId = 0
    return
  }
  if (!submittableTiers.value.some((tier) => tier.id === form.tierId)) {
    form.tierId = submittableTiers.value[0].id
  }
}

async function submit() {
  if (!form.tierId) {
    ElMessage.warning('请选择兑换档位')
    return
  }
  if (!selectedTierSubmittable.value) {
    ElMessage.warning('当前订阅档位不可提交，请刷新后重试')
    return
  }
  loading.value = true
  try {
    await createAccessRequest(form.tierId, form.note)
    ElMessage.success('兑换申请已提交')
    form.note = ''
    await loadAll()
  } catch (error: any) {
    ElMessage.error(error?.message ?? '提交充值申请失败')
  } finally {
    loading.value = false
  }
}

async function copyCode(code: string) {
  const copied = await copyText(code)
  if (copied) {
    ElMessage.success('已复制')
  }
}

function isCodeUsed(code: RedeemCode) {
  return code.status === 'used'
}

function codeEmptyText(status: string) {
  if (status === 'consumed' || status === 'approved') return '暂无关联兑换码'
  if (status === 'rejected') return '申请未通过'
  if (status === 'expired') return '申请已过期'
  return '审批通过后会在这里显示兑换码'
}

function tierDisplayName(tier: RedeemTier) {
  return tier.label?.trim() || (tierCodeType(tier) === 'subscription' ? tierGroupLabel(tier) : `档位 #${tier.id}`)
}

function tierSelectable(tier: RedeemTier) {
  return tier.enabled && isSubscriptionTierAvailable(tier)
}

function validityDaysText(tier: Pick<RedeemTier, 'validity_days'>) {
  const days = Number(tier.validity_days ?? 0)
  return days > 0 ? `${days} 天订阅` : '订阅'
}

function requestTierName(request: AccessRequest) {
  return request.tier_label?.trim() || tierNameById(request.tier_id)
}

function requestSummary(request: AccessRequest) {
  if (request.code_type === 'subscription') {
    const group = request.sub2api_group_name?.trim() || '订阅分组'
    return `${group} · ${validityDaysText(request)} · ${formatCny(request.pay_amount_cny)}`
  }
  return `${formatMoney(request.amount)} 美元 · ${formatCny(request.pay_amount_cny)}`
}

function requestLimitSummary(request: AccessRequest) {
  return formatLimitTriplet({
    sub2api_daily_limit_usd: request.sub2api_daily_limit_usd,
    sub2api_weekly_limit_usd: request.sub2api_weekly_limit_usd,
    sub2api_monthly_limit_usd: request.sub2api_monthly_limit_usd,
  })
}

function refreshWhenVisible() {
  if (document.visibilityState === 'hidden') return
  void loadAll({ silent: true })
}

function refreshFromMenu(event: Event) {
  const targetPath = event instanceof CustomEvent ? event.detail?.path : ''
  if (targetPath && targetPath !== route.path) return
  void loadAll()
}

function formatMoney(value: number) {
  return Number(value).toFixed(0)
}

function formatCny(value: number) {
  return `¥${Number(value).toFixed(0)}`
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
    void loadAll({ silent: true })
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

<style scoped>
.recharge-page {
  --recharge-side-width: 340px;
  display: grid;
  gap: 16px;
}

.recharge-hero {
  display: grid;
  grid-template-columns: minmax(0, 1fr) var(--recharge-side-width);
  gap: 16px;
  align-items: stretch;
}

.recharge-hero > div:first-child,
.wallet-card,
.tier-section,
.summary-card,
.status-panel {
  border: 1px solid #dfe7f1;
  border-radius: 8px;
  background: #fff;
}

.recharge-hero > div:first-child {
  padding: 24px;
}

.eyebrow {
  margin-bottom: 8px;
  color: #1d6fd0;
  font-size: 12px;
  font-weight: 800;
  text-transform: uppercase;
}

h1,
h2,
h3 {
  margin: 0;
  letter-spacing: 0;
}

h1 {
  font-size: 30px;
  line-height: 1.15;
}

h2 {
  font-size: 17px;
}

h3 {
  font-size: 15px;
}

p {
  margin: 8px 0 0;
  color: #64748b;
}

.wallet-card {
  display: grid;
  align-content: center;
  padding: 22px;
  color: #fff;
  background: linear-gradient(135deg, #123a5a, #247a8a);
  box-shadow: 0 16px 34px rgba(15, 23, 42, 0.16);
}

.wallet-card span,
.wallet-card small {
  opacity: 0.86;
}

.wallet-card strong {
  margin: 8px 0 2px;
  font-size: 34px;
  line-height: 1.1;
}

.recharge-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) var(--recharge-side-width);
  gap: 16px;
  align-items: start;
}

.tier-section,
.status-panel,
.summary-card {
  padding: 16px;
}

.section-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
}

.section-heading.compact {
  margin-bottom: 10px;
}

.tier-grid {
  min-height: 140px;
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
  gap: 12px;
}

.tier-card {
  min-height: 128px;
  display: grid;
  gap: 6px;
  justify-items: start;
  align-content: center;
  padding: 16px;
  border: 1px solid #dfe7f1;
  border-radius: 8px;
  background: #fff;
  color: #1f2937;
  text-align: left;
  cursor: pointer;
}

.tier-card:disabled,
.tier-card.disabled {
  cursor: not-allowed;
  opacity: 0.62;
}

.tier-card.active {
  border-color: #1d6fd0;
  box-shadow: inset 0 0 0 1px #1d6fd0;
}

.tier-card:hover:not(:disabled) {
  border-color: #7db1ee;
}

.tier-kind {
  color: #1d6fd0;
  font-size: 12px;
  font-weight: 800;
}

.tier-name {
  font-size: 15px;
  font-weight: 800;
}

.tier-card strong {
  color: #c2410c;
  font-size: 28px;
  line-height: 1.1;
}

.tier-card span:last-child {
  color: #64748b;
}

.tier-card small {
  color: #64748b;
  font-size: 12px;
  overflow-wrap: anywhere;
}

.tier-error {
  color: #b42318 !important;
}

.submit-panel {
  display: grid;
  gap: 12px;
  position: sticky;
  top: 16px;
}

.summary-line {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 0;
  border-bottom: 1px solid #edf2f7;
}

.summary-line span {
  color: #64748b;
}

.summary-detail {
  display: grid;
  gap: 4px;
  padding: 10px 0;
  border-bottom: 1px solid #edf2f7;
  color: #64748b;
  font-size: 13px;
}

.summary-detail span {
  color: #1f2937;
  font-weight: 700;
}

.note-label {
  display: block;
  margin: 14px 0 6px;
  color: #64748b;
  font-size: 13px;
}

.submit-button {
  width: 100%;
  margin-top: 12px;
}

.status-panel {
  margin-top: 16px;
}

.request-list {
  display: grid;
  gap: 8px;
}

.request-item {
  display: grid;
  gap: 10px;
  padding: 12px;
  border: 1px solid #edf2f7;
  border-radius: 8px;
}

.request-main {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.request-main > div {
  min-width: 0;
  display: grid;
  gap: 3px;
}

.request-main span,
.request-main small {
  color: #64748b;
}

.request-codes {
  display: grid;
  gap: 8px;
  padding-top: 10px;
  border-top: 1px solid #edf2f7;
}

.code-item,
.code-actions {
  display: flex;
  align-items: center;
}

.code-item {
  justify-content: space-between;
  gap: 12px;
  padding: 10px 12px;
  border: 1px solid #edf2f7;
  border-radius: 8px;
  background: #fbfcfe;
}

.code-meta {
  min-width: 0;
  display: grid;
  gap: 4px;
}

.code-meta code {
  color: #0f172a;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
  font-weight: 800;
  overflow-wrap: anywhere;
}

.code-meta span {
  color: #64748b;
  font-size: 13px;
}

.code-item.used .code-meta code {
  color: #64748b;
  text-decoration: line-through;
}

.code-actions {
  flex: 0 0 auto;
  gap: 8px;
}

.request-code-empty {
  padding-top: 10px;
  border-top: 1px solid #edf2f7;
  color: #94a3b8;
  font-size: 13px;
}

.legacy-codes {
  margin-top: 8px;
}

.empty-state {
  display: grid;
  place-items: center;
  min-height: 120px;
  color: #64748b;
  border: 1px dashed #cfd9e6;
  border-radius: 8px;
  background: #fbfcfe;
}

.empty-state.small {
  min-height: 72px;
}

@media (max-width: 900px) {
  .recharge-page {
    gap: 12px;
  }

  .recharge-hero,
  .recharge-layout {
    grid-template-columns: 1fr;
  }

  .recharge-hero > div:first-child {
    display: none;
  }

  .wallet-card {
    min-height: 132px;
    border-radius: 12px;
  }

  .wallet-card strong {
    font-size: 32px;
  }

  .section-heading {
    align-items: flex-start;
  }

  .tier-section,
  .summary-card,
  .status-panel {
    padding: 14px;
  }

  .tier-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 10px;
  }

  .tier-card {
    min-height: 104px;
    padding: 12px;
  }

  .tier-card strong {
    font-size: 22px;
  }

  .submit-panel {
    position: static;
  }

  .summary-card {
    position: sticky;
    bottom: 0;
    z-index: 5;
    box-shadow: 0 -12px 28px rgba(15, 23, 42, 0.1);
  }

  .request-main,
  .code-item {
    align-items: flex-start;
  }
}

@media (max-width: 520px) {
  .section-heading {
    display: grid;
  }

  .section-heading :deep(.el-button) {
    width: 100%;
  }

  .tier-grid {
    grid-template-columns: 1fr 1fr;
  }
  .request-main,
  .code-item,
  .code-actions {
    align-items: stretch;
    display: grid;
  }
}
</style>
