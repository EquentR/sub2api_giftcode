import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { test } from 'node:test'

const __dirname = dirname(fileURLToPath(import.meta.url))

function source(path: string) {
  return readFileSync(resolve(__dirname, path), 'utf8')
}

test('redeem tier types include optional original paid amount', () => {
  const types = source('../src/api/types.ts')

  assert.match(types, /original_pay_amount_cny\?: number \| null/)
})

test('tier editor exposes nullable original paid amount input', () => {
  const editor = source('../src/components/TierEditor.vue')

  assert.match(editor, /prop="original_pay_amount_cny"/)
  assert.match(editor, /label="原价"/)
  assert.match(editor, /v-model="row\.original_pay_amount_cny"/)
  assert.match(editor, /original_pay_amount_cny: normalizeOriginalPayAmount\(tier\.original_pay_amount_cny\)/)
})

test('recharge request view renders strikethrough original price only when present', () => {
  const view = source('../src/views/RechargeRequestView.vue')

  assert.match(view, /v-if="hasOriginalPrice\(tier\)"/)
  assert.match(view, /class="original-price"/)
  assert.match(view, /formatCny\(tier\.original_pay_amount_cny\)/)
  assert.match(view, /v-if="selectedTier && hasOriginalPrice\(selectedTier\)"/)
  assert.match(view, /function hasOriginalPrice/)
})
