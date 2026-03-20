<script setup lang="ts">
import { computed, ref } from 'vue'

const props = defineProps<{
  payload: Record<string, unknown>
  label?: string
}>()

const expanded = ref(false)

const prettyPayload = computed(() => JSON.stringify(props.payload, null, 2))
</script>

<template>
  <div class="payload-viewer">
    <button class="payload-toggle" type="button" @click="expanded = !expanded">
      {{ expanded ? 'Hide payload' : props.label ?? 'Show payload' }}
    </button>

    <pre v-if="expanded" class="payload-block">{{ prettyPayload }}</pre>
  </div>
</template>
