<template>
    <div class="play">
        <!-- 左侧播放列表 -->
        <div class="left-panel" :style="{ width: `${sidebarWidth}px` }">
            <!-- 配套图标：与导航主图标同一字形，去掉底板，18px 与标题文字对齐 -->
            <div class="title">
                <BrandIcon class="playlist-logo" variant="mark" :size="18" />
                <span class="title-text">播放列表 [{{ playlist.length }}]</span>
            </div>

            <!-- <div class="search">
                <n-input size="small" placeholder="请输入搜索关键词" />
            </div> -->

            <div class="list">
                <!-- 数据来自 VideoService.ListPlaylist，按 resourceName 分组；单条分组不显示组名 -->
                <div v-for="group in playlistGroups" :key="group.title" class="group">
                    <div
                        v-if="group.videos.length > 1"
                        class="group-title"
                        @click="toggleGroup(group.title)"
                        @contextmenu.prevent.stop="openContextMenu($event, { type: 'group', group })"
                    >
                        <n-icon
                            size="14"
                            :component="isGroupExpanded(group.title) ? CaretDown : CaretForward"
                        />
                        <span class="group-name">
                            {{ group.title }} [{{ group.videos.length }}]
                        </span>
                    </div>
                    <!-- 只有播放中的分组默认展开，其余分组收起 -->
                    <div v-show="group.videos.length === 1 || isGroupExpanded(group.title)">
                        <div
                            v-for="video in group.videos"
                            :key="video.id"
                            class="episode"
                            :class="{ active: video.id === currentVideoId }"
                            @click="playVideo(video)"
                            @contextmenu.prevent.stop="openContextMenu($event, { type: 'item', video })"
                        >
                            <span class="name">
                                {{ video.fileName || video.resourceName }}
                            </span>
                        </div>
                    </div>
                </div>
            </div>
        </div>

        <!-- 列表右键菜单：播放 / 删除；manual 模式按鼠标坐标弹出 -->
        <n-dropdown
            trigger="manual"
            placement="bottom-start"
            :show="contextMenu.show"
            :x="contextMenu.x"
            :y="contextMenu.y"
            :options="contextMenuOptions"
            @select="handleContextMenuSelect"
            @clickoutside="closeContextMenu"
        />

        <!-- 拖动调整播放列表宽度，松开后自动保存 -->
        <div class="resize-handle" @mousedown="onResizeStart" />

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
                ref="playerRef"
                v-if="videoUrl"
                :url="videoUrl"
                :options="playerOptions"
                :has-prev="canPlayPrev"
                :has-next="canPlayNext"
                :autoplay="pageActive"
                @fullscreen-web="syncWindowMaximise"
                @prev="playPrev"
                @next="playNext"
                @ended="playNext"
                @error="onPlayerError"
            />
            <!-- 空状态：还没有播放地址时展示拖拽引导 -->
            <div v-if="!videoUrl && !dragOver && !preparing" class="empty-state">
                <svg class="empty-icon" viewBox="0 0 64 64" aria-hidden="true">
                    <rect x="7" y="13" width="50" height="38" rx="4" />
                    <circle cx="32" cy="32" r="9.5" />
                    <path class="film-holes" d="M15 13v7M23 13v7M31 13v7M39 13v7M47 13v7M15 51v-7M23 51v-7M31 51v-7M39 51v-7M47 51v-7" />
                    <path class="film-line" d="M7 22h50M7 42h50" />
                    <path class="play-triangle" d="M29.6 27.4l6.8 4.6-6.8 4.6z" />
                </svg>
                <div class="empty-title">{{ t('video.empty_title') }}</div>
                <div class="empty-hint">{{ t('video.empty_hint') }}</div>
            </div>
            <div v-if="dragOver" class="drop-tip">{{ t('video.drop_tip') }}</div>
            <!-- 播放前探测 / 转码提示，文案与进度都来自后端 -->
            <div v-if="preparing && !videoUrl" class="prepare-tip">
                <div class="prepare-text">
                    {{ prepareMessage || t('video.transcode.preparing') }}
                </div>
                <n-progress
                    v-if="prepareProgress > 0"
                    class="prepare-progress"
                    type="line"
                    :percentage="prepareProgressValue"
                    :height="4"
                    :show-indicator="false"
                />
                <div v-if="prepareProgress > 0" class="prepare-percent">{{ prepareProgressValue }}%</div>
            </div>
            <div v-if="errorMsg" class="error-tip">{{ errorMsg }}</div>
            <!-- 底部控制条 -->
            <!-- <PlayerPanel /> -->
        </div>
    </div>
