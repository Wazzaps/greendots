<script setup lang="ts">
import ProjectsGrid from '@/components/ProjectsGrid.vue';
import logo from '@/assets/logo.svg';
import { ref } from 'vue';
const docs_url = ref('/api/v1/');
// const source_code_url = ref('javascript:');
</script>

<template>
  <div class="logo"><img :src="logo" /><span>GreenDots</span></div>
  <nav>
    <a :href="docs_url">Docs</a>
    <a href="/api/v1/config">Config</a>
    <a href="/api/v1/version">Build info</a>
    <!-- <a :href="source_code_url">Source code</a> -->
  </nav>
  <Suspense>
    <ProjectsGrid class="projects-container" />
    <template #fallback>
      <div class="loading-projects">Loading projects...</div>
    </template>
  </Suspense>
</template>

<style scoped>
.logo {
  display: flex;
  font-size: 4rem;
  text-align: center;
  font-family: 'Jockey One', 'Exo 2', sans-serif;
  color: #d5ffc6;
  margin-top: 4rem;
  gap: 8px;
  justify-content: center;
}
.loading-projects {
  text-align: center;
}
.logo span {
  line-height: 4.3rem;
}
.logo img {
  padding-top: 5px;
}
nav {
  display: flex;
  justify-content: center;
  gap: 1rem;
  margin-bottom: 3rem;
}
nav a {
  color: #aeaeae;
}
</style>
