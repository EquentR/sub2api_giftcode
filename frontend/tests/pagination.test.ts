import assert from 'node:assert/strict'
import { test } from 'node:test'

import { paginateItems } from '../src/utils/pagination.ts'

test('paginateItems returns a stable page slice and metadata', () => {
  const result = paginateItems([1, 2, 3, 4, 5], 2, 2)

  assert.deepEqual(result.items, [3, 4])
  assert.equal(result.total, 5)
  assert.equal(result.page, 2)
  assert.equal(result.pageSize, 2)
  assert.equal(result.pages, 3)
})

test('paginateItems clamps out of range page values', () => {
  const over = paginateItems(['a', 'b', 'c'], 9, 2)
  const under = paginateItems(['a', 'b', 'c'], 0, 2)

  assert.deepEqual(over.items, ['c'])
  assert.equal(over.page, 2)
  assert.deepEqual(under.items, ['a', 'b'])
  assert.equal(under.page, 1)
})
