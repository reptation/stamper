<script setup lang="ts">
import { useRouter } from 'vue-router'

import { formatTimestamp } from '@/lib/format'
import type { Run } from '@/lib/types'

import StatusBadge from './StatusBadge.vue'

defineProps<{
  runs: Run[]
}>()

const router = useRouter()

function statusTone(status: Run['status']) {
  if (status === 'completed') return 'success'
  if (status === 'failed') return 'danger'
  return 'warning'
}

function openRun(runID: string) {
  void router.push(`/runs/${runID}`)
}
</script>

<template>
  <div class="panel table-panel">
    <table class="run-table">
      <thead>
        <tr>
          <th>Run ID</th>
          <th>Agent</th>
          <th>Environment</th>
          <th>Task</th>
          <th>Status</th>
          <th>Started</th>
          <th>Finished</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="run in runs"
          :key="run.run_id"
          class="run-row"
          tabindex="0"
          @click="openRun(run.run_id)"
          @keydown.enter.prevent="openRun(run.run_id)"
          @keydown.space.prevent="openRun(run.run_id)"
        >
          <td class="run-table__mono">{{ run.run_id }}</td>
          <td>{{ run.agent_id }}</td>
          <td>{{ run.environment }}</td>
          <td>{{ run.task }}</td>
          <td>
            <StatusBadge :label="run.status" :tone="statusTone(run.status)" />
          </td>
          <td>{{ formatTimestamp(run.started_at) }}</td>
          <td>{{ formatTimestamp(run.finished_at) }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
