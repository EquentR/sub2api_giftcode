import type { UserProfile } from '@/api/types'

export function formatUserDisplayName(user?: Pick<UserProfile, 'username' | 'email'> | null) {
  return user?.username?.trim() || user?.email?.trim() || '未命名用户'
}
