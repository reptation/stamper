<script setup lang="ts">
import { RouterView } from 'vue-router'
</script>

<template>
  <div class="app-shell">
    <header class="topbar">
      <div>
        <p class="eyebrow">Stamper</p>
        <h1>Run Explorer</h1>
      </div>
      <p class="topbar-copy">Inspect run timelines, policy decisions, and execution history.</p>
    </header>

    <main class="page-shell">
      <RouterView />
    </main>
  </div>
</template>
