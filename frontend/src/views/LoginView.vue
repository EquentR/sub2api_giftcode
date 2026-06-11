<template>
  <div class="login-page">
    <div class="login-panel surface section">
      <div class="login-brand">{{ branding.title }}</div>
      <div class="login-brand-subtitle">{{ branding.subtitle }}</div>
      <el-alert
        v-if="session.embeddedLaunchError"
        :title="session.embeddedLaunchError"
        type="warning"
        :closable="false"
        show-icon
        style="margin-bottom: 16px"
      />
      <div class="muted" style="margin-bottom: 20px">{{ subtitle }}</div>

      <el-form :model="form" label-position="top" @submit.prevent="onSubmit">
        <el-form-item label="邮箱">
          <el-input v-model="form.email" autocomplete="email" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="form.password" type="password" show-password autocomplete="current-password" />
        </el-form-item>
        <el-button type="primary" :icon="SwitchButton" :loading="session.loading" native-type="submit">
          登录
        </el-button>
      </el-form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, watchEffect } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { SwitchButton } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useBrandingStore } from '@/stores/branding'
import { useSessionStore } from '@/stores/session'

const session = useSessionStore()
const branding = useBrandingStore()
const router = useRouter()
const route = useRoute()

const form = reactive({
  email: '',
  password: '',
})

const subtitle = computed(() => {
  if (session.embeddedMode) {
    return session.embeddedLaunchError
      ? '嵌入式登录未完成，你仍然可以直接登录。'
      : `正在连接你的 ${branding.title} 会话。`
  }
  return `使用你已有的 ${branding.title} 账号登录。`
})

watchEffect(() => {
  document.title = `${branding.title} · 登录`
})

async function onSubmit() {
  try {
    await session.signIn(form.email, form.password)
    ElMessage.success('登录成功')
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : ''
    if (redirect) {
      await router.push(redirect)
      return
    }
    await router.push(session.isAdmin ? '/admin' : '/dashboard')
  } catch (error: any) {
    ElMessage.error(error?.message ?? '登录失败')
  }
}
</script>

<style scoped>
.login-page {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 24px;
}

.login-panel {
  width: min(420px, 100%);
}

.login-brand {
  font-size: 22px;
  font-weight: 700;
  margin-bottom: 6px;
}

.login-brand-subtitle {
  margin-bottom: 14px;
  color: #6b7280;
  font-size: 13px;
}
</style>
