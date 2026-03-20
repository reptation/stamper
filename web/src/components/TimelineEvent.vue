<script setup lang="ts">
import { computed } from 'vue'

import { formatTimestamp } from '@/lib/format'
import type { Event } from '@/lib/types'

import PayloadViewer from './PayloadViewer.vue'
import StatusBadge from './StatusBadge.vue'

const props = defineProps<{
  event: Event
}>()

const payload = computed(() => props.event.payload)

const title = computed(() => {
  const currentPayload = payload.value

  switch (props.event.event_type) {
    case 'reasoning':
      return String(currentPayload.summary ?? 'Reasoning')
    case 'tool_call':
      return `Tool call: ${String(currentPayload.tool_name ?? 'unknown')}`
    case 'tool_requested':
      return `Tool requested: ${String(currentPayload.tool_name ?? 'unknown')}`
    case 'policy_decision':
      if (currentPayload.decision === 'deny') return 'Denied by policy'
      if (currentPayload.decision === 'allow') return 'Allowed by policy'
      if (currentPayload.decision === 'require_approval') return 'Approval required by policy'
      return 'Policy decision'
    case 'execution_blocked':
      return 'Execution blocked'
    case 'execution_result':
      return 'Execution result'
    case 'tool_executed':
      return `Tool executed: ${String(currentPayload.tool_name ?? 'unknown')}`
    case 'tool_failed':
      return `Tool failed: ${String(currentPayload.tool_name ?? 'unknown')}`
    case 'run_finished':
      return `Run ${String(currentPayload.status ?? 'finished')}`
    default:
      return 'Event recorded'
  }
})

const summary = computed(() => {
  const currentPayload = payload.value

  switch (props.event.event_type) {
    case 'reasoning':
      return 'The agent recorded its intent before taking action.'
    case 'tool_call':
      return 'The agent attempted a tool invocation.'
    case 'tool_requested':
      return 'The governed tool requested policy evaluation before execution.'
    case 'policy_decision':
      return String(currentPayload.rationale ?? currentPayload.reason ?? 'A policy decision was recorded.')
    case 'execution_blocked':
      return String(currentPayload.reason ?? 'Execution blocked')
    case 'execution_result':
      return String(currentPayload.summary ?? currentPayload.status ?? 'Execution result recorded')
    case 'tool_executed':
      return String(currentPayload.summary ?? `The governed tool completed with status ${String(currentPayload.status_code ?? 'unknown')}.`)
    case 'tool_failed':
      return String(currentPayload.error ?? 'The governed tool failed during execution.')
    case 'run_finished':
      return String(currentPayload.output_summary ?? 'Run finished')
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
  if (props.event.event_type === 'tool_executed') return 'success'
  if (props.event.event_type === 'tool_failed') return 'danger'
  if (props.event.event_type === 'policy_decision' && props.event.payload.decision === 'deny') return 'danger'
  if (props.event.event_type === 'policy_decision' && props.event.payload.decision === 'require_approval') return 'warning'
  if (props.event.event_type === 'run_finished' && props.event.payload.status === 'failed') return 'danger'
  if (props.event.event_type === 'run_finished' && props.event.payload.status === 'completed') return 'success'
  return 'neutral'
})

const toolCallDetails = computed(() => {
  if (props.event.event_type !== 'tool_call' && props.event.event_type !== 'tool_requested') return []

  const argumentsValue = (payload.value.arguments as Record<string, unknown> | undefined) ?? {}

  return [
    { label: 'Tool', value: String(payload.value.tool_name ?? 'unknown') },
    { label: 'Method', value: String(argumentsValue.method ?? '—') },
    { label: 'URL', value: String(argumentsValue.url ?? '—') },
  ]
})

const policyDecisionDetails = computed(() => {
  if (props.event.event_type !== 'policy_decision') return []

  return [
    { label: 'Policy ID', value: String(payload.value.policy_id ?? '—') },
    { label: 'Rationale', value: String(payload.value.rationale ?? payload.value.reason ?? '—') },
  ]
})

const executionDetails = computed(() => {
  if (props.event.event_type !== 'tool_executed' && props.event.event_type !== 'tool_failed') return []

  const details = [{ label: 'Tool', value: String(payload.value.tool_name ?? 'unknown') }]

  if (props.event.event_type === 'tool_executed') {
    details.push({ label: 'Status Code', value: String(payload.value.status_code ?? '—') })
  }

  if (props.event.event_type === 'tool_failed') {
    details.push({ label: 'Error', value: String(payload.value.error ?? '—') })
  }

  return details
})

const runFinishedDetails = computed(() => {
  if (props.event.event_type !== 'run_finished') return []

  return [
    { label: 'Status', value: String(payload.value.status ?? '—') },
    { label: 'Summary', value: String(payload.value.output_summary ?? '—') },
  ]
})

const shouldShowPayload = computed(() => props.event.event_type !== 'reasoning')
</script>

<template>
  <article class="timeline-event panel" :class="`timeline-event--${event.event_type}`">
    <div class="timeline-event__header">
      <div class="timeline-event__sequence">#{{ event.sequence }}</div>
      <div class="timeline-event__heading">
        <div class="timeline-event__topline">
          <h3>{{ title }}</h3>
          <StatusBadge :label="badgeLabel" :tone="badgeTone" />
        </div>
        <p class="timeline-event__meta">{{ formatTimestamp(event.created_at) }}</p>
        <p class="timeline-event__summary">{{ summary }}</p>
      </div>
    </div>

    <dl v-if="toolCallDetails.length > 0" class="timeline-event__details">
      <div v-for="detail in toolCallDetails" :key="detail.label">
        <dt>{{ detail.label }}</dt>
        <dd>{{ detail.value }}</dd>
      </div>
    </dl>

    <dl v-if="policyDecisionDetails.length > 0" class="timeline-event__details">
      <div v-for="detail in policyDecisionDetails" :key="detail.label">
        <dt>{{ detail.label }}</dt>
        <dd>{{ detail.value }}</dd>
      </div>
    </dl>

    <dl v-if="executionDetails.length > 0" class="timeline-event__details">
      <div v-for="detail in executionDetails" :key="detail.label">
        <dt>{{ detail.label }}</dt>
        <dd>{{ detail.value }}</dd>
      </div>
    </dl>

    <dl v-if="runFinishedDetails.length > 0" class="timeline-event__details">
      <div v-for="detail in runFinishedDetails" :key="detail.label">
        <dt>{{ detail.label }}</dt>
        <dd>{{ detail.value }}</dd>
      </div>
    </dl>

    <PayloadViewer v-if="shouldShowPayload" :payload="event.payload" label="Show event payload" />
  </article>
</template>
