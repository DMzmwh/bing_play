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

            <div class="right-icon" @click="refresh">⟳</div>
            <div class="window-button" @click="minimise">─</div>
            <div class="window-button" @click="toggleMaximise">□</div>
            <div class="window-button close" @click="closeWindow">×</div>
        </div>

        <div class="body">
            <Play />
        </div>
    </div>
</template>

<script setup lang="ts">
import { useRoute, useRouter } from "vue-router";
import { Window } from "@wailsio/runtime";
import Play from "@/views/Play.vue";

const router = useRouter();
const route = useRoute();

const tabs = [
    { label: "播放", path: "/play" },
    { label: "我的下载", path: "/download" },
    { label: "影片库", path: "/library" },
    { label: "找片", path: "/find" },
];


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
    padding: 0 20px;
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