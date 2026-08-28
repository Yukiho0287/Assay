import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import type { TaskProgressEvent, TaskStatus } from '@/lib/api'

export function isTerminalStatus(s: TaskStatus): boolean {
  return s === 'succeeded' || s === 'failed' || s === 'canceled'
}

// 订阅任务进度 SSE：EventSource 同源自带 cookie；服务端连上先推 DB 快照帧，
// 断线后浏览器自动重连、重连后再收一帧快照补平，前端无需自管重连。
// 终态帧到达即关闭连接并失效相关查询（详情/列表/用例结果）。
export function useTaskEvents(taskId: string, enabled: boolean): TaskProgressEvent | null {
  const queryClient = useQueryClient()
  const [live, setLive] = useState<TaskProgressEvent | null>(null)

  useEffect(() => {
    if (!enabled || !taskId) return
    const es = new EventSource(`/api/quality/tasks/${taskId}/events`)
    es.onmessage = (e) => {
      let ev: TaskProgressEvent
      try {
        ev = JSON.parse(e.data) as TaskProgressEvent
      } catch {
        return
      }
      // 并发槽位各自上报，帧序可能轻微乱序：同状态下 done 只增不减，避免进度条回跳
      setLive((prev) =>
        prev && prev.status === ev.status && prev.done > ev.done ? prev : ev,
      )
      if (isTerminalStatus(ev.status)) {
        es.close()
        queryClient.invalidateQueries({ queryKey: ['quality-task', taskId] })
        queryClient.invalidateQueries({ queryKey: ['quality-task-results', taskId] })
        queryClient.invalidateQueries({ queryKey: ['quality-tasks'] })
      }
    }
    return () => es.close()
  }, [taskId, enabled, queryClient])

  return live
}
