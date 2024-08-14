<script setup lang="ts">
import { TestDataFetcher } from '@/controllers/TestDataController';
import { computed, inject, ref } from 'vue';
import ProjectCard from './ProjectCard.vue';

const pinnedProjectIds = ref((localStorage.getItem('pinnedProjects') || null)?.split('\x00') || []);

const test_data = inject<TestDataFetcher>('test_data')!;
const projects = await test_data.getProjectsList();
const pinnedProjects = computed(() =>
  projects.filter((project) => pinnedProjectIds.value.includes(project.id))
);
const unpinnedProjects = computed(() =>
  projects.filter((project) => !pinnedProjectIds.value.includes(project.id))
);

function pinProject(projectId: string) {
  if (pinnedProjectIds.value.includes(projectId)) {
    return;
  }
  pinnedProjectIds.value.push(projectId);
  localStorage.setItem('pinnedProjects', pinnedProjectIds.value.join('\x00'));
}

function unpinProject(projectId: string) {
  pinnedProjectIds.value = pinnedProjectIds.value.filter((id) => id !== projectId);
  localStorage.setItem('pinnedProjects', pinnedProjectIds.value.join('\x00'));
}
</script>

<template>
  <main>
    <div class="projects pinned-projects">
      <ProjectCard
        v-for="project in pinnedProjects"
        :key="project.id"
        :project="project"
        :pinned="true"
        @pin="pinProject(project.id)"
        @unpin="unpinProject(project.id)"
      />
    </div>
    <hr v-if="pinnedProjects.length > 0 && unpinnedProjects.length > 0" />
    <div class="projects unpinned-projects">
      <ProjectCard
        v-for="project in unpinnedProjects"
        :key="project.id"
        :project="project"
        :pinned="false"
        @pin="pinProject(project.id)"
        @unpin="unpinProject(project.id)"
      />
    </div>
  </main>
</template>

<style scoped>
main {
  max-width: calc(400px * 3 + 32px * 3);
  margin: 0 auto;
  margin-bottom: 3rem;
}
.projects {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  /* display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); */
  padding: 0 32px;
  gap: 16px;
}
hr {
  border-color: #656589;
  margin: 2rem 0;
}
h2 {
  margin-top: 0;
}
</style>
