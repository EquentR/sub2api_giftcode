<template>
  <div class="tier-editor">
    <div class="toolbar">
      <div class="muted">可用兑换档位</div>
      <div class="tier-actions">
        <el-button :icon="Plus" @click="addRow('balance')">新增余额档位</el-button>
        <el-button :icon="Plus" type="primary" @click="addRow('subscription')">新增订阅档位</el-button>
      </div>
    </div>

    <el-alert
      v-if="groupsError"
      :title="groupsError"
      type="warning"
      :closable="false"
      show-icon
      style="margin-bottom: 12px"
    />

    <el-table :data="rows" stripe border size="small" style="width: 100%">
      <el-table-column prop="code_type" label="类型" width="130">
        <template #default="{ row }">
          <el-select v-model="row.code_type" style="width: 100%" @change="onTypeChange(row)">
            <el-option label="余额" value="balance" />
            <el-option label="订阅" value="subscription" />
          </el-select>
        </template>
      </el-table-column>

      <el-table-column prop="amount" label="到账金额" width="150">
        <template #default="{ row }">
          <el-input-number
            v-if="tierCodeType(row) === 'balance'"
            v-model="row.amount"
            :min="1"
            :step="1"
            style="width: 100%"
            @change="emitUpdate"
          />
          <span v-else class="muted">订阅码</span>
        </template>
      </el-table-column>

      <el-table-column prop="pay_amount_cny" label="实付金额" width="150">
        <template #default="{ row }">
          <el-input-number
            v-model="row.pay_amount_cny"
            :min="1"
            :step="1"
            style="width: 100%"
            @change="emitUpdate"
          />
        </template>
      </el-table-column>

      <el-table-column prop="original_pay_amount_cny" label="原价" width="150">
        <template #default="{ row }">
          <el-input-number
            v-model="row.original_pay_amount_cny"
            :min="0"
            :step="1"
            placeholder="不显示"
            style="width: 100%"
            @change="emitUpdate"
          />
        </template>
      </el-table-column>

      <el-table-column prop="label" label="标签" min-width="190">
        <template #default="{ row }">
          <el-input v-model="row.label" placeholder="例如 120 美元 / 30 天订阅" @input="emitUpdate" />
        </template>
      </el-table-column>

      <el-table-column prop="sub2api_group_id" label="订阅分组" min-width="270">
        <template #default="{ row }">
          <template v-if="tierCodeType(row) === 'subscription'">
            <el-select
              v-model="row.sub2api_group_id"
              :loading="groupsLoading"
              placeholder="选择 sub2api 分组"
              filterable
              style="width: 100%"
              @change="onGroupChange(row)"
            >
              <el-option
                v-for="group in subscriptionGroups"
                :key="group.id"
                :label="groupLabel(group)"
                :value="group.id"
              >
                <div class="group-option">
                  <span>{{ groupLabel(group) }}</span>
                  <small>{{ formatLimitTriplet(group) }}</small>
                </div>
              </el-option>
            </el-select>
            <div class="limit-line">
              {{ groupLimitText(row) }}
            </div>
          </template>
          <span v-else class="muted">-</span>
        </template>
      </el-table-column>

      <el-table-column prop="validity_days" label="有效天数" width="140">
        <template #default="{ row }">
          <el-input-number
            v-if="tierCodeType(row) === 'subscription'"
            v-model="row.validity_days"
            :min="1"
            :step="1"
            controls-position="right"
            style="width: 100%"
            @change="emitUpdate"
          />
          <span v-else class="muted">-</span>
        </template>
      </el-table-column>

      <el-table-column prop="concurrency" label="并发数" width="140">
        <template #default="{ row }">
          <el-input-number
            v-if="tierCodeType(row) === 'subscription'"
            v-model="row.concurrency"
            :min="1"
            :step="1"
            controls-position="right"
            style="width: 100%"
            @change="emitUpdate"
          />
          <span v-else class="muted">-</span>
        </template>
      </el-table-column>

      <el-table-column prop="reset_count" label="重置次数" width="150">
        <template #default="{ row }">
          <el-input-number
            v-model="row.reset_count"
            :min="0"
            :step="1"
            :precision="0"
            :disabled="!tierCanResetQuota(row)"
            controls-position="right"
            style="width: 100%"
            @change="emitUpdate"
          />
        </template>
      </el-table-column>

      <el-table-column prop="sort_order" label="排序" width="130">
        <template #default="{ row }">
          <el-input-number
            v-model="row.sort_order"
            class="tier-sort-input"
            :min="0"
            :step="10"
            controls-position="right"
            style="width: 100%"
            @change="emitUpdate"
          />
        </template>
      </el-table-column>

      <el-table-column prop="enabled" label="启用" width="100">
        <template #default="{ row }">
          <el-switch v-model="row.enabled" @change="emitUpdate" />
        </template>
      </el-table-column>

      <el-table-column label="操作" width="90" fixed="right">
        <template #default="{ $index }">
          <el-button text type="danger" :icon="Delete" @click="removeRow($index)" />
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Delete, Plus } from '@element-plus/icons-vue'
import type { RedeemTier, SubscriptionGroup } from '@/api/types'
import { formatLimitTriplet, formatTierDisplay, groupLabel, tierCodeType } from '@/utils/tiers'

