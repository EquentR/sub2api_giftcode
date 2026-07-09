<template>
  <el-container class="app-shell">
    <el-aside v-if="layout.sidebarVisible" width="240px" class="app-sidebar">
      <div class="brand">
        <div class="brand-title">{{ branding.title }}</div>
        <div class="brand-subtitle">{{ branding.subtitle }}</div>
      </div>

      <el-menu
        :default-active="route.path"
        class="menu"
        background-color="#ffffff"
        text-color="#1f2937"
        active-text-color="#2563eb"
        @select="onMenuSelect"
      >
        <el-menu-item index="/dashboard">
          <el-icon><House /></el-icon>
          <span>首页</span>
        </el-menu-item>
        <el-menu-item index="/recharge-request">
          <el-icon><Wallet /></el-icon>
          <span>充值兑换申请</span>
        </el-menu-item>
        <el-menu-item v-if="session.isAdmin" index="/admin">
          <el-icon><DataLine /></el-icon>
          <span>管理</span>
        </el-menu-item>
        <el-menu-item v-if="session.isAdmin" index="/admin/access">
          <el-icon><Operation /></el-icon>
          <span>审批队列</span>
        </el-menu-item>
        <el-menu-item v-if="session.isAdmin" index="/admin/openai-accounts">
          <el-icon><Connection /></el-icon>
          <span>OpenAI UA</span>
        </el-menu-item>
        <el-menu-item v-if="session.isAdmin" index="/admin/tiers">
          <el-icon><Setting /></el-icon>
          <span>档位设置</span>
        </el-menu-item>
        <el-menu-item v-if="session.isAdmin" index="/admin/compensation">
          <el-icon><Wallet /></el-icon>
          <span>批量补偿</span>
        </el-menu-item>
      </el-menu>
    </el-aside>

    <el-container>
      <el-header class="app-header">
        <div>
          <div class="header-title">{{ title }}</div>
          <div class="muted">{{ subtitle }}</div>
        </div>
        <div class="header-actions">
          <el-tooltip :content="layout.sidebarVisible ? '隐藏侧边栏' : '显示侧边栏'" placement="bottom">
            <el-button
              class="sidebar-toggle"
              :icon="layout.sidebarVisible ? Fold : Expand"
              :aria-label="layout.sidebarVisible ? '隐藏侧边栏' : '显示侧边栏'"
              circle
              plain
              @click="layout.toggleSidebar"
            />
          </el-tooltip>
          <el-tag v-if="session.user" type="info" effect="light">{{ sessionUserLabel }}</el-tag>
          <el-tag v-if="session.isAdmin" type="success" effect="light">管理员</el-tag>
          <el-button class="logout-button" :icon="SwitchButton" @click="onLogout">退出登录</el-button>
        </div>
      </el-header>
      <el-main class="page">
        <slot />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup lang="ts">
import { computed, nextTick, watchEffect } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Connection, DataLine, Expand, Fold, House, Operation, Setting, SwitchButton, Wallet } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useBrandingStore } from '@/stores/branding'
import { useLayoutStore } from '@/stores/layout'
import { useSessionStore } from '@/stores/session'
import { formatUserDisplayName } from '@/utils/user-display'

const props = defineProps<{
  title: string
  subtitle?: string
}>()

const session = useSessionStore()
const branding = useBrandingStore()
const layout = useLayoutStore()
const router = useRouter()
const route = useRoute()

const subtitle = computed(() => props.subtitle ?? '')
const sessionUserLabel = computed(() => formatUserDisplayName(session.user))

watchEffect(() => {
  const brandTitle = branding.title.trim()
  const pageTitle = props.title.trim()
  document.title = brandTitle ? `${brandTitle} · ${pageTitle}` : pageTitle
})

async function onLogout() {
  await session.signOut()
  ElMessage.success('已退出登录')
  await router.push('/login')
}

async function onMenuSelect(index: string) {
  if (index !== route.path) {
    await router.push(index)
  }
  await nextTick()
  window.setTimeout(() => {
    if (index !== route.path) return
    window.dispatchEvent(new CustomEvent('giftcode:refresh-current-view', { detail: { path: index } }))
  }, 0)
}
</script>

<style scoped>
.app-sidebar {
  background: #fff;
  border-right: 1px solid #dfe6ef;
  min-height: 100vh;
}

.brand {
  padding: 18px 16px 8px;
}

.brand-title {
  font-size: 18px;
  font-weight: 700;
}

.brand-subtitle {
  font-size: 12px;
  color: #6b7280;
}

.menu {
  border-right: 0;
}

.app-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  background: #eef2f6;
  border-bottom: 1px solid #dfe6ef;
}

.header-title {
  font-size: 18px;
  font-weight: 700;
}

.header-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
}

.sidebar-toggle {
  flex: 0 0 auto;
}

.logout-button {
  white-space: nowrap;
}

@media (max-width: 900px) {
  .app-shell {
    display: flex;
    flex-direction: column;
  }

  .app-sidebar {
    width: 100% !important;
    min-height: auto;
    border-right: 0;
    border-bottom: 1px solid #dfe6ef;
  }

  .brand {
    padding: 12px 14px 4px;
  }

  .brand-subtitle {
    display: none;
  }

  .menu {
    display: flex;
    overflow-x: auto;
    padding: 0 8px 8px;
    white-space: nowrap;
  }

  .menu :deep(.el-menu-item) {
    flex: 0 0 auto;
    height: 40px;
    border-radius: 8px;
  }

  .app-header {
    height: auto;
    min-height: 56px;
    align-items: flex-start;
    padding: 10px 14px;
  }

  .header-title {
    font-size: 16px;
  }

  .header-actions {
    gap: 6px;
    justify-content: flex-end;
  }

  .header-actions :deep(.el-tag) {
    display: none;
  }

  .page {
    padding: 12px;
  }
}

@media (max-width: 520px) {
  .app-header {
    display: grid;
  }
}
</style>
