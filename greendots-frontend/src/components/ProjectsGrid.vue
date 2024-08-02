<script setup lang="ts">
import { useProjectsList } from '@/controllers/TestDataController';

const projects = await useProjectsList();
</script>

<template>
  <main>
    <div class="project" v-for="project in projects" :key="project.id">
      <h2>
        <RouterLink :to="`/${project.id}`">{{ project.name }}</RouterLink>
      </h2>
      <ul>
        <li class="run-row" v-for="run in project.runs" :key="run.id">
          <RouterLink class="run-name" :to="`/${project.id}/${run.id}`">{{ run.name }}</RouterLink>
          &nbsp;|&nbsp;
          <span class="run-age" :title="run.created_at">{{ run.pretty_age }}</span>
        </li>
      </ul>
    </div>
  </main>
</template>

<style scoped>
main a {
  color: #ffffff;
}
</style>
