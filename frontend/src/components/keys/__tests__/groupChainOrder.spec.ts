import { describe, expect, it } from 'vitest'
import { applyDraggedOrder, moveChainItem } from '@/components/keys/groupChainOrder'

describe('group chain order helpers', () => {
  it('moves an item one step and keeps every member', () => {
    expect(moveChainItem([1, 2, 3], 0, 1)).toEqual([2, 1, 3])
    expect(moveChainItem([1, 2, 3], 2, -1)).toEqual([1, 3, 2])
  })

  it('refuses to move past the ends of the chain', () => {
    expect(moveChainItem([1, 2, 3], 0, -1)).toEqual([1, 2, 3])
    expect(moveChainItem([1, 2, 3], 2, 1)).toEqual([1, 2, 3])
    expect(moveChainItem([1, 2, 3], 5, -1)).toEqual([1, 2, 3])
  })

  it('accepts a dragged order that only permutes the chain', () => {
    expect(applyDraggedOrder([1, 2, 3], [3, 1, 2])).toEqual([3, 1, 2])
  })

  it('rejects a dragged order that adds or drops a group', () => {
    expect(applyDraggedOrder([1, 2, 3], [2, 1])).toEqual([1, 2, 3])
    expect(applyDraggedOrder([1, 2, 3], [1, 2, 4])).toEqual([1, 2, 3])
  })
})
