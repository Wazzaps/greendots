<template>
  <nav>
    <span class="project-name">Project 1</span>
    <span class="run-name">Run 1</span>
    <span>TODO:Search</span>
    <span>TODO:Statistics</span>
    <span>TODO:Settings</span>
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
  justify-content: space-around;
  gap: 1rem;
  background: #121212;
  font-size: 18px;
  z-index: 100;
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
