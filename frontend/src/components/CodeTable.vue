<template>
  <el-table :data="codes" stripe size="small" style="width: 100%">
    <el-table-column prop="code" label="兑换码" min-width="220">
      <template #default="{ row }">
        <div class="code-cell">
          <code>{{ row.code }}</code>
          <el-button text :icon="CopyDocument" @click="copy(row.code)" />
        </div>
      </template>
    </el-table-column>
    <el-table-column prop="status" label="状态" width="110">
      <template #default="{ row }">
        <StatusTag :status="row.status" />
      </template>
    </el-table-column>
    <el-table-column prop="code_type" label="类型" width="110">
      <template #default="{ row }">
        {{ formatCodeTypeLabel(row.code_type) }}
      </template>
    </el-table-column>
    <el-table-column prop="value" label="内容" width="120">
      <template #default="{ row }">
        {{ formatCodeValue(row) }}
      </template>
    </el-table-column>
    <el-table-column prop="request_id" label="申请" width="90" />
    <el-table-column prop="used_by_upstream_user_id" label="使用者ID" width="110" />
    <el-table-column prop="used_at" label="使用时间" min-width="180">
      <template #default="{ row }">
        {{ formatTime(row.used_at) }}
      </template>
    </el-table-column>
    <el-table-column prop="created_at" label="创建时间" min-width="180">
      <template #default="{ row }">
        {{ formatTime(row.created_at) }}
      </template>
    </el-table-column>
  </el-table>
</template>

<script setup lang="ts">
import { CopyDocument } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import StatusTag from './StatusTag.vue'
import type { RedeemCode } from '@/api/types'
import { copyText } from '@/utils/clipboard'
import { formatCodeTypeLabel, formatCodeValue } from '@/utils/tiers'

defineProps<{
  codes: RedeemCode[]
}>()

async function copy(text: string) {
  const copied = await copyText(text)
  if (copied) {
    ElMessage.success('已复制')
  }
}

function formatTime(value?: string | null) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}
</script>

<style scoped>
.code-cell {
  display: flex;
  align-items: center;
  gap: 8px;
}

code {
  padding: 2px 6px;
  border-radius: 4px;
  background: #f3f4f6;
  word-break: break-all;
}
</style>
