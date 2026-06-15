import assert from 'node:assert/strict'
import { test } from 'node:test'

import { formatLimitValue } from '../src/utils/tiers.ts'

test('formatLimitValue displays zero as unlimited', () => {
  assert.equal(formatLimitValue(0), '无限制')
})