</template>

<script lang="ts" setup>
import { Window, Events } from '@wailsio/runtime'
import { CaretDown, CaretForward } from '@vicons/ionicons5'
import { computed, onActivated, onBeforeUnmount, onDeactivated, onMounted, ref, watch } from 'vue'
import { useMessage } from 'naive-ui'
import BrandIcon from '@/components/BrandIcon.vue'
import { i18nState, t } from '@/i18n'
import {
    SettingKeys,
    activeAccentColor,
    loadSettings,
    saveSettingValue,
    settingNumber
} from '@/settings'
import {
    ListPlaylist,
    RemovePlaylistItems
} from '../../bindings/github.com/bing/bing-play/internal/service/videoservice'
import type {
    OpenVideoResult,
    PlaylistItem
} from '../../bindings/github.com/bing/bing-play/internal/service/models'

/** 后端视频服务地址，视频流只能通过 HTTP 拉取 */
const API_BASE = 'http://127.0.0.1:9090'
/** 后端打开视频成功的事件名，需与 Go 端保持一致 */
const VIDEO_OPENED_EVENT = 'video:opened'
/** 后端打开视频失败的事件名，需与 Go 端保持一致 */
const VIDEO_OPEN_FAILED_EVENT = 'video:open-failed'

/** video:opened 事件载荷：current 为立即播放的视频 */
interface VideoOpenedPayload {
    current?: OpenVideoResult
}

/** ArtPlayer 组件对外暴露的控制方法，用于页面切换时暂停/恢复 */
interface PlayerExpose {
    isPlaying: () => boolean
    play: () => Promise<void> | undefined
    pause: () => void
}

/** 分组后的播放列表，组名为 resourceName */
interface PlaylistGroup {
    title: string
    videos: PlaylistItem[]
}

/** 当前播放的视频 ID，为空时不渲染播放器，播放区保持空白 */
const currentVideoId = ref<number | null>(null)
/** 播放列表数据，统一来自 VideoService.ListPlaylist */
const playlist = ref<PlaylistItem[]>([])
/** 当前展开的分组；只允许一个分组展开，避免列表过长 */
const expandedGroupTitle = ref<string | null>(null)
/** 当前视频在播放列表中的位置，-1 表示不在列表中 */
const currentIndex = computed(() =>
    playlist.value.findIndex((video) => video.id === currentVideoId.value)
)
/** 是否存在上一个/下一个，用于控制条按钮置灰 */
const canPlayPrev = computed(() => currentIndex.value > 0)
const canPlayNext = computed(
    () => currentIndex.value >= 0 && currentIndex.value < playlist.value.length - 1
)

/** 分组的 key：优先资源名，没有资源名时按文件名分组，避免出现空组名 */
function groupKeyOf(video: PlaylistItem) {
    return video.resourceName || video.fileName || String(video.id)
}

/** 按 resourceName 分组 */
const playlistGroups = computed<PlaylistGroup[]>(() => {
    const groups = new Map<string, PlaylistItem[]>()
    for (const video of playlist.value) {
        const key = groupKeyOf(video)
        const group = groups.get(key)
        if (group) {
            group.push(video)
        } else {
            groups.set(key, [video])
        }
    }
    return Array.from(groups, ([title, videos]) => ({ title, videos }))
})

/** 判断分组是否展开 */
function isGroupExpanded(title: string) {
    return expandedGroupTitle.value === title
}

/** 点击三角号切换分组展开状态 */
function toggleGroup(title: string) {
    expandedGroupTitle.value = isGroupExpanded(title) ? null : title
}

