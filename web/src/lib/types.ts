export type RunStatus = 'running' | 'completed' | 'failed'

export interface Run {
  run_id: string
  agent_id: string
  environment: string
  task: string
  status: RunStatus
  started_at: string
  finished_at?: string | null
}

export type EventType =
  | 'reasoning'
  | 'tool_call'
  | 'tool_requested'
  | 'policy_decision'
  | 'execution_blocked'
  | 'execution_result'
  | 'tool_executed'
  | 'tool_failed'
  | 'run_finished'

export interface Event {
  id: number
  run_id: string
  sequence: number
  event_type: EventType
  payload: Record<string, unknown>
  created_at: string
}

export interface RunsResponse {
  runs: Run[]
}

export interface RunDetailResponse {
  run: Run
  events: Event[]
}
