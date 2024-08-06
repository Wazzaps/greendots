<script setup lang="ts">
import { onMounted } from 'vue';
onMounted(() => {
  document.title = `Not Found · GreenDots`;
});
</script>

<template>
  <main>
    <h1>404 Not Found</h1>
    <h2><RouterLink to="/">Go Home →</RouterLink></h2>
  </main>
</template>

<style scoped>
main {
  display: block;
  margin: 5rem auto;
  max-width: 800px;
}
h1 {
  font-size: 3rem;
  color: #ffffff;
}
h2,
h2 a {
  font-size: 1.5rem;
  color: #ffffff;
}
</style>
