<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  tone?: 'neutral' | 'success' | 'danger' | 'warning'
  label: string
}>()

const toneClass = computed(() => `badge--${props.tone ?? 'neutral'}`)
</script>

<template>
  <span class="badge" :class="toneClass">{{ label }}</span>
</template>
