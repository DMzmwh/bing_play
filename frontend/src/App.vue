<template>
  <n-config-provider :theme-overrides="themeOverrides">
    <Layout />
  </n-config-provider>
</template>

<script setup lang="ts">
import Layout from '@/views/Layout.vue';
import { NConfigProvider } from 'naive-ui';



const themeOverrides = {
  Input: {
    textColor: '#fff',
  }
}
</script>
<style>
</style>