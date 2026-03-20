export function formatTimestamp(value?: string | null) {
  if (!value) {
    return '—'
  }

  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(new Date(value))
}

export function formatDuration(startedAt?: string | null, finishedAt?: string | null) {
  if (!startedAt || !finishedAt) {
    return '—'
  }

  const durationMs = new Date(finishedAt).getTime() - new Date(startedAt).getTime()
  if (Number.isNaN(durationMs) || durationMs < 0) {
    return '—'
  }

  if (durationMs < 1000) {
    return `${durationMs} ms`
  }

  const seconds = durationMs / 1000
  if (seconds < 60) {
    return `${seconds.toFixed(seconds < 10 ? 1 : 0)} s`
  }

  const minutes = Math.floor(seconds / 60)
  const remainingSeconds = Math.round(seconds % 60)
  return `${minutes}m ${remainingSeconds}s`
}
