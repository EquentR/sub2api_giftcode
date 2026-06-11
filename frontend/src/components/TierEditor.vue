<template>
  <div class="tier-editor">
    <div class="toolbar">
      <div class="muted">可用余额档位</div>
      <el-button :icon="Plus" type="primary" @click="addRow">新增档位</el-button>
    </div>

    <el-table :data="rows" stripe border size="small" style="width: 100%">
      <el-table-column prop="amount" label="到账金额" width="160">
        <template #default="{ row }">
          <el-input-number v-model="row.amount" :min="1" :step="1" style="width: 100%" @change="emitUpdate" />
        </template>
      </el-table-column>
      <el-table-column prop="pay_amount_cny" label="实付金额" width="160">
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
      <el-table-column prop="label" label="标签" min-width="200">
        <template #default="{ row }">
          <el-input v-model="row.label" placeholder="例如 120 美元" @input="emitUpdate" />
        </template>
      </el-table-column>
      <el-table-column prop="sort_order" label="排序" width="140">
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
      <el-table-column prop="enabled" label="启用" width="120">
        <template #default="{ row }">
          <el-switch v-model="row.enabled" @change="emitUpdate" />
        </template>
      </el-table-column>
      <el-table-column label="操作" width="110" fixed="right">
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
import type { BalanceTier } from '@/api/types'
import { formatTierDisplay } from '@/utils/tiers'

const props = defineProps<{
  modelValue: BalanceTier[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: BalanceTier[]]
}>()

const rows = ref<BalanceTier[]>([])

function clone(rowsIn: BalanceTier[]) {
  return rowsIn.map((tier) => ({ ...tier }))
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

function emitUpdate() {
  emit('update:modelValue', clone(rows.value))
}

function addRow() {
  rows.value.push({
    id: 0,
    amount: 120,
    pay_amount_cny: 120,
    label: formatTierDisplay({ amount: 120, pay_amount_cny: 120, label: '' }),
    enabled: true,
    sort_order: nextSort.value,
    created_at: '',
    updated_at: '',
  })
  emitUpdate()
}

function removeRow(index: number) {
  rows.value.splice(index, 1)
  emitUpdate()
}
</script>

<style scoped>
.tier-sort-input {
  width: 100%;
  max-width: 160px;
}
</style>
