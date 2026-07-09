import { createRouter, createWebHistory } from 'vue-router'
import { useSessionStore } from '@/stores/session'
import LoginView from '@/views/LoginView.vue'
import UserDashboardView from '@/views/UserDashboardView.vue'
import AccessRequestView from '@/views/AccessRequestView.vue'
import ApprovalConfirmView from '@/views/ApprovalConfirmView.vue'
import RechargeRequestView from '@/views/RechargeRequestView.vue'
import AdminDashboardView from '@/views/AdminDashboardView.vue'
import AdminAccessQueueView from '@/views/AdminAccessQueueView.vue'
import AdminOpenAIAccountsView from '@/views/AdminOpenAIAccountsView.vue'
import AdminTiersView from '@/views/AdminTiersView.vue'
import AdminCompensationView from '@/views/AdminCompensationView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/dashboard' },
    { path: '/login', name: 'login', component: LoginView, meta: { plain: true } },
    { path: '/approval/confirm', name: 'approval-confirm', component: ApprovalConfirmView, meta: { plain: true } },
    { path: '/dashboard', name: 'dashboard', component: UserDashboardView, meta: { requiresAuth: true } },
    { path: '/recharge-request', name: 'recharge-request', component: RechargeRequestView, meta: { requiresAuth: true } },
    { path: '/access-request', name: 'access-request', component: AccessRequestView, meta: { requiresAuth: true } },
    { path: '/redeem-request', redirect: '/recharge-request' },
    { path: '/admin', name: 'admin-dashboard', component: AdminDashboardView, meta: { requiresAuth: true, requiresAdmin: true } },
    { path: '/admin/access', name: 'admin-access', component: AdminAccessQueueView, meta: { requiresAuth: true, requiresAdmin: true } },
    { path: '/admin/openai-accounts', name: 'admin-openai-accounts', component: AdminOpenAIAccountsView, meta: { requiresAuth: true, requiresAdmin: true } },
    { path: '/admin/tiers', name: 'admin-tiers', component: AdminTiersView, meta: { requiresAuth: true, requiresAdmin: true } },
    { path: '/admin/compensation', name: 'admin-compensation', component: AdminCompensationView, meta: { requiresAuth: true, requiresAdmin: true } },
  ],
})

router.beforeEach(async (to) => {
  const session = useSessionStore()
  if (!session.bootstrapped) {
    await session.bootstrap()
  }

  if (to.name === 'login' && session.isLoggedIn) {
    return session.isAdmin ? { name: 'admin-dashboard' } : { name: 'dashboard' }
  }

  if (to.meta.requiresAuth && !session.isLoggedIn) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }

  if (to.meta.requiresAdmin && !session.isAdmin) {
    return { name: 'dashboard' }
  }

  return true
})

export default router