/** 右键菜单的当前目标：单集或整个分组（目录） */
interface ContextMenuTarget {
    type: 'item' | 'group'
    video?: PlaylistItem
    group?: PlaylistGroup
}

/** 右键菜单位置与目标 */
const contextMenu = ref<{
    show: boolean
    x: number
    y: number
    target: ContextMenuTarget | null
}>({
    show: false,
    x: 0,
    y: 0,
    target: null
})

/** 右键菜单选项：播放、删除 */
const contextMenuOptions = computed(() => [
    { label: t('video.play'), key: 'play' },
    { label: t('video.remove'), key: 'delete' }
])

/** 在鼠标位置打开右键菜单 */
function openContextMenu(event: MouseEvent, target: ContextMenuTarget) {
    contextMenu.value = {
        show: true,
        x: event.clientX,
        y: event.clientY,
        target
    }
}

/** 关闭右键菜单 */
function closeContextMenu() {
    contextMenu.value = { ...contextMenu.value, show: false, target: null }
}

/** 处理右键菜单操作；分组播放表示播放该组第一集 */
function handleContextMenuSelect(key: string | number) {
    const target = contextMenu.value.target
    closeContextMenu()
    if (!target) return

    if (key === 'play') {
        const video = target.type === 'group' ? target.group?.videos[0] : target.video
        if (video) playVideo(video)
        return
    }

    if (key === 'delete') {
        if (target.type === 'group' && target.group) {
            removeGroup(target.group)
        } else if (target.video) {
            removeItem(target.video)
        }
    }
}

/** 播放中的视频所在分组，没有播放时返回 null */
function playingGroupTitle() {
    const id = currentVideoId.value
    if (id === null) return null
    const video = playlist.value.find((item) => item.id === id)
    return video ? groupKeyOf(video) : null
}

// 播放切换或列表刷新后，只展开当前播放视频所在的分组，其余分组收起
watch(
    [currentVideoId, playlistGroups],
    () => {
        expandedGroupTitle.value = playingGroupTitle()
    },
    { immediate: true }
)
function videoUrlById(id: number): string {
    return `${API_BASE}/video/${id}`
}

/** 后端 /video/prepare 与 /video/transcode/{jobId} 的返回结构 */
interface PrepareResponse {
    id?: number
    url?: string
    action?: string
    jobId?: string
    phase?: string
    progress?: number
    message?: string
}

/** 转换进度轮询间隔 */
const TRANSCODE_POLL_INTERVAL = 800

/** 真正交给播放器的地址：探测 / 转换完成前为空，避免先播出只有声音的视频 */
const playbackUrl = ref('')
/** 是否正在探测或转换视频格式 */
const preparing = ref(false)
/** 转换进度百分比 */
const prepareProgress = ref(0)
/** 后端按当前语言生成的提示文案 */
const prepareMessage = ref('')
/** 切换视频后，旧请求返回的数据不再生效 */
let prepareToken = 0

const videoUrl = computed(() => playbackUrl.value)
const prepareProgressValue = computed(() => Math.round(prepareProgress.value))
const dragOver = ref(false)
const errorMsg = ref('')

function sleep(ms: number) {
    return new Promise<void>((resolve) => window.setTimeout(resolve, ms))
}

/** 进度值做边界保护，后端异常数据不影响进度条 */
function clampProgress(value?: number) {
    if (!value || value < 0) return 0
    return value > 100 ? 100 : value
}

const message = useMessage()

/** 播放器组件实例，用于页面切走时暂停、切回时恢复 */
const playerRef = ref<PlayerExpose | null>(null)
/** 当前是否停留在播放页，离开时不自动播放新视频 */
const pageActive = ref(true)
/** 离开页面时是否正在播放；切回后仅恢复原本正在播放的视频 */
let resumeOnActivate = false

/** 切到其它 tab：记录播放状态并暂停，避免后台播放占用资源 */
onDeactivated(() => {
    pageActive.value = false
    resumeOnActivate = playerRef.value?.isPlaying() ?? false
    if (resumeOnActivate) playerRef.value?.pause()
})

