<script setup lang="ts">
import { computed } from 'vue'

import { formatTimestamp } from '@/lib/format'
import type { Event } from '@/lib/types'

import PayloadViewer from './PayloadViewer.vue'
import StatusBadge from './StatusBadge.vue'

const props = defineProps<{
  event: Event
}>()

const summary = computed(() => {
  const payload = props.event.payload

  switch (props.event.event_type) {
    case 'reasoning':
      return String(payload.summary ?? 'Reasoning step recorded')
    case 'tool_call':
      return `Tool: ${String(payload.tool_name ?? 'unknown')}`
    case 'policy_decision':
      return `Decision: ${String(payload.decision ?? 'unknown')}`
    case 'execution_blocked':
      return String(payload.reason ?? 'Execution blocked')
    case 'execution_result':
      return String(payload.summary ?? payload.status ?? 'Execution result recorded')
    case 'run_finished':
      return `Finished with ${String(payload.status ?? 'unknown')} status`
    default:
      return 'Event recorded'
  }
})

const badgeLabel = computed(() => {
  if (props.event.event_type === 'policy_decision' && props.event.payload.decision) {
    return String(props.event.payload.decision)
  }
  if (props.event.event_type === 'run_finished' && props.event.payload.status) {
    return String(props.event.payload.status)
  }

  return props.event.event_type
})

const badgeTone = computed<'neutral' | 'success' | 'danger' | 'warning'>(() => {
  if (props.event.event_type === 'execution_blocked') return 'danger'
  if (props.event.event_type === 'policy_decision' && props.event.payload.decision === 'deny') return 'danger'
  if (props.event.event_type === 'policy_decision' && props.event.payload.decision === 'require_approval') return 'warning'
  if (props.event.event_type === 'run_finished' && props.event.payload.status === 'failed') return 'danger'
  if (props.event.event_type === 'run_finished' && props.event.payload.status === 'completed') return 'success'
  return 'neutral'
})
</script>

<template>
  <article class="timeline-event panel">
    <div class="timeline-event__header">
      <div class="timeline-event__sequence">#{{ event.sequence }}</div>
      <div class="timeline-event__heading">
        <div class="timeline-event__topline">
          <h3>{{ summary }}</h3>
          <StatusBadge :label="badgeLabel" :tone="badgeTone" />
        </div>
        <p class="timeline-event__meta">{{ formatTimestamp(event.created_at) }}</p>
      </div>
    </div>

    <dl v-if="event.event_type === 'policy_decision'" class="timeline-event__details">
      <div>
        <dt>Policy ID</dt>
        <dd>{{ String(event.payload.policy_id ?? '—') }}</dd>
      </div>
      <div>
        <dt>Rationale</dt>
        <dd>{{ String(event.payload.rationale ?? '—') }}</dd>
      </div>
    </dl>

    <PayloadViewer :payload="event.payload" />
  </article>
</template>
