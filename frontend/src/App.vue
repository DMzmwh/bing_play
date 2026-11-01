<template>
  <n-config-provider
    :theme="theme"
    :theme-overrides="themeOverrides"
    :locale="naiveLocale"
    :date-locale="naiveDateLocale"
  >
    <n-message-provider>
      <Layout />
    </n-message-provider>
  </n-config-provider>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import Layout from '@/views/Layout.vue';
import {
  NConfigProvider,
  NMessageProvider,
  darkTheme,
  zhCN,
  zhTW,
  enUS,
  dateZhCN,
  dateZhTW,
  dateEnUS,
} from 'naive-ui';
import type { NDateLocale, NLocale } from 'naive-ui';
import { initI18n, i18nState, loadMessages, t } from '@/i18n';
import { loadSettings, activeAccentColor, activeThemeMode } from '@/settings';

import { Events, Updater } from "@wailsio/runtime";

void initI18n();
void loadSettings();

/** 界面主题来自系统设置，深色使用 Naive UI 深色主题，浅色使用默认主题 */
const theme = computed(() => (activeThemeMode.value === 'dark' ? darkTheme : null));

/** 语言代码 -> Naive UI 内置组件文案（分页、表格空态、确认框等） */
const naiveLocaleMap: Record<string, [NLocale, NDateLocale]> = {
  'zh-cn': [zhCN, dateZhCN],
  'zh-tw': [zhTW, dateZhTW],
  en: [enUS, dateEnUS],
};

/** 跟随当前语言，未匹配到语言包时回退英文 */
const naiveLocale = computed(
  () => naiveLocaleMap[i18nState.lang.toLowerCase()]?.[0] ?? enUS
);
const naiveDateLocale = computed(
  () => naiveLocaleMap[i18nState.lang.toLowerCase()]?.[1] ?? dateEnUS
);

const cleanup = Events.On(
    Updater.Events.UpdateAvailable,
    (event) => {
        console.log('update available:', event.data.version);
        // 事件可能早于语言包/设置加载完成，等加载后再取文案
        void Promise.all([loadMessages(), loadSettings()]).then(() => {
            alert(t('update.available', { Version: event.data.version }))
        })
    }
);

const themeOverrides = computed(() => ({
  // 强调色来自系统设置，修改颜色时可实时预览
  Common: {
    primaryColor: activeAccentColor.value,
    primaryColorHover: activeAccentColor.value,
    primaryColorPressed: activeAccentColor.value,
    primaryColorSuppl: activeAccentColor.value,
    borderRadius: '6px',
  },
  // 表单控件统一使用页面语义色，避免 Naive 默认灰黑色割裂
  Input: {
    color: 'var(--app-surface)',
    colorFocus: 'var(--app-surface)',
    textColor: 'var(--app-text-strong)',
    placeholderColor: 'var(--app-text-dim)',
    caretColor: 'var(--app-accent, #3b82f6)',
    border: '1px solid var(--app-border)',
    borderHover: '1px solid var(--app-accent, #3b82f6)',
    borderFocus: '1px solid var(--app-accent, #3b82f6)',
    boxShadowFocus: 'none',
    borderRadius: '6px',
  },
  Select: {
    peers: {
      InternalSelection: {
        color: 'var(--app-surface)',
        colorActive: 'var(--app-surface)',
        textColor: 'var(--app-text-strong)',
        placeholderColor: 'var(--app-text-dim)',
        border: '1px solid var(--app-border)',
        borderHover: '1px solid var(--app-accent, #3b82f6)',
        borderActive: '1px solid var(--app-accent, #3b82f6)',
        borderFocus: '1px solid var(--app-accent, #3b82f6)',
        boxShadowFocus: 'none',
        arrowColor: 'var(--app-text-muted)',
        borderRadius: '6px',
      },
      InternalSelectMenu: {
        color: 'var(--app-surface)',
        optionTextColor: 'var(--app-text-muted)',
        optionTextColorActive: 'var(--app-accent, #3b82f6)',
        optionColorPending: 'var(--app-hover)',
        optionColorActive: 'transparent',
        optionColorActivePending: 'var(--app-hover)',
        borderRadius: '6px',
      },
    },
  },
  // 气泡层与下拉统一语义色
  Popover: {
    color: 'var(--app-surface)',
    textColor: 'var(--app-text-strong)',
    boxShadow: 'var(--app-shadow)',
  },
  Dropdown: {
    color: 'var(--app-hover)',
    optionTextColor: 'var(--app-text-muted)',
    optionTextColorHover: 'var(--app-text)',
    optionTextColorActive: 'var(--app-accent, #3b82f6)',
    optionColorHover: 'var(--app-active-row)',
    optionColorActive: 'transparent',
    dividerColor: 'var(--app-border)',
    borderRadius: '6px',
    peers: {
      Popover: {
        color: 'var(--app-hover)',
        dividerColor: 'var(--app-border)',
        boxShadow: 'var(--app-shadow)',
      },
    },
  },
  // 表格：表头、行、hover、边框全部对齐主题变量
  DataTable: {
    tdColor: 'var(--app-panel)',
    tdColorHover: 'var(--app-hover-row)',
    tdColorSorting: 'var(--app-hover-row)',
    thColor: 'var(--app-table-header)',
    thColorHover: 'var(--app-hover)',
    thColorSorting: 'var(--app-hover)',
    tdTextColor: 'var(--app-text-strong)',
    thTextColor: 'var(--app-text-muted)',
    borderColor: 'var(--app-border-soft)',
    thFontWeight: '500',
  },
}))
</script>
<style>
</style>
