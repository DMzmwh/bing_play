<template>
    <div class="play">
        <!-- 左侧播放列表 -->
        <div class="left-panel">
            <div class="title">播放列表 [3]</div>

            <div class="search">
                <n-input size="small" placeholder="请输入搜索关键词" />
            </div>

            <div class="list">
                <div class="group">
                    <div class="group-title">除恶 [16]</div>
                    <div
                        v-for="i in 50"
                        :key="i"
                        class="episode"
                        :class="{ active: i === 1 }"
                    >
                        <span>{{ i.toString().padStart(2, '0') }} 第{{ i }}集</span>
                        <span class="time">00:00:00</span>
                    </div>
                </div>

                <div class="group small">
                    <div>牛气有为 [26]</div>
                </div>

                <div class="group small">
                    <div>低智商犯罪 [24]</div>
                </div>
            </div>
        </div>

        <!-- 中间播放区 -->
        <div
            id="play-drop-area"
            class="center"
            data-file-drop-target
            @dragenter.prevent="dragOver = true"
            @dragover.prevent
            @dragleave="onDragLeave"
            @drop.prevent="dragOver = false"
        >
            <ArtPlayer
                :url="videoUrl"
                :options="playerOptions"
                autoplay
                @fullscreen-web="syncWindowMaximise"
                @error="onPlayerError"
                />
            <div v-if="dragOver" class="drop-tip">{{ t('video.drop_tip') }}</div>
            <div v-if="errorMsg" class="error-tip">{{ errorMsg }}</div>
            <!-- 底部控制条 -->
            <!-- <PlayerPanel /> -->
        </div>
    </div>
</template>

<script lang="ts" setup>
import { Window, Events } from '@wailsio/runtime'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { i18nState, t } from '@/i18n'

/** 后端视频服务地址，视频流只能通过 HTTP 拉取 */
const API_BASE = 'http://127.0.0.1:9090'
/** 后端打开视频成功的事件名，需与 Go 端保持一致 */
const VIDEO_OPENED_EVENT = 'video:opened'
/** 后端打开视频失败的事件名，需与 Go 端保持一致 */
const VIDEO_OPEN_FAILED_EVENT = 'video:open-failed'

const videoUrl = ref(`${API_BASE}/video/local/11`)
const dragOver = ref(false)
const errorMsg = ref('')

/** 播放器配置；ArtPlayer 文案格式为 { 语言代码: { 原生key: 文案 } }，语言由 lang 选项决定 */
const playerOptions = computed(() => {
    // ArtPlayer 内部会对 lang 做 lowerCase 匹配，语言文件里的分组 key 也统一用小写
    const lang = i18nState.lang.toLowerCase()
    return {
        fullscreen: true,
        fullscreenWeb: true,
        theme: '#3b82f6',
        lang,
        i18n: { [lang]: { ...i18nState.playerMessages } }
    }
})

let offVideoOpened: (() => void) | undefined
let offVideoOpenFailed: (() => void) | undefined

function onDragLeave(event: DragEvent) {
    const zone = event.currentTarget as HTMLElement | null
    // 指针仍在拖拽区域内（移入子元素）时不隐藏提示
    if (zone && event.relatedTarget && zone.contains(event.relatedTarget as Node)) return
    dragOver.value = false
}

function onPlayerError(error: Error) {
    errorMsg.value = error?.message || t('video.play_failed')
}

onMounted(() => {
    // 拖拽由 Go 侧处理（路径本来就在 Go 手里），前端只接收结果
    offVideoOpened = Events.On(VIDEO_OPENED_EVENT, (event) => {
        const result = (event.data ?? {}) as { id?: number }
        if (!result.id) return

        errorMsg.value = ''
        videoUrl.value = `${API_BASE}/video/${result.id}`
    })

    offVideoOpenFailed = Events.On(VIDEO_OPEN_FAILED_EVENT, (event) => {
        const { message } = (event.data ?? {}) as { message?: string }
        errorMsg.value = message || t('video.error_open_failed')
    })
})

onBeforeUnmount(() => {
    offVideoOpened?.()
    offVideoOpenFailed?.()
})

// 进入播放器网页全屏前窗口是否已最大化，用于退出时恢复
let wasMaximised = false

async function syncWindowMaximise(state: boolean) {
    // try {
    //     if (state) {
    //         wasMaximised = await Window.IsMaximised()
    //         if (!wasMaximised) {
    //             await Window.Maximise()
    //         }
    //     } else if (!wasMaximised) {
    //         await Window.UnMaximise()
    //     }
    // } catch (error) {
    //     console.warn('同步窗口最大化失败：', error)
    // }
}
</script>

<style lang="scss" scoped>
.play {
    /* 它是 .body 的直接flex子项 */
    flex: 1 1 0; /* ✅改用 flex:1 1 0，basis=0，比单纯 flex:1 更强力 */
    min-height: 0;
    display: flex;
    overflow: hidden;
}

.left-panel {
    flex: 0 0 260px; /* ✅ flex简写：grow 0 shrink 0 basis 260px，固定宽度 */
    min-height: 0;
    border-right: 1px solid #1d2733;
    padding: 10px;
    overflow-y: auto;
    overflow-x: hidden;
}

.title {
    color: #fff;
    margin-bottom: 10px;
    flex-shrink: 0; // 固定不压缩
}

.search {
    border-radius: 6px;
    flex-shrink: 0; // 固定不压缩
    margin-bottom: 8px;
}

.search :deep(.n-input) {
    background: #111827;
    border-radius: 6px;
}


.group-title {
    margin-top: 10px;
    color: #999;
    font-size: 12px;
}

.episode {
    display: flex;
    justify-content: space-between;
    padding: 6px;
    font-size: 12px;
    cursor: pointer;
    color: #bbb;
}

.episode:hover {
    background: #182230;
}

.episode.active {
    background: #1e2a3a;
    color: #fff;
}

.time {
    color: #666;
}

.group.small {
    > div {
        margin-top: 10px;
        color: #999;
        font-size: 12px;
    }
}

/* center */
.center {
    flex: 1;
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
}

/* 系统拖拽进入播放区域时的高亮，类名由 Wails 运行时自动添加 */
.center.file-drop-target-active {
    outline: 2px dashed #3b82f6;
    outline-offset: -6px;
}

.drop-tip,
.error-tip {
    position: absolute;
    left: 50%;
    transform: translateX(-50%);
    padding: 8px 14px;
    border-radius: 6px;
    font-size: 13px;
    pointer-events: none;
    z-index: 10;
}

.drop-tip {
    top: 16px;
    color: #fff;
    background: rgba(59, 130, 246, 0.85);
}

.error-tip {
    bottom: 16px;
    max-width: 80%;
    color: #fff;
    background: rgba(220, 38, 38, 0.85);
}

.player-placeholder {
    text-align: center;
    color: #aaa;
}

.logo {
    font-size: 40px;
}

.text {
    margin-top: 10px;
    font-size: 18px;
    color: #fff;
}

.info-card {
    margin-top: 20px;
    font-size: 12px;
    color: #888;
}

.open-btn {
    margin-top: 20px;
}
</style>