/** 切回播放页：若离开前正在播放，则自动继续播放 */
onActivated(() => {
    pageActive.value = true
    // 下载中的视频会进入播放列表，切回时重新拉取才能看到新任务
    void fetchPlaylist()
    if (!resumeOnActivate) return
    resumeOnActivate = false
    void playerRef.value?.play()
})

/** 播放列表宽度，拖动分隔条后保存到设置 */
const sidebarWidth = ref(settingNumber(SettingKeys.sidebarWidth, 260))
/** 拖动分隔条时的起始鼠标位置与起始宽度 */
let resizeStartX = 0
let resizeStartWidth = 0

/** 限制播放列表宽度范围，与后端设置项校验保持一致 */
function clampSidebarWidth(value: number) {
    return Math.min(360, Math.max(200, value))
}

function onResizeStart(event: MouseEvent) {
    if (event.button !== 0) return
    resizeStartX = event.clientX
    resizeStartWidth = sidebarWidth.value
    window.addEventListener('mousemove', onResizeMove)
    window.addEventListener('mouseup', onResizeEnd)
    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'
}

function onResizeMove(event: MouseEvent) {
    sidebarWidth.value = clampSidebarWidth(resizeStartWidth + event.clientX - resizeStartX)
}

/** 松开鼠标后把新宽度写入设置，失败时提示并保留本地宽度 */
function onResizeEnd() {
    window.removeEventListener('mousemove', onResizeMove)
    window.removeEventListener('mouseup', onResizeEnd)
    document.body.style.cursor = ''
    document.body.style.userSelect = ''

    void saveSettingValue(SettingKeys.sidebarWidth, String(Math.round(sidebarWidth.value))).then(
        (ok) => {
            if (!ok) message.error(t('settings.save_failed'))
        }
    )
}

