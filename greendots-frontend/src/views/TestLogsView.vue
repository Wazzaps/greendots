<template>
  <nav>
    <span class="project-name">{{ $route.params.project }}</span>
    <span class="run-name">{{ $route.params.run }}</span>
    <span class="test-name">{{ $route.params.test }}</span>
    <!-- <span>TODO:Search</span>
    <span>TODO:Statistics</span>
    <span>TODO:Settings</span> -->
  </nav>
  <iframe
    class="logs"
    :src="`/api/v1/projects/${$route.params.project}/runs/${$route.params.run}/test/${$route.params.test}/log_stream`"
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
