<template>
    <div class="app">
        <div class="top-bar">
            <!-- 品牌主图标：B / 兵 字形，尺寸 26px，与 50px 导航栏垂直居中 -->
            <BrandIcon class="nav-logo" variant="nav" :size="26" />
            <div class="left-tabs">
                <div
                    v-for="item in tabs"
                    :key="item.path"
                    :class="['tab', route.path === item.path ? 'active' : '']"
                    @click="router.push(item.path)"
                >
                    {{ item.label }}
                </div>
            </div>
            <div
                class="drag-area"
                style="--wails-draggable: drag;"
                @dblclick="toggleMaximise"
            />

            <n-dropdown
                v-if="i18nState.locales.length"
                trigger="click"
                placement="bottom-end"
                :options="langOptions"
                @select="onLangChange"
            >
                <div class="right-icon lang-icon" :title="currentLangLabel">
                    <svg
                        class="globe"
                        viewBox="0 0 24 24"
                        width="16"
                        height="16"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="1.6"
                        stroke-linecap="round"
                    >
                        <circle cx="12" cy="12" r="8.5" />
                        <ellipse cx="12" cy="12" rx="4" ry="8.5" />
                        <path d="M3.5 12h17" />
                    </svg>
                </div>
            </n-dropdown>
            <div class="right-icon" @click="refresh">⟳</div>
            <div class="window-button" @click="minimise">─</div>
            <div class="window-button" @click="toggleMaximise">□</div>
            <div class="window-button close" @click="closeWindow">×</div>
        </div>

        <div class="body">
            <!-- 只缓存播放页：切到其它 tab 时播放器实例保留，回来可继续播放 -->
            <router-view v-slot="{ Component }">
                <keep-alive include="Play">
                    <component :is="Component" />
                </keep-alive>
            </router-view>
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Window } from "@wailsio/runtime";
import { i18nState, setLang, t } from "@/i18n";
import BrandIcon from "@/components/BrandIcon.vue";

const router = useRouter();
const route = useRoute();

const tabs = computed(() => [
    { label: t("nav.play"), path: "/play" },
    { label: t("nav.download"), path: "/download" },
    { label: t("nav.library"), path: "/library" },
    { label: t("nav.resources"), path: "/resources" },
    { label: t("nav.find"), path: "/find" },
    { label: t("nav.settings"), path: "/settings" },
]);

const langOptions = computed(() =>
    i18nState.locales.map((locale) => ({
        key: locale.code,
        // 当前语言加 ✓，避免下拉里看不出选中项
        label: (locale.nativeName || locale.name) + (locale.code === i18nState.lang ? " ✓" : ""),
    }))
);

const currentLangLabel = computed(() => {
    const matched = i18nState.locales.find((locale) => locale.code === i18nState.lang);
    return matched ? matched.nativeName || matched.name : i18nState.lang;
});

function onLangChange(lang: string) {
    if (lang) void setLang(lang);
}

const toggleMaximise = () => Window.ToggleMaximise();

const minimise = () => Window.Minimise();

const closeWindow = () => Window.Close();

const refresh = () => window.location.reload();
</script>

<style scoped>
.app {
    /* 占满 #app 的全部空间，并把 flex 上下文传给子 Layout */
    min-height: 100vh;
    min-height: 100dvh;
    max-height: 100vh;
    max-height: 100dvh;
    display: flex;
    flex-direction: column;
    background: var(--app-bg);
    overflow: hidden;
}

.top-bar {
    width: 100%;
    height: 50px;
    flex: 0 0 50px;
    display: flex;
    align-items: center;
    padding-left: 20px;
    box-sizing: border-box;
    background: var(--app-surface);
    border-bottom: 1px solid var(--app-border);
    user-select: none;
}

/* 主图标与标签组之间保持 14px 间距，图标自身不参与点击和拖拽 */
.nav-logo {
    margin-right: 14px;
    pointer-events: none;
}

.left-tabs {
    height: 100%;
    display: flex;
    align-items: center;
    gap: 12px;
    flex-shrink: 0;
}

.tab {
    height: 100%;
    display: flex;
    align-items: center;
    padding: 0 12px;
    color: var(--app-text-muted);
    cursor: pointer;
    box-sizing: border-box;
}

.tab:hover {
    color: var(--app-text);
}

.tab.active {
    color: var(--app-text);
    border-bottom: 2px solid var(--app-accent, #3b82f6);
}

.drag-area {
    flex: 1;
    height: 100%;
    min-width: 30px;
    cursor: default;
}

.lang-icon .globe {
    color: var(--app-text-muted);
}

.lang-icon:hover .globe {
    color: var(--app-text);
}

.right-icon,
.window-button {
    width: 40px;
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    color: var(--app-text-muted);
    cursor: pointer;
    box-sizing: border-box;
}

.right-icon:hover,
.window-button:hover {
    color: var(--app-text);
    background: var(--app-hover);
}

.window-button.close:hover {
    color: #fff;
    background: #e81123;
}

.body {
    width: 100%;
    flex: 1;
    min-height: 0;
    display: flex;
    overflow: hidden;
}
</style>