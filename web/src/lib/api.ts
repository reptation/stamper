import type { RunDetailResponse, RunsResponse } from './types'

const API_BASE_URL = (import.meta.env.VITE_API_BASE_URL as string | undefined)?.replace(/\/$/, '') || 'http://127.0.0.1:8080'

async function apiGet<T>(path: string): Promise<T> {
  const response = await fetch(`${API_BASE_URL}${path}`, {
    headers: {
      Accept: 'application/json',
    },
  })

  if (!response.ok) {
    let message = `Request failed with status ${response.status}`

    try {
      const body = (await response.json()) as { error?: string }
      if (body.error) {
        message = body.error
      }
    } catch {
      // Ignore JSON parse errors and use the fallback message.
    }

    throw new Error(message)
  }

  return (await response.json()) as T
}

export function getRuns() {
  return apiGet<RunsResponse>('/v1/runs')
}

export function getRunDetail(runID: string) {
  return apiGet<RunDetailResponse>(`/v1/runs/${runID}`)
}
