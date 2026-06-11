<template>
  <AppLayout title="管理总览" subtitle="查看用户、申请和已发放的兑换码">
    <div class="grid-stats" style="margin-bottom: 16px">
      <div class="stat"><div class="label">用户数</div><div class="value">{{ stats?.total_users ?? 0 }}</div></div>
      <div class="stat"><div class="label">待处理</div><div class="value">{{ stats?.pending_access_requests ?? 0 }}</div></div>
      <div class="stat"><div class="label">已使用兑换码</div><div class="value">{{ stats?.redeem_codes_used ?? 0 }}</div></div>
      <div class="stat"><div class="label">启用档位</div><div class="value">{{ stats?.active_tiers ?? 0 }}</div></div>
    </div>

    <div class="surface section" style="margin-bottom: 16px">
      <div class="toolbar">
        <div>
          <div style="font-weight: 700">用户汇总</div>
          <div class="muted">统计来自本地账本。</div>
        </div>
        <div style="display: flex; gap: 8px">
          <el-button :icon="Refresh" @click="loadAll">刷新</el-button>
          <el-button type="primary" :icon="Operation" @click="router.push('/admin/access')">审批队列</el-button>
          <el-button type="primary" :icon="Setting" @click="router.push('/admin/tiers')">档位设置</el-button>
        </div>
      </div>
      <el-table :data="users" stripe size="small" style="width: 100%">
        <el-table-column prop="upstream_user_id" label="用户编号" width="100" />
        <el-table-column prop="username" label="用户名" min-width="160" />
        <el-table-column prop="email" label="邮箱" min-width="220" />
        <el-table-column prop="role" label="角色" width="110">
          <template #default="{ row }"><StatusTag :status="row.role" /></template>
        </el-table-column>
        <el-table-column prop="access_request_count" label="申请数" width="100" />
        <el-table-column prop="redeem_code_count" label="兑换码数" width="100" />
        <el-table-column prop="used_code_count" label="已使用" width="100" />
        <el-table-column label="操作" width="140" fixed="right">
          <template #default="{ row }">
            <el-button text type="primary" @click="viewCodes(row.upstream_user_id)">查看兑换码</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Operation, Refresh, Setting } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import AppLayout from '@/components/AppLayout.vue'
import StatusTag from '@/components/StatusTag.vue'
import { listUsers, stats as fetchStats } from '@/api/admin'
import type { DashboardStats, UserSummary } from '@/api/types'

const router = useRouter()
const users = ref<UserSummary[]>([])
const stats = ref<DashboardStats | null>(null)

async function loadAll() {
  try {
    const [statData, userData] = await Promise.all([fetchStats(), listUsers()])
    stats.value = statData
    users.value = userData
  } catch (error: any) {
    ElMessage.error(error?.message ?? '加载管理总览失败')
  }
}

function viewCodes(userId: number) {
  router.push({ name: 'admin-access', query: { user_id: String(userId) } })
}

onMounted(loadAll)
</script>
