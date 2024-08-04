<script setup lang="ts">
import { TestDataController } from '@/controllers/TestDataController';
import { inject } from 'vue';

const test_data = inject<TestDataController>('test_data')!;
const projects = await test_data.getProjectsList();
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
main {
  display: flex;
  flex-direction: column;
  max-width: 400px;
  margin: 0 auto;
}
main a {
  color: #ffffff;
}
h2 {
  margin-top: 0;
}
.project {
  background: #151519;
  padding: 1rem;
  padding-top: 0.5rem;
  border-radius: 8px;
  margin-bottom: 1rem;
  box-shadow: 0 8px 16px rgba(21, 21, 25, 0.6);
}
</style>
