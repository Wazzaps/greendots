<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue';
import { useRouter } from 'vue-router';

const iframe = ref<HTMLIFrameElement | null>(null);
const router = useRouter();
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
    } else {
      body.classList.add(className);
    }
  }
}

function keydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    router.back();
  }
}

onMounted(() => {
  document.addEventListener('keydown', keydown);
});
onUnmounted(() => {
  document.removeEventListener('keydown', keydown);
});
</script>

<template>
  <nav>
    <span class="project-name">{{ $route.params.project }}</span>
    <span class="run-name">{{ $route.params.run }}</span>
    <span class="test-name">{{ $route.params.test }}</span>
    <div class="spacer"></div>
    <label class="level-toggle"
      ><input type="checkbox" checked @change="(e) => toggleStatus(e, 'd')" />Debug</label
    >
    <label class="level-toggle"
      ><input type="checkbox" checked @change="(e) => toggleStatus(e, 'i')" />Info</label
    >
    <label class="level-toggle"
      ><input type="checkbox" checked @change="(e) => toggleStatus(e, 'w')" />Warn</label
    >
    <label class="level-toggle"
      ><input type="checkbox" checked @change="(e) => toggleStatus(e, 'e')" />Error</label
    >
    <label class="level-toggle"
      ><input type="checkbox" checked @change="(e) => toggleStatus(e, 'c')" />Critical</label
    >
    <label class="level-toggle"
      ><input type="checkbox" checked @change="(e) => toggleStatus(e, 't')" />Timestamps</label
    >
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
