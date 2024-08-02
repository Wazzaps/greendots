<script setup lang="ts">
import { useProjectRuns } from '@/controllers/TestDataController';
import { useRoute } from 'vue-router';

const route = useRoute();
const project_runs = await useProjectRuns(route.params.project as string);
</script>

<template>
  <main>
    <ul>
      <li class="run-row" v-for="run in project_runs" :key="run.id">
        <RouterLink class="run-name" :to="`/${$route.params.project}/${run.id}`">{{
          run.name
        }}</RouterLink>
        &nbsp;|&nbsp;
        <span class="run-age" :title="run.created_at">{{ run.pretty_age }}</span>
      </li>
    </ul>
  </main>
</template>

<style scoped>
main a {
  color: #ffffff;
}
</style>
