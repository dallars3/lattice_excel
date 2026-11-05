import { beforeEach, describe, expect, it } from 'vitest'

import { deriveSaveState, trackWrite, workspaceStore } from './workspace'

describe('workspaceStore：在途写计数与保存状态派生（票 03）', () => {
  beforeEach(() => {
    workspaceStore.setState({ inFlightWriteCount: 0, offline: false })
  })

  it('beginWrite / endWrite 维护在途写计数', () => {
    workspaceStore.getState().beginWrite()
    workspaceStore.getState().beginWrite()
    expect(workspaceStore.getState().inFlightWriteCount).toBe(2)

    workspaceStore.getState().endWrite()
    expect(workspaceStore.getState().inFlightWriteCount).toBe(1)
    workspaceStore.getState().endWrite()
    expect(workspaceStore.getState().inFlightWriteCount).toBe(0)
  })

  it('endWrite 不会把计数降为负值', () => {
    workspaceStore.getState().endWrite()
    expect(workspaceStore.getState().inFlightWriteCount).toBe(0)
  })

  it('保存状态派生：saving → saved，offline 优先', () => {
    expect(deriveSaveState(0, false)).toBe('saved')
    expect(deriveSaveState(2, false)).toBe('saving')
    expect(deriveSaveState(0, true)).toBe('offline')
    expect(deriveSaveState(2, true)).toBe('offline')
  })

  it('trackWrite 在 promise 未完成期间计数为 1，完成后归零', async () => {
    let release: () => void = () => undefined
    const pending = new Promise<void>((resolve) => {
      release = resolve
    })

    const task = trackWrite(async () => {
      await pending
      return 'done'
    })

    expect(workspaceStore.getState().inFlightWriteCount).toBe(1)
    release()
    await expect(task).resolves.toBe('done')
    expect(workspaceStore.getState().inFlightWriteCount).toBe(0)
  })

  it('trackWrite 在失败时同样归零并原样抛出错误', async () => {
    await expect(
      trackWrite(async () => {
        throw new Error('write failed')
      }),
    ).rejects.toThrow('write failed')

    expect(workspaceStore.getState().inFlightWriteCount).toBe(0)
  })

  it('offline 占位布尔可切换（票 09 接线）', () => {
    workspaceStore.getState().setOffline(true)
    expect(workspaceStore.getState().offline).toBe(true)
    workspaceStore.getState().setOffline(false)
    expect(workspaceStore.getState().offline).toBe(false)
  })
})
