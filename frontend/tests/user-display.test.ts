import assert from 'node:assert/strict'
import { test } from 'node:test'

import { formatUserDisplayName } from '../src/utils/user-display.ts'

test('formatUserDisplayName prefers username when present', () => {
  assert.equal(
    formatUserDisplayName({ username: 'alice', email: 'alice@example.com' }),
    'alice',
  )
})

test('formatUserDisplayName falls back to email when username is blank', () => {
  assert.equal(
    formatUserDisplayName({ username: '   ', email: 'alice@example.com' }),
    'alice@example.com',
  )
})

test('formatUserDisplayName never returns a blank label', () => {
  assert.equal(formatUserDisplayName({ username: '   ', email: '   ' }), '未命名用户')
})
