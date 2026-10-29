<template>
    <div class="app">
        <div class="top-bar">
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
            <router-view/>
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Window } from "@wailsio/runtime";
import { i18nState, setLang, t } from "@/i18n";

const router = useRouter();
const route = useRoute();

const tabs = computed(() => [
    { label: t("nav.play"), path: "/play" },
    { label: t("nav.download"), path: "/download" },
    { label: t("nav.library"), path: "/library" },
    { label: t("nav.find"), path: "/find" },
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
    background: #0b0f14;
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
    background: #0f1620;
    border-bottom: 1px solid #1d2733;
    user-select: none;
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
    color: #aaa;
    cursor: pointer;
    box-sizing: border-box;
}

.tab:hover {
    color: #ddd;
}

.tab.active {
    color: #fff;
    border-bottom: 2px solid #3b82f6;
}

.drag-area {
    flex: 1;
    height: 100%;
    min-width: 30px;
    cursor: default;
}

.lang-icon .globe {
    color: #aaa;
}

.lang-icon:hover .globe {
    color: #fff;
}

.right-icon,
.window-button {
    width: 40px;
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    color: #aaa;
    cursor: pointer;
    box-sizing: border-box;
}

.right-icon:hover,
.window-button:hover {
    color: #fff;
    background: #1a2430;
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