<script setup lang="ts">
defineProps<{ pinned: boolean; project: any }>();
defineEmits(['pin', 'unpin']);
</script>

<template>
  <div class="project">
    <h2>
      <RouterLink :to="`/${project.id}`">{{ project.metadata?.name || project.id }}</RouterLink>
      <button @click="pinned ? $emit('unpin') : $emit('pin')" :class="{ activated: pinned }">
        <img src="@/assets/pin.svg" />
      </button>
    </h2>
    <ul>
      <li class="run-row" v-for="run in project.runs" :key="run.id">
        <RouterLink class="run-name" :to="`/${project.id}/${run.id}`">{{
          run.metadata?.name || run.id
        }}</RouterLink>
        <span class="run-age" :title="run.created_at">{{ run.pretty_age }}</span>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.project {
  min-width: 300px;
  background: #151519;
  padding: 1rem;
  padding-top: 4px;
  padding-right: 0;
  border-top: 3px solid rgb(59, 59, 87);
  border-radius: 8px;
  box-shadow: 0 8px 16px rgba(21, 21, 25, 0.6);
}
.run-age {
  color: rgb(131, 131, 168);
}
a {
  color: #ffffff;
}
ul {
  padding-inline-start: 1em;
  padding-inline-end: 1rem;
}
li {
  list-style: none;
  display: flex;
  gap: 12px;
}
h2 {
  display: flex;
  justify-content: space-between;
  height: 44px;
  font-size: 28px;
}
button {
  background: transparent;
  border: none;
  width: 60px;
  opacity: 0.2;
  transition: opacity 0.2s;
  cursor: pointer;
}
button:hover {
  opacity: 0.5;
}
button * {
  -webkit-user-select: none;
  -khtml-user-select: none;
  -moz-user-select: none;
  -o-user-select: none;
  user-select: none;
  -webkit-user-drag: none;
  -khtml-user-drag: none;
  -moz-user-drag: none;
  -o-user-drag: none;
  user-drag: none;
}
button.activated {
  opacity: 1;
}
</style>
