import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'
import { test } from 'node:test'

const __dirname = dirname(fileURLToPath(import.meta.url))
const rechargeView = readFileSync(resolve(__dirname, '../src/views/RechargeRequestView.vue'), 'utf8')

test('recharge subscription realtime data copy uses global brand title', () => {
  assert.match(rechargeView, /useBrandingStore/)
  assert.match(rechargeView, /const branding = useBrandingStore\(\)/)
  assert.match(rechargeView, /\{\{\s*branding\.title\s*\}\} 实时数据/)
  assert.doesNotMatch(rechargeView, /来自 sub2api 实时数据/)
})
