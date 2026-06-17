<template>
  <el-tag :type="preset.type" effect="light" size="small">
    {{ preset.label }}
  </el-tag>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  status: string
}>()

const preset = computed(() => {
  const key = props.status?.toLowerCase?.() ?? ''
  const map: Record<string, { type: 'success' | 'warning' | 'danger' | 'info' | ''; label: string }> = {
    pending: { type: 'warning', label: '待处理' },
    approved: { type: 'success', label: '已批准' },
    rejected: { type: 'danger', label: '已拒绝' },
    consumed: { type: 'success', label: '已完成' },
    expired: { type: 'info', label: '已过期' },
    sent: { type: 'success', label: '已发送' },
    failed: { type: 'danger', label: '失败' },
    issued: { type: 'success', label: '已发放' },
    unused: { type: 'info', label: '未使用' },
    used: { type: 'success', label: '已使用' },
    disabled: { type: 'info', label: '已停用' },
    enabled: { type: 'success', label: '已启用' },
    admin: { type: 'success', label: '管理员' },
    user: { type: 'info', label: '用户' },
    direct_charge_succeeded: { type: 'success', label: '直充成功' },
    redeem_code_issued: { type: 'success', label: '已发码' },
    direct_charge: { type: 'success', label: '直充' },
    redeem_code: { type: 'info', label: '兑换码' },
    redeem_code_fallback: { type: 'warning', label: '回退发码' },
  }
  return map[key] ?? { type: 'info', label: key || '未知' }
})
</script>
