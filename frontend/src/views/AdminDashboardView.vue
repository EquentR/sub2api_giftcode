<template>
  <AppLayout title="管理总览" subtitle="查看用户、申请和已发放的兑换码">
    <div class="surface section" style="margin-bottom: 16px">
      <div class="toolbar">
        <div>
          <div style="font-weight: 700">站点品牌</div>
          <div class="muted">这些内容会同步到侧边栏、登录页、浏览器标题和邮件主题。</div>
        </div>
        <el-button type="primary" :loading="savingBranding" @click="saveBranding">保存设置</el-button>
      </div>
      <el-form :model="brandingForm" label-position="top" class="branding-form">
        <el-form-item label="左侧栏标题">
          <el-input v-model="brandingForm.title" />
        </el-form-item>
        <el-form-item label="左侧栏副标题">
          <el-input v-model="brandingForm.subtitle" />
        </el-form-item>
        <el-form-item label="邮件主题前缀">
          <el-input v-model="brandingForm.mail_subject_prefix" placeholder="留空则自动使用标题" />
        </el-form-item>
      </el-form>
    </div>

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
          <el-button type="primary" :icon="Connection" @click="router.push('/admin/openai-accounts')">OpenAI UA</el-button>
          <el-button type="primary" :icon="Setting" @click="router.push('/admin/tiers')">档位设置</el-button>
          <el-button type="primary" :icon="Connection" @click="router.push('/admin/compensation')">批量补偿</el-button>
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
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Connection, Operation, Refresh, Setting } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import AppLayout from '@/components/AppLayout.vue'
import StatusTag from '@/components/StatusTag.vue'
import { updateSiteBranding } from '@/api/branding'
import { listUsers, stats as fetchStats } from '@/api/admin'
import type { DashboardStats, UserSummary } from '@/api/types'
import { useBrandingStore } from '@/stores/branding'

const router = useRouter()
const branding = useBrandingStore()
const users = ref<UserSummary[]>([])
const stats = ref<DashboardStats | null>(null)
const savingBranding = ref(false)
const brandingForm = reactive({
  title: branding.title,
  subtitle: branding.subtitle,
  mail_subject_prefix: branding.mailSubjectPrefix,
})

async function loadAll() {
  try {
    const [statData, userData] = await Promise.all([fetchStats(), listUsers()])
    stats.value = statData
    users.value = userData
  } catch (error: any) {
    ElMessage.error(error?.message ?? '加载管理总览失败')
  }
}

async function saveBranding() {
  savingBranding.value = true
  try {
    const updated = await updateSiteBranding({
      title: brandingForm.title,
      subtitle: brandingForm.subtitle,
      mail_subject_prefix: brandingForm.mail_subject_prefix,
    })
    branding.applyBranding(updated)
    brandingForm.title = branding.title
    brandingForm.subtitle = branding.subtitle
    brandingForm.mail_subject_prefix = branding.mailSubjectPrefix
    ElMessage.success('品牌设置已保存')
  } catch (error: any) {
    ElMessage.error(error?.message ?? '保存品牌设置失败')
  } finally {
    savingBranding.value = false
  }
}

function viewCodes(userId: number) {
  router.push({ name: 'admin-access', query: { user_id: String(userId) } })
}

onMounted(loadAll)
</script>

<style scoped>
.branding-form {
  display: grid;
  gap: 8px;
}

.branding-form :deep(.el-form-item) {
  margin-bottom: 0;
}
</style>
