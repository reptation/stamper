<script setup lang="ts">
import { computed } from 'vue'

import { formatTimestamp } from '@/lib/format'
import type { Run } from '@/lib/types'

import StatusBadge from './StatusBadge.vue'

const props = defineProps<{
  run: Run
}>()

const statusTone = computed(() => {
  if (props.run.status === 'completed') return 'success'
  if (props.run.status === 'failed') return 'danger'
  return 'warning'
})
</script>

<template>
  <section class="panel metadata-card">
    <div class="panel-heading">
      <div>
        <p class="eyebrow">Run Detail</p>
        <h2>{{ run.task }}</h2>
      </div>
      <StatusBadge :label="run.status" :tone="statusTone" />
    </div>

    <dl class="metadata-grid">
      <div>
        <dt>Run ID</dt>
        <dd>{{ run.run_id }}</dd>
      </div>
      <div>
        <dt>Agent</dt>
        <dd>{{ run.agent_id }}</dd>
      </div>
      <div>
        <dt>Environment</dt>
        <dd>{{ run.environment }}</dd>
      </div>
      <div>
        <dt>Started</dt>
        <dd>{{ formatTimestamp(run.started_at) }}</dd>
      </div>
      <div>
        <dt>Finished</dt>
        <dd>{{ formatTimestamp(run.finished_at) }}</dd>
      </div>
    </dl>
  </section>
</template>