/** 播放器配置；ArtPlayer 文案格式为 { 语言代码: { 原生key: 文案 } }，语言由 lang 选项决定 */
const playerOptions = computed(() => {
    // ArtPlayer 内部会对 lang 做 lowerCase 匹配，语言文件里的分组 key 也统一用小写
    const lang = i18nState.lang.toLowerCase()
    return {
        fullscreen: true,
        fullscreenWeb: true,
        theme: activeAccentColor.value,
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

/** 切换到指定视频 */
function selectVideo(video: PlaylistItem) {
    errorMsg.value = ''
    currentVideoId.value = video.id
}

/** 点击播放列表项切换当前视频 */
function playVideo(video: PlaylistItem) {
    selectVideo(video)
}

/** 从播放列表移除若干条目：本地视频删除记录，种子资源重置为未下载，均不删除磁盘文件 */
async function removePlaylistItems(items: PlaylistItem[]) {
    if (!items.length) return

    try {
        await RemovePlaylistItems(items.map((item) => item.id))
    } catch (error) {
        console.error('移除播放列表条目失败:', error)
        message.error(t('video.remove_failed'))
        // 部分条目可能已移除，重新拉取保证列表与数据库一致
        void fetchPlaylist()
        return
    }

    const removedIds = new Set(items.map((item) => item.id))
    playlist.value = playlist.value.filter((video) => !removedIds.has(video.id))
    if (currentVideoId.value !== null && removedIds.has(currentVideoId.value)) {
        currentVideoId.value = null
    }
}

/** 删除单集 */
function removeItem(video: PlaylistItem) {
    void removePlaylistItems([video])
}

/** 删除整个分组（目录） */
function removeGroup(group: PlaylistGroup) {
    void removePlaylistItems(group.videos)
}

// 切换视频时重新准备：清空旧地址，并让上一次的请求与轮询失效
watch(currentVideoId, (id) => {
    prepareToken += 1
    const token = prepareToken

    playbackUrl.value = ''
    prepareProgress.value = 0
    prepareMessage.value = ''

    if (id === null) {
        preparing.value = false
        return
    }
    preparing.value = true
    void prepareVideo(id, token)
})

/** 播放前请求后端探测格式，需要转码时等待转换完成再播放 */
async function prepareVideo(id: number, token: number) {
    try {
        const response = await fetch(`${API_BASE}/video/prepare/${id}`)
        if (token !== prepareToken) return
        if (!response.ok) throw new Error(await response.text())

        const data = (await response.json()) as PrepareResponse
        if (token !== prepareToken) return

        applyPrepareResult(data, id)
        if (data.action === 'convert' && data.jobId) {
            await pollTranscode(data.jobId, id, token)
        }
    } catch (error) {
        if (token !== prepareToken) return
        // 准备接口异常时不阻断播放，直接使用原文件地址
        playbackUrl.value = videoUrlById(id)
        errorMsg.value = error instanceof Error ? error.message : ''
    } finally {
        if (token === prepareToken) preparing.value = false
    }
}

/** 应用准备结果：只有转换成功才切到缓存地址，其余场景回退原文件 */
function applyPrepareResult(data: PrepareResponse, id: number) {
    prepareProgress.value = clampProgress(data.progress)
    if (data.message) prepareMessage.value = data.message

    if (data.action === 'play_converted' && data.url) {
        playbackUrl.value = data.url
        prepareMessage.value = ''
        return
    }
    if (data.action === 'play_direct') {
        playbackUrl.value = videoUrlById(id)
        prepareMessage.value = ''
        return
    }
    // convert 由轮询继续等待；fallback / defer 直接播放原文件并保留后端提示
    if (data.action !== 'convert') {
        playbackUrl.value = videoUrlById(id)
    }
}

/** 轮询转换进度，任务结束后回到播放流程 */
async function pollTranscode(jobId: string, id: number, token: number) {
    while (token === prepareToken) {
        await sleep(TRANSCODE_POLL_INTERVAL)
        if (token !== prepareToken) return

        try {
            const response = await fetch(`${API_BASE}/video/transcode/${jobId}`)
            if (!response.ok) throw new Error(await response.text())

            const data = (await response.json()) as PrepareResponse
            if (token !== prepareToken) return

            prepareProgress.value = clampProgress(data.progress)
            if (data.message) prepareMessage.value = data.message
            if (data.phase !== 'done' && data.phase !== 'canceled') continue

            applyPrepareResult(data, id)
            return
        } catch (error) {
            if (token !== prepareToken) return
            // 进度查询失败时不再等待，回退原文件播放
            playbackUrl.value = videoUrlById(id)
            errorMsg.value = error instanceof Error ? error.message : ''
            return
        }
    }
}

/** 播放失败时切到下一个；已是最后一个则显示错误提示 */
function onPlayerError(_error: Error, failedUrl?: string) {
    const failedId = currentVideoId.value
    if (failedId === null) {
        errorMsg.value = t('video.play_failed')
        return
    }
    // 已切换到别的视频后旧视频源才抛出的错误，直接忽略
    if (failedUrl && failedUrl !== playbackUrl.value) return

    if (canPlayNext.value) {
        selectVideo(playlist.value[currentIndex.value + 1])
        return
    }

    errorMsg.value = t('video.play_failed')
}

/** 播放上一个，已是第一个则不处理 */
function playPrev() {
    if (!canPlayPrev.value) return

    selectVideo(playlist.value[currentIndex.value - 1])
}

/** 播放下一个；播完自动续播和点击按钮共用，已是最后一个则停止 */
function playNext(): boolean {
    if (!canPlayNext.value) return false

    selectVideo(playlist.value[currentIndex.value + 1])
    return true
}

/** 通过 Wails service 重新获取左侧播放列表，拖入文件后由后端事件触发 */
async function fetchPlaylist() {
    try {
        playlist.value = (await ListPlaylist()) ?? []
    } catch (error) {
        console.warn('获取播放列表失败:', error)
    }
}

onMounted(async () => {
    // 读取设置中的播放列表宽度，未保存过时使用默认值
    await loadSettings()
    sidebarWidth.value = clampSidebarWidth(settingNumber(SettingKeys.sidebarWidth, 260))

    void fetchPlaylist()

    // 拖拽由 Go 侧处理（路径本来就在 Go 手里），前端只接收结果
    offVideoOpened = Events.On(VIDEO_OPENED_EVENT, (event) => {
        const payload = (event.data ?? {}) as VideoOpenedPayload
        const current = payload.current
        if (!current?.id) return

        errorMsg.value = ''
        currentVideoId.value = current.id
        // 拖入的文件已写入数据库，重新拉取才能看到同组的其它剧集
        void fetchPlaylist()
    })

    offVideoOpenFailed = Events.On(VIDEO_OPEN_FAILED_EVENT, (event) => {
        const { message } = (event.data ?? {}) as { message?: string }
        errorMsg.value = message || t('video.error_open_failed')
    })
})

onBeforeUnmount(() => {
    offVideoOpened?.()
    offVideoOpenFailed?.()
    window.removeEventListener('mousemove', onResizeMove)
    window.removeEventListener('mouseup', onResizeEnd)
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
    flex: 0 0 auto; /* 宽度由拖动分隔条控制并保存到设置 */
    box-sizing: border-box;
    min-height: 0;
    border-right: 1px solid var(--app-border);
    padding: 10px;
    overflow-y: auto;
    overflow-x: hidden;
}

/* 播放列表与播放区之间的拖动分隔条 */
.resize-handle {
    flex: 0 0 4px;
    cursor: col-resize;
    background: transparent;
    transition: background 0.15s;
}

.resize-handle:hover {
    background: var(--app-accent, #3b82f6);
}

.title {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--app-text);
    margin-bottom: 10px;
    flex-shrink: 0; // 固定不压缩
}

.title-text {
    min-width: 0;
}

.search {
    border-radius: 6px;
    flex-shrink: 0; // 固定不压缩
    margin-bottom: 8px;
}

.search :deep(.n-input) {
    background: var(--app-surface);
    border-radius: 6px;
}


.group-title {
    display: flex;
    align-items: center;
    gap: 4px;
    margin-top: 10px;
    color: var(--app-text-dim);
    font-size: 12px;
    cursor: pointer;
    user-select: none;
}

.group-title:hover {
    color: var(--app-text-muted);
}

.group-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.episode {
    display: flex;
    align-items: center;
    gap: 6px;
    justify-content: space-between;
    padding: 6px;
    font-size: 12px;
    cursor: pointer;
    color: var(--app-text-muted);
}

.episode .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.episode:hover {
    background: var(--app-hover-row);
}

.episode.active {
    background: var(--app-active-row);
    color: var(--app-text);
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
    outline: 2px dashed var(--app-accent, #3b82f6);
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

/* 播放前探测 / 转码提示 */
.prepare-tip {
    position: absolute;
    left: 50%;
    top: 50%;
    transform: translate(-50%, -50%);
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
    max-width: 70%;
    padding: 12px 18px;
    border-radius: 8px;
    font-size: 13px;
    color: var(--app-text-muted);
    background: var(--app-surface);
    z-index: 10;
}

.prepare-progress {
    width: 220px;
}

.prepare-percent {
    font-size: 12px;
    opacity: 0.8;
}

/* 空状态：无播放地址时的拖拽引导 */
.empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    pointer-events: none;
    user-select: none;
}

.empty-icon {
    width: 64px;
    height: 64px;
    margin-bottom: 14px;
    fill: none;
    stroke: var(--app-icon);
    stroke-width: 2;
    stroke-linecap: round;
    stroke-linejoin: round;
}

.empty-icon .film-holes,
.empty-icon .film-line {
    stroke: var(--app-icon-line);
    stroke-width: 1.4;
}

.empty-icon .play-triangle {
    fill: var(--app-accent, #3b82f6);
    stroke: none;
}

.empty-title {
    font-size: 14px;
    color: var(--app-text-muted);
}

.empty-hint {
    margin-top: 6px;
    font-size: 12px;
    color: var(--app-text-dim);
}

.drop-tip {
    top: 16px;
    color: #fff;
    background: var(--app-accent-soft, rgba(59, 130, 246, 0.85));
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
