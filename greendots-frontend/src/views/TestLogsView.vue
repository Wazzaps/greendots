<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';

const iframe = ref<HTMLIFrameElement | null>(null);
const router = useRouter();
const route = useRoute();

const labels = {
  d: 'Debug',
  i: 'Info',
  w: 'Warn',
  e: 'Error',
  c: 'Critical',
  t: 'Timestamps'
};

let hidden_log_levels: string[] = JSON.parse(localStorage.getItem('hidden_log_levels') || '[]');

function toggleStatus(e: Event, level: string) {
  if (!iframe.value) {
    return;
  }
  const checked = (e.target! as HTMLInputElement).checked;
  const className = 'hide-' + level;
  const body = iframe.value.contentWindow?.document.body;
  if (body) {
    if (checked) {
      body.classList.remove(className);
      hidden_log_levels = hidden_log_levels.filter((l) => l !== level);
    } else {
      body.classList.add(className);
      hidden_log_levels.push(level);
    }
  }
  localStorage.setItem('hidden_log_levels', JSON.stringify(hidden_log_levels));
}

function keydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    router.back();
  }
}

function onMessage(e: MessageEvent) {
  if (e.origin === window.location.origin && e.data === 'close-logs-view') {
    router.back();
  }
}

onMounted(() => {
  document.addEventListener('keydown', keydown);
  window.addEventListener('message', onMessage);
  document.title = `${route.params.test} (${route.params.run} - ${route.params.project}) · GreenDots`;
});
onUnmounted(() => {
  document.removeEventListener('keydown', keydown);
  window.removeEventListener('message', onMessage);
});
</script>

<template>
  <nav>
    <span class="project-name">{{ $route.params.project }}</span>
    <span class="run-name">{{ $route.params.run }}</span>
    <span class="test-name">{{ $route.params.test }}</span>
    <div class="spacer"></div>
    <label class="level-toggle" v-for="level in ['d', 'i', 'w', 'e', 'c', 't']" :key="level">
      <input
        type="checkbox"
        :checked="!hidden_log_levels.includes(level)"
        @change="(e) => toggleStatus(e, level)"
      />{{ labels[level] }}
    </label>
    <!-- <span>TODO:Search</span>
    <span>TODO:Statistics</span>
    <span>TODO:Settings</span> -->
  </nav>
  <iframe
    class="logs"
    ref="iframe"
    :src="`/api/v1/projects/${encodeURIComponent($route.params.project)}/runs/${encodeURIComponent($route.params.run)}/test/${encodeURIComponent($route.params.test)}/log_stream`"
  ></iframe>
</template>

<style scoped>
nav {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  height: 32px;
  display: flex;
  justify-content: start;
  gap: 1rem;
  background: #121212;
  font-size: 18px;
  z-index: 100;
  padding: 0 32px;
}
nav .test-name {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.spacer {
  flex-grow: 1;
}
.level-toggle {
  display: flex;
  gap: 6px;
}
nav a {
  color: #aeaeae;
}
.logs {
  position: fixed;
  top: 32px;
  left: 0;
  right: 0;
  bottom: 0;
  width: 100%;
  height: calc(100% - 32px);
  border: none;
}
</style>
