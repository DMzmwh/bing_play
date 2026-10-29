<template>
  <n-config-provider :theme-overrides="themeOverrides">
    <Layout />
  </n-config-provider>
</template>

<script setup lang="ts">
import Layout from '@/views/Layout.vue';
import { NConfigProvider } from 'naive-ui';
import { initI18n, loadMessages, t } from '@/i18n';

import { Events, Updater } from "@wailsio/runtime";

void initI18n();

const cleanup = Events.On(
    Updater.Events.UpdateAvailable,
    (event) => {
        console.log('update available:', event.data.version);
        // 事件可能早于语言包加载完成，等加载后再取文案
        void loadMessages().then(() => {
            alert(t('update.available', { Version: event.data.version }))
        })
    }
);
const themeOverrides = {
  Input: {
    textColor: '#fff',
  },
  // 顶部语言切换下拉，与标题栏深色风格保持一致
  Dropdown: {
    color: '#1a2430',
    optionTextColor: '#aaa',
    optionTextColorHover: '#fff',
    optionTextColorActive: '#60a5fa',
    optionColorHover: '#243040',
    optionColorActive: 'rgba(59, 130, 246, 0.12)',
    dividerColor: '#1d2733',
    borderRadius: '6px',
    peers: {
      Popover: {
        color: '#1a2430',
        dividerColor: '#1d2733',
        boxShadow: '0 6px 20px rgba(0, 0, 0, 0.45)',
      },
    },
  },
}
</script>
<style>
</style>