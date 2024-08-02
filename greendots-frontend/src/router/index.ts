import { createRouter, createWebHistory } from 'vue-router';
import HomeView from '@/views/HomeView.vue';
import TestResultsView from '@/views/TestResultsView.vue';
import TestLogsView from '@/views/TestLogsView.vue';
import ProjectRunsView from '@/views/ProjectRunsView.vue';

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/',
      name: 'home',
      component: HomeView
    },
    {
      path: '/:project',
      name: 'project',
      component: ProjectRunsView
    },
    {
      path: '/:project/:run',
      name: 'run',
      component: TestResultsView
    },
    {
      path: '/:project/:run/:test',
      name: 'test_logs',
      component: TestLogsView
    }
  ],
  scrollBehavior(to, from, savedPosition) {
    if (savedPosition) {
      return savedPosition;
    } else {
      return { top: 0 };
    }
  }
});

export default router;