const props = withDefaults(defineProps<{
  modelValue: RedeemTier[]
  subscriptionGroups?: SubscriptionGroup[]
  groupsLoading?: boolean
  groupsError?: string
  defaultConcurrency?: number
}>(), {
  subscriptionGroups: () => [],
  groupsLoading: false,
  groupsError: '',
  defaultConcurrency: 0,
})

const emit = defineEmits<{
  'update:modelValue': [value: RedeemTier[]]
}>()

const rows = ref<RedeemTier[]>([])

const groupMap = computed(() => new Map(props.subscriptionGroups.map((group) => [group.id, group])))

function clone(rowsIn: RedeemTier[]) {
  return rowsIn.map((tier) => ({ ...tier, code_type: tierCodeType(tier) }))
}

watch(
  () => props.modelValue,
  (value) => {
    rows.value = clone(value ?? [])
  },
  { deep: true, immediate: true },
)

const nextSort = computed(() => {
  const values = rows.value.map((item) => item.sort_order ?? 0)
  return values.length ? Math.max(...values) + 10 : 10
})

function sanitizedRows() {
  return clone(rows.value).map((tier) => {
    if (tierCodeType(tier) === 'subscription') {
      return {
        ...tier,
        amount: 0,
        original_pay_amount_cny: normalizeOriginalPayAmount(tier.original_pay_amount_cny),
        sub2api_group_id: tier.sub2api_group_id ?? null,
        validity_days: Number(tier.validity_days ?? 30),
        concurrency: Math.max(0, Number(tier.concurrency || 0)),
        reset_count: tierCanResetQuota(tier) ? Math.max(0, Number(tier.reset_count || 0)) : 0,
      }
    }
    return {
      ...tier,
      amount: Number(tier.amount || 0),
      original_pay_amount_cny: normalizeOriginalPayAmount(tier.original_pay_amount_cny),
      sub2api_group_id: null,
      sub2api_group_name: '',
      sub2api_group_platform: '',
      sub2api_daily_limit_usd: null,
      sub2api_weekly_limit_usd: null,
      sub2api_monthly_limit_usd: null,
      validity_days: 0,
      concurrency: 0,
      reset_count: 0,
    }
  })
}

function emitUpdate() {
  emit('update:modelValue', sanitizedRows())
}

