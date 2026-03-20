<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'

import RunMetadataCard from '@/components/RunMetadataCard.vue'
import TimelineEvent from '@/components/TimelineEvent.vue'
import { getRunDetail } from '@/lib/api'
import type { Event, Run } from '@/lib/types'

const props = defineProps<{
  id: string
}>()

const run = ref<Run | null>(null)
const events = ref<Event[]>([])
const loading = ref(true)
const error = ref('')

const title = computed(() => run.value?.task ?? 'Run detail')

async function loadRunDetail() {
  loading.value = true
  error.value = ''

  try {
    const response = await getRunDetail(props.id)
    run.value = response.run
    events.value = response.events
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Failed to load run'
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void loadRunDetail()
})

watch(
  () => props.id,
  () => {
    void loadRunDetail()
  },
)
</script>

<template>
  <section class="page-stack">
    <RouterLink class="back-link" to="/runs">Back to runs</RouterLink>

    <div class="page-heading">
      <div>
        <p class="eyebrow">Timeline</p>
        <h2>{{ title }}</h2>
      </div>
      <button class="secondary-button" type="button" @click="loadRunDetail">Refresh</button>
    </div>

    <div v-if="loading" class="panel state-card">Loading run...</div>
    <div v-else-if="error" class="panel state-card state-card--error">Failed to load run: {{ error }}</div>
    <template v-else-if="run">
      <RunMetadataCard :run="run" />

      <section class="page-stack">
        <div class="section-heading">
          <h3>Timeline</h3>
          <p>{{ events.length }} event{{ events.length === 1 ? '' : 's' }}</p>
        </div>

        <div v-if="events.length === 0" class="panel state-card">No events found</div>
        <div v-else class="timeline-list">
          <TimelineEvent v-for="event in events" :key="event.id" :event="event" />
        </div>
      </section>
    </template>
  </section>
</template>
