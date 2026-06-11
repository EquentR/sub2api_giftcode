import assert from 'node:assert/strict'
import { test } from 'node:test'

import { copyText } from '../src/utils/clipboard.ts'

test('copyText uses the native clipboard path when it succeeds', async () => {
  const calls: string[] = []

  const result = await copyText('CODE-123', {
    writeText: async (text) => {
      calls.push(`native:${text}`)
    },
    legacyCopy: (text) => {
      calls.push(`legacy:${text}`)
      return true
    },
    promptManualCopy: (text) => {
      calls.push(`prompt:${text}`)
    },
  })

  assert.equal(result, true)
  assert.deepEqual(calls, ['native:CODE-123'])
})

test('copyText falls back to legacy copy when the clipboard API rejects', async () => {
  const calls: string[] = []

  const result = await copyText('CODE-456', {
    writeText: async () => {
      throw new Error('blocked')
    },
    legacyCopy: (text) => {
      calls.push(`legacy:${text}`)
      return true
    },
    promptManualCopy: (text) => {
      calls.push(`prompt:${text}`)
    },
  })

  assert.equal(result, true)
  assert.deepEqual(calls, ['legacy:CODE-456'])
})

test('copyText shows a manual prompt when both copy paths fail', async () => {
  const calls: string[] = []

  const result = await copyText('CODE-789', {
    writeText: async () => {
      throw new Error('blocked')
    },
    legacyCopy: () => false,
    promptManualCopy: (text) => {
      calls.push(`prompt:${text}`)
    },
  })

  assert.equal(result, false)
  assert.deepEqual(calls, ['prompt:CODE-789'])
})