function addRow(codeType: 'balance' | 'subscription') {
  const baseAmount = codeType === 'balance' ? 120 : 0
  const validityDays = codeType === 'subscription' ? 30 : 0
  const concurrency = codeType === 'subscription' ? subscriptionConcurrencyDefault() : 0
  rows.value.push({
    id: 0,
    code_type: codeType,
    amount: baseAmount,
    pay_amount_cny: 120,
    original_pay_amount_cny: null,
    label: formatTierDisplay({
      code_type: codeType,
      amount: baseAmount,
      pay_amount_cny: 120,
      label: '',
      validity_days: validityDays,
    }),
    enabled: true,
    sort_order: nextSort.value,
    sub2api_group_id: null,
    sub2api_group_name: '',
    sub2api_group_platform: '',
    sub2api_daily_limit_usd: null,
    sub2api_weekly_limit_usd: null,
    sub2api_monthly_limit_usd: null,
    validity_days: validityDays,
    concurrency,
    reset_count: 0,
    upstream_available: true,
    upstream_error: '',
    created_at: '',
    updated_at: '',
  })
  emitUpdate()
}

function removeRow(index: number) {
  rows.value.splice(index, 1)
  emitUpdate()
}

function normalizeOriginalPayAmount(value?: number | null) {
  const amount = Number(value ?? 0)
  return amount > 0 ? amount : null
}

function onTypeChange(row: RedeemTier) {
  if (tierCodeType(row) === 'subscription') {
    row.amount = 0
    row.validity_days = Number(row.validity_days || 30)
    row.concurrency = Number(row.concurrency) > 0 ? Number(row.concurrency) : subscriptionConcurrencyDefault()
  } else {
    row.amount = Number(row.amount || 120)
    row.sub2api_group_id = null
    row.validity_days = 0
    row.concurrency = 0
    row.reset_count = 0
  }
  emitUpdate()
}

function subscriptionConcurrencyDefault() {
  return props.defaultConcurrency > 0 ? props.defaultConcurrency : 0
}

function onGroupChange(row: RedeemTier) {
  const group = row.sub2api_group_id ? groupMap.value.get(row.sub2api_group_id) : null
  row.sub2api_group_name = group?.name ?? ''
  row.sub2api_group_platform = group?.platform ?? ''
  row.sub2api_daily_limit_usd = group?.daily_limit_usd ?? null
  row.sub2api_weekly_limit_usd = group?.weekly_limit_usd ?? null
  row.sub2api_monthly_limit_usd = group?.monthly_limit_usd ?? null
  if (!tierCanResetQuota(row)) {
    row.reset_count = 0
  }
  emitUpdate()
}

function tierCanResetQuota(row: RedeemTier) {
  if (tierCodeType(row) !== 'subscription') return false
  const group = row.sub2api_group_id ? groupMap.value.get(row.sub2api_group_id) : null
  const limits = group ?? row
  return [
    'daily_limit_usd' in limits ? limits.daily_limit_usd : row.sub2api_daily_limit_usd,
    'weekly_limit_usd' in limits ? limits.weekly_limit_usd : row.sub2api_weekly_limit_usd,
    'monthly_limit_usd' in limits ? limits.monthly_limit_usd : row.sub2api_monthly_limit_usd,
  ].some((value) => Number(value ?? 0) > 0)
}

function groupLimitText(row: RedeemTier) {
  const group = row.sub2api_group_id ? groupMap.value.get(row.sub2api_group_id) : null
  if (group) {
    return formatLimitTriplet(group)
  }
  if (row.upstream_error) {
    return row.upstream_error
  }
  return row.sub2api_group_id ? formatLimitTriplet(row) : '请选择分组'
}
</script>

<style scoped>
.tier-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.tier-sort-input {
  width: 100%;
  max-width: 160px;
}

.group-option {
  display: grid;
  gap: 2px;
  line-height: 1.25;
}

.group-option small,
.limit-line {
  color: #6b7280;
  font-size: 12px;
}

.limit-line {
  margin-top: 4px;
  overflow-wrap: anywhere;
}
</style>
