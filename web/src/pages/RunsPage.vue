<script setup lang="ts">
import { onMounted, ref } from 'vue'

import RunTable from '@/components/RunTable.vue'
import { getRuns } from '@/lib/api'
import type { Run } from '@/lib/types'

const runs = ref<Run[]>([])
const loading = ref(true)
const error = ref('')

async function loadRuns() {
  loading.value = true
  error.value = ''

  try {
    const response = await getRuns()
    runs.value = response.runs
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Failed to load runs'
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void loadRuns()
})
</script>

<template>
  <section class="page-stack">
    <div class="page-heading">
      <div>
        <p class="eyebrow">Runs</p>
        <h2>All runs</h2>
      </div>
      <button class="secondary-button" type="button" @click="loadRuns">Refresh</button>
    </div>

    <div v-if="loading" class="panel state-card">Loading runs...</div>
    <div v-else-if="error" class="panel state-card state-card--error">{{ error }}</div>
    <div v-else-if="runs.length === 0" class="panel state-card">No runs found</div>
    <RunTable v-else :runs="runs" />
  </section>
</template>
