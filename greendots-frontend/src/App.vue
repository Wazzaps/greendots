<script setup lang="ts">
import { RouterView } from 'vue-router';
import { provide } from 'vue';
import { TestDataController } from './controllers/TestDataController';
provide('test_data', new TestDataController());
// provide('test_data', new TestTestDataController());
</script>

<template>
  <RouterView />
</template>
