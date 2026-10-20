/**
 * 普通分组链的顺序调整。
 *
 * 拖拽排序与「上移/下移」按钮共用同一套语义：顺序即尝试优先级，
 * 任何一条路径都不能越界或丢成员。
 */

/**把第 index 项向 delta 方向移动一步；越界时原样返回。 */
export function moveChainItem(ids: number[], index: number, delta: number): number[] {
  const target = index + delta
  if (index < 0 || index >= ids.length) return [...ids]
  if (target < 0 || target >= ids.length) return [...ids]
  const next = [...ids]
  const moved = next[index]!
  next.splice(index, 1)
  next.splice(target, 0, moved)
  return next
}

/**
 * 拖拽结束后的新顺序。
 *
 * 只接受「成员集合不变、仅顺序变化」的结果：拖拽不得增删分组，
 * 否则就退化成一次普通的链改写，与拖拽语义不符。
 */
export function applyDraggedOrder(ids: number[], dragged: number[]): number[] {
  if (dragged.length !== ids.length) return [...ids]
  const remaining = new Set(ids)
  for (const id of dragged) {
    if (!remaining.has(id)) return [...ids]
    remaining.delete(id)
  }
  return [...dragged]
}
