<script setup lang="ts">
import ProjectsGrid from '@/components/ProjectsGrid.vue';
</script>

<template>
  <div class="logo">GreenDots</div>
  <nav>
    <a href="javascript:void;">Docs</a>
    <RouterLink to="/config">Config</RouterLink>
    <a href="javascript:void;">Source code</a>
  </nav>
  <Suspense>
    <ProjectsGrid class="projects-container" />
    <template #fallback>
      <div>Loading projects...</div>
    </template>
  </Suspense>
</template>

<style scoped>
.logo {
  display: block;
  font-size: 3rem;
  text-align: center;
  font-family: 'Jockey One', 'Exo 2', sans-serif;
  color: #d5ffc6;
}
nav {
  display: flex;
  justify-content: center;
  gap: 1rem;
}
nav a {
  color: #aeaeae;
}
</style>
