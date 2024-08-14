<script setup lang="ts">
import type { TestDataFetcher } from '@/controllers/TestDataController';
import { inject } from 'vue';
import { useRoute } from 'vue-router';

const route = useRoute();
const test_data = inject<TestDataFetcher>('test_data')!;
const project_runs = await test_data.getProjectRuns(route.params.project as string);
</script>

<template>
  <main>
    <ul>
      <li class="run-row" v-for="run in project_runs" :key="run.id">
        <RouterLink class="run-name" :to="`/${$route.params.project}/${run.id}`">{{
          run.metadata?.name || run.id
        }}</RouterLink>
        <span class="run-age" :title="run.created_at">{{ run.pretty_age }}</span>
      </li>
    </ul>
  </main>
</template>

<style scoped>
main a {
  color: #ffffff;
}
.run-age {
  color: rgb(131, 131, 168);
}
ul {
  padding-inline-start: 1.5em;
}
li {
  list-style: none;
  display: flex;
  gap: 12px;
}
</style>
