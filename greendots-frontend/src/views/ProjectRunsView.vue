<script setup lang="ts">
import ProjectRuns from '@/components/ProjectRuns.vue';
</script>

<template>
  <nav>
    <span class="project-name">Project 1</span>
    <span>TODO:Settings</span>
  </nav>
  <Suspense>
    <ProjectRuns class="project-runs-container" />
    <template #fallback>
      <div>Loading project runs...</div>
    </template>
  </Suspense>
</template>

<style scoped>
nav {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  height: 32px;
  display: flex;
  justify-content: space-around;
  gap: 1rem;
  background: #121212;
  font-size: 18px;
  z-index: 100;
}
nav a {
  color: #aeaeae;
}
main {
  margin-top: 32px;
  padding: 8px;
  white-space: pre-wrap;
  font-family: monospace;
}
</style>
