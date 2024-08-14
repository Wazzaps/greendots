<script setup lang="ts">
import { RouterView } from 'vue-router';
import { provide } from 'vue';
import { TestDataFetcher } from './controllers/TestDataController';
provide('test_data', new TestDataFetcher());
</script>

<template>
  <RouterView />
</template>
