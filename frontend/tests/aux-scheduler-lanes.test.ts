import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { test } from 'node:test'

const __dirname = dirname(fileURLToPath(import.meta.url))
const readSource = (path: string) => readFileSync(resolve(__dirname, path), 'utf8')

test('aux scheduler frontend exposes lane and migration fields in the API contract', () => {
  const typeSource = readSource('../src/api/types.ts')
  assert.match(typeSource, /model_names:\s*string\[\]/)
  assert.match(typeSource, /lanes:\s*number\[\]\[\]/)
  assert.match(typeSource, /maximum_auto_lane:\s*number/)
  assert.match(typeSource, /migration_status:\s*''\s*\|\s*'needs_migration'/)
  assert.match(typeSource, /lane_accounts:\s*AuxSchedulerLaneView\[\]/)
  assert.match(typeSource, /interface AuxSchedulerMigrationSource/)
  assert.match(typeSource, /interface AuxSchedulerAccountInfo\s*\{[\s\S]*?name\?: string[\s\S]*?type\?: string[\s\S]*?status\?: string[\s\S]*?schedulable\?: boolean/)
})

test('aux scheduler view distinguishes migration, disabled, and active rules', () => {
  const source = readSource('../src/views/AdminAuxSchedulerView.vue')
  assert.match(source, /needs_migration/)
  assert.match(source, /'需要迁移'/)
  assert.match(source, /'已禁用'/)
  assert.match(source, /:type="statusTagType\(row\)"/)
  assert.match(source, /statusLabel\(row\)/)
  assert.match(source, /migration_status === 'needs_migration'\) return '需要迁移'/)
  assert.match(source, /observedAccountLabel/)
  assert.match(source, /item\.status \|\| 'unknown'/)
  assert.match(source, /'可调度'/)
  assert.match(source, /'不可调度'/)
  assert.match(source, /'未观测'/)
  assert.match(source, /否则不会自动调度/)
  assert.match(source, /lane_accounts/)
  assert.match(source, /model_names/)
  assert.match(source, /未配置模型集合/)
})
