<template>
  <n-config-provider :theme-overrides="themeOverrides">
    <Layout />
  </n-config-provider>
</template>

<script setup lang="ts">
import Layout from '@/views/Layout.vue';
import { NConfigProvider } from 'naive-ui';

import { Events, Updater } from "@wailsio/runtime";

const cleanup = Events.On(
    Updater.Events.UpdateAvailable,
    (event) => {
        console.log("发现新版本:", event.data.version);
        alert("发现新版本：" + event.data.version)
    }
);
const themeOverrides = {
  Input: {
    textColor: '#fff',
  }
}
</script>
<style>
</style>