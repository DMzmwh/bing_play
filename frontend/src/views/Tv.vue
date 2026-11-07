<template>
    <div class="tv">
        <!-- 左侧频道列表 -->
        <div class="left-panel" :style="{ width: `${sidebarWidth}px` }">
            <div class="panel-header">
                <div class="title">
                    <BrandIcon class="playlist-logo" variant="mark" :size="18" />
                    <span class="title-text">{{ t('tv.title') }} [{{ filteredChannels.length }}]</span>
                </div>
                <n-input
                    v-model:value="keyword"
                    class="search"
                    clearable
                    size="small"
                    :placeholder="t('tv.search_placeholder')"
                />
                <div v-if="statusText" class="status" :class="{ error: Boolean(syncError) }">
                    {{ statusText }}
                </div>
            </div>

            <div class="list">
                <div v-for="group in groups" :key="group.title" class="group">
                    <div class="group-title" @click="toggleGroup(group.title)">
                        <n-icon
                            size="14"
                            :component="isGroupExpanded(group.title) ? CaretDown : CaretForward"
                        />
                        <span class="group-name">
                            {{ group.title }} [{{ group.channels.length }}]
                        </span>
                    </div>
                    <!-- 折叠时不渲染子节点；大分组用虚拟列表，避免一次创建上千个 DOM -->
                    <template v-if="isGroupExpanded(group.title)">
                        <n-virtual-list
                            v-if="group.channels.length > virtualThreshold"
                            class="group-virtual"
                            :items="group.channels"
                            :item-size="channelItemHeight"
                            key-field="id"
                            :style="{ height: `${virtualListHeight(group)}px` }"
                        >
                            <template #default="{ item }">
                                <div
                                    class="channel"
                                    :class="{ active: item.id === currentChannel?.id }"
                                    @click="playChannel(item)"
                                >
                                    <span class="channel-logo">
                                        <img
                                            v-if="item.logo"
                                            :src="item.logo"
                                            loading="lazy"
                                            @load="onLogoLoad"
                                            @error="onLogoError"
                                        />
                                    </span>
                                    <span class="name">{{ item.name }}</span>
                                </div>
                            </template>
                        </n-virtual-list>
                        <div v-else>
                            <div
                                v-for="channel in group.channels"
                                :key="channel.id"
                                class="channel"
                                :class="{ active: channel.id === currentChannel?.id }"
                                @click="playChannel(channel)"
                            >
                                <span class="channel-logo">
                                    <img
                                        v-if="channel.logo"
                                        :src="channel.logo"
                                        loading="lazy"
                                        @load="onLogoLoad"
                                        @error="onLogoError"
                                    />
                                </span>
                                <span class="name">{{ channel.name }}</span>
                            </div>
                        </div>
                    </template>
                </div>
            </div>
        </div>

        <!-- 拖动调整频道列表宽度，松开后保存 -->
        <div class="resize-handle" @mousedown="onResizeStart" />

        <!-- 中间播放区 -->
        <div class="center">
            <ArtPlayer
                v-if="currentUrl"
                :url="currentUrl"
                :options="playerOptions"
                :autoplay="true"
                :has-prev="hasPrevChannel"
                :has-next="hasNextChannel"
                @prev="playPrevChannel"
                @next="playNextChannel"
                @error="onPlayerError"
            />
            <div v-if="!currentUrl" class="empty-state">
                <svg class="empty-icon" viewBox="0 0 64 64" aria-hidden="true">
                    <rect x="8" y="14" width="48" height="34" rx="4" />
                    <path class="antenna" d="M24 14l-6-8M40 14l6-8" />
                    <path class="screen-line" d="M16 38h10M32 38h16" />
                </svg>
                <div class="empty-title">
                    {{ channels.length ? t('tv.select_channel') : t('tv.empty_title') }}
                </div>
                <div v-if="!channels.length" class="empty-hint">
                    {{ t('tv.empty_hint') }}
                </div>
            </div>
            <div v-if="errorMsg" class="error-tip">{{ errorMsg }}</div>
        </div>
    </div>
</template>

<script lang="ts" setup>
import { Events } from '@wailsio/runtime'
import { CaretDown, CaretForward } from '@vicons/ionicons5'
import Hls from 'hls.js'
import type Artplayer from 'artplayer'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
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
import { GetTvSnapshot } from '../../bindings/github.com/bing/bing-play/internal/service/tvservice'
import type {
    ChannelItem,
    SourceStatus
} from '../../bindings/github.com/bing/bing-play/internal/tv/models'

/** 后端频道更新事件名，需与 Go 端 tv.EventChannelsUpdated 保持一致 */
const TV_CHANNELS_UPDATED_EVENT = 'tv:channels-updated'

/** ArtPlayer 实例上挂载的 hls 实例，切换频道时先销毁旧的 */
type PlayerWithHls = Artplayer & { hls?: Hls }

/** 本地 Go HTTP 服务地址，与 Play.vue 保持一致 */
const API_BASE = 'http://127.0.0.1:9090'

/** 左侧频道列表宽度，复用播放页的宽度设置 */
const sidebarWidth = ref(settingNumber(SettingKeys.sidebarWidth, 260))
let resizeStartX = 0
let resizeStartWidth = 0

/** 频道数据与订阅源状态 */
const channels = ref<ChannelItem[]>([])
const sources = ref<SourceStatus[]>([])
/** 搜索关键词 */
const keyword = ref('')
/** 当前播放的频道 */
const currentChannel = ref<ChannelItem | null>(null)
/** 播放错误提示 */
const errorMsg = ref('')
/** 当前展开的分组，同一时间只展开一个，避免频道过多时创建过多 DOM */
const expandedGroup = ref<string | null>(null)

const message = useMessage()

/** 分组结构：组名为 group-title，缺失时归入「未分组」 */
interface ChannelGroup {
    title: string
    channels: ChannelItem[]
}

/** 频道行固定高度，需与 .channel 样式保持一致，供虚拟列表计算 */
const channelItemHeight = 32
/** 超过该数量的分组改用虚拟列表，避免一次创建过多 DOM */
const virtualThreshold = 100
/** 虚拟列表最大高度，超出后在分组内滚动 */
const virtualMaxHeight = 320

/** 大分组的虚拟列表高度 */
function virtualListHeight(group: ChannelGroup) {
    return Math.min(group.channels.length * channelItemHeight, virtualMaxHeight)
}

/** 搜索模式下强制展开所有分组，便于直接看到匹配结果 */
const searching = computed(() => keyword.value.trim() !== '')

const filteredChannels = computed(() => {
    const search = keyword.value.trim().toLowerCase()
    if (!search) return channels.value
    return channels.value.filter((channel) => {
        const haystack = `${channel.name} ${channel.tvgName} ${channel.groupTitle}`.toLowerCase()
        return haystack.includes(search)
    })
})

const groups = computed<ChannelGroup[]>(() => {
    const result = new Map<string, ChannelItem[]>()
    for (const channel of filteredChannels.value) {
        const title = channel.groupTitle || t('tv.group_unknown')
        const group = result.get(title)
        if (group) {
            group.push(channel)
        } else {
            result.set(title, [channel])
        }
    }
    return Array.from(result, ([title, list]) => ({ title, channels: list }))
})

/** 所有订阅源中最新的同步时间 */
const lastSyncAt = computed(() => {
    let latest = ''
    for (const source of sources.value) {
        if (source.lastSyncAt && source.lastSyncAt > latest) latest = source.lastSyncAt
    }
    return latest
})

/** 任意订阅源最近一次同步失败的错误码 */
const syncError = computed(() => {
    return sources.value.find((source) => source.lastError)?.lastError ?? ''
})

const statusText = computed(() => {
    if (syncError.value) return t(syncError.value)
    if (!lastSyncAt.value) return t('tv.sync_never')
    return t('tv.sync_last', { Time: lastSyncAt.value })
})

/**
 * 播放地址统一从这里生成。
 * 自动模式：默认直连订阅源；该订阅源直连失败过一次后，本次运行内改走本地 Go 代理。
 */
const proxySources = ref<Set<number>>(new Set())

function proxyPlayUrl(streamUrl: string): string {
    return `${API_BASE}/tv/proxy?src=${encodeURIComponent(streamUrl)}`
}

function resolvePlayUrl(channel: ChannelItem): string {
    if (proxySources.value.has(channel.sourceId)) {
        return proxyPlayUrl(channel.streamUrl)
    }
    return channel.streamUrl
}

const currentUrl = computed(() => {
    const channel = currentChannel.value
    return channel ? resolvePlayUrl(channel) : ''
})

/**
 * 直连出现网络类致命错误（CORS、403、超时等）时切换到代理重试。
 * 同一个订阅源只兜底一次，代理仍失败则按播放失败处理。
 */
function tryProxyFallback(failedUrl: string): boolean {
    const channel = currentChannel.value
    if (!channel || !channel.sourceId || failedUrl !== currentUrl.value) return false
    if (proxySources.value.has(channel.sourceId)) return false

    const next = new Set(proxySources.value)
    next.add(channel.sourceId)
    proxySources.value = next

    errorMsg.value = ''
    message.info(t('tv.proxy_fallback'))
    return true
}

/** 频道分组是否展开 */
function isGroupExpanded(title: string) {
    return searching.value || expandedGroup.value === title
}

/** 展开指定分组，同时收起其他分组 */
function toggleGroup(title: string) {
    expandedGroup.value = expandedGroup.value === title ? null : title
}

/** 播放指定频道；自动展开该频道所在分组，并收起其他分组 */
function playChannel(channel: ChannelItem) {
    errorMsg.value = ''
    currentChannel.value = channel
    expandedGroup.value = channel.groupTitle || t('tv.group_unknown')
}

/** 当前频道在可见列表中的位置，供播放器「上一个/下一个」频道使用 */
const currentIndex = computed(() => {
    const channel = currentChannel.value
    if (!channel) return -1
    return filteredChannels.value.findIndex((item) => item.id === channel.id)
})

const hasPrevChannel = computed(() => currentIndex.value > 0)

const hasNextChannel = computed(
    () => currentIndex.value >= 0 && currentIndex.value < filteredChannels.value.length - 1
)

/** 切换到可见列表中的上一个频道 */
function playPrevChannel() {
    const index = currentIndex.value
    if (index <= 0) return
    playChannel(filteredChannels.value[index - 1])
}

/** 切换到可见列表中的下一个频道 */
function playNextChannel() {
    const index = currentIndex.value
    const list = filteredChannels.value
    if (index < 0 || index >= list.length - 1) return
    playChannel(list[index + 1])
}

/** 台标加载失败时隐藏图片，露出底层的默认台标 */
function onLogoError(event: Event) {
    const img = event.target as HTMLImageElement
    img.style.visibility = 'hidden'
}

/** 虚拟列表会复用节点，新台标加载成功后恢复显示 */
function onLogoLoad(event: Event) {
    const img = event.target as HTMLImageElement
    img.style.visibility = 'visible'
}

function onPlayerError(_error: Error, failedUrl?: string) {
    // 已切换到其它频道后迟到的错误直接忽略
    if (failedUrl && currentUrl.value && failedUrl !== currentUrl.value) return
    errorMsg.value = t('tv.play_failed')
}

/**
 * hls.js 直播参数：
 * - 关闭低延迟模式，普通 IPTV 不是 LL-HLS，开启反而更容易 stall
 * - liveMaxLatencyDurationCount 限制落后分片数，长时间卡顿后自动追到直播边缘
 * - 放宽分片加载超时与重试次数，适应不稳定的直播源
 */
const hlsLiveConfig = {
    // 是否启用低延迟模式（LL-HLS）。普通 IPTV 不支持 LL-HLS，关闭避免额外开销
    lowLatencyMode: false,
    // 直播同步点：播放位置落后直播边缘的分片数。3 个分片 ≈ 18 秒延迟（按每片 6s 计）
    liveSyncDurationCount: 3,
    // 最大允许延迟：超过这个分片数会强制 seek 回同步点，避免越播越慢
    liveMaxLatencyDurationCount: 10,
    // 向前缓冲最大时长（秒），预加载多少秒视频
    maxBufferLength: 20,
    // 向前缓冲的硬上限（秒），防止缓冲无限增长
    maxMaxBufferLength: 30,
    // 向后保留缓冲时长（秒），播放位置之前的分片保留多久，控制内存
    backBufferLength: 30,
    // 主列表（master m3u8）单次加载超时（毫秒）
    manifestLoadingTimeOut: 5000,
    // 主列表加载失败重试次数，0 表示不重试，失败立即切代理
    manifestLoadingMaxRetry: 0,
    // 子列表（某个清晰度的 m3u8）单次加载超时（毫秒）
    levelLoadingTimeOut: 5000,
    // 子列表加载失败重试次数，1 表示重试 1 次
    levelLoadingMaxRetry: 1,
    // 单个视频分片（ts/fmp4）单次加载超时（毫秒），分片较大给足时间
    fragLoadingTimeOut: 20000,
    // 单个分片加载失败重试次数
    fragLoadingMaxRetry: 6,
    // 根据播放器窗口大小限制最高清晰度，省带宽
    capLevelToPlayerSize: true
}

/** ArtPlayer 配置；直播源统一按 m3u8 交给 hls.js 播放 */
const playerOptions = computed(() => {
    const lang = i18nState.lang.toLowerCase()
    return {
        fullscreen: true,
        fullscreenWeb: true,
        theme: activeAccentColor.value,
        lang,
        i18n: {
            [lang]: {
                ...i18nState.playerMessages,
                // 直播场景里这两个按钮用于切换频道，不是切换剧集
                Previous: t('tv.prev_channel'),
                Next: t('tv.next_channel')
            }
        },
        type: 'm3u8',
        isLive: true,
        customType: {
            m3u8(video: HTMLVideoElement, url: string, art: Artplayer) {
                const player = art as PlayerWithHls
                player.hls?.destroy()

                if (Hls.isSupported()) {
                    const hls = new Hls(hlsLiveConfig)
                    player.hls = hls
                    hls.loadSource(url)
                    hls.attachMedia(video)

                    // 直连时连续分片加载失败就切代理；成功加载后清零
                    let loadErrorCount = 0
                    // 代理模式下网络错误的重试次数
                    let proxyRetryCount = 0
                    // 媒体错误恢复次数，避免无限 recover
                    let mediaRecoverCount = 0

                    hls.on(Hls.Events.FRAG_LOADED, () => {
                        loadErrorCount = 0
                        proxyRetryCount = 0
                    })
                    hls.on(Hls.Events.LEVEL_LOADED, () => {
                        loadErrorCount = 0
                    })

                    hls.on(Hls.Events.ERROR, (_event, data) => {
                        if (!data) return

                        if (data.fatal) {
                            if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
                                // 直连的网络类错误（跨域、403、超时等）自动切换本地代理重试
                                if (tryProxyFallback(url)) return
                                // 已在代理模式：延迟后重新加载，最多两次
                                if (proxyRetryCount < 2) {
                                    proxyRetryCount += 1
                                    const delay = proxyRetryCount * 1000
                                    window.setTimeout(() => {
                                        if (player.hls === hls) hls.startLoad()
                                    }, delay)
                                    return
                                }
                            } else if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
                                if (mediaRecoverCount < 2) {
                                    mediaRecoverCount += 1
                                    hls.recoverMediaError()
                                    return
                                }
                            }
                            errorMsg.value = t('tv.play_failed')
                            return
                        }

                        // 非致命的网络错误：
                        // - 主列表/子列表加载失败：直播源基本是跨域/403，重试无意义，立即切代理
                        // - 分片加载失败：单个分片可能偶发失败，累计 3 次再切代理
                        if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
                            if (
                                data.details === Hls.ErrorDetails.MANIFEST_LOAD_ERROR ||
                                data.details === Hls.ErrorDetails.LEVEL_LOAD_ERROR
                            ) {
                                tryProxyFallback(url)
                                return
                            }
                            if (data.details === Hls.ErrorDetails.FRAG_LOAD_ERROR) {
                                loadErrorCount += 1
                                if (loadErrorCount >= 3 && tryProxyFallback(url)) {
                                    loadErrorCount = 0
                                }
                            }
                        }
                    })

                    art.on('destroy', () => hls.destroy())
                    return
                }

                // Safari 等原生支持 HLS 的环境直接交给 video
                if (video.canPlayType('application/vnd.apple.mpegurl')) {
                    video.src = url
                    return
                }

                errorMsg.value = t('tv.unsupported')
            }
        }
    }
})

/** 快照请求进行中 / 期间又收到更新事件 */
let snapshotLoading = false
let snapshotDirty = false

/**
 * 读取频道列表与订阅源状态。
 * 后端一次返回完整快照，避免两次 IPC 与重复查询。
 */
async function fetchSnapshot() {
    if (snapshotLoading) {
        snapshotDirty = true
        return
    }

    snapshotLoading = true
    try {
        const snapshot = await GetTvSnapshot()
        channels.value = snapshot?.channels ?? []
        sources.value = snapshot?.sources ?? []
    } catch (error) {
        console.warn('获取电视频道快照失败:', error)
    } finally {
        snapshotLoading = false
        if (snapshotDirty) {
            snapshotDirty = false
            void fetchSnapshot()
        }
    }
}

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

let offChannelsUpdated: (() => void) | undefined

onMounted(async () => {
    await loadSettings()
    sidebarWidth.value = clampSidebarWidth(settingNumber(SettingKeys.sidebarWidth, 260))

    void fetchSnapshot()

    // 后台同步完成后自动刷新频道列表与状态
    offChannelsUpdated = Events.On(TV_CHANNELS_UPDATED_EVENT, () => {
        void fetchSnapshot()
    })
})

onBeforeUnmount(() => {
    offChannelsUpdated?.()
    window.removeEventListener('mousemove', onResizeMove)
    window.removeEventListener('mouseup', onResizeEnd)
})
</script>

<style lang="scss" scoped>
.tv {
    flex: 1 1 0;
    min-height: 0;
    display: flex;
    overflow: hidden;
}

.left-panel {
    flex: 0 0 auto;
    box-sizing: border-box;
    min-height: 0;
    border-right: 1px solid var(--app-border);
    padding: 10px;
    display: flex;
    flex-direction: column;
}

.panel-header {
    flex-shrink: 0;
    margin-bottom: 8px;
}

.title {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 8px;
    color: var(--app-text);
    min-width: 0;
}

.title-text {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.search {
    border-radius: 6px;
}

.status {
    margin-top: 6px;
    font-size: 11px;
    color: var(--app-text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.status.error {
    color: #e88080;
}

/* 频道列表与播放区之间的拖动分隔条 */
.resize-handle {
    flex: 0 0 4px;
    cursor: col-resize;
    background: transparent;
    transition: background 0.15s;
}

.resize-handle:hover {
    background: var(--app-accent, #3b82f6);
}

.list {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden;
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
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.channel {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 32px;
    box-sizing: border-box;
    padding: 0 6px;
    font-size: 12px;
    cursor: pointer;
    color: var(--app-text-muted);
}

/* 大分组使用虚拟列表，内部滚动 */
.group-virtual {
    width: 100%;
}

.channel .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.channel:hover {
    background: var(--app-hover-row);
}

.channel.active {
    background: var(--app-active-row);
    color: var(--app-text);
}

/* 默认台标在懒加载完成前以及加载失败时显示，加载成功后由 img 覆盖 */
.channel-logo {
    position: relative;
    width: 22px;
    height: 22px;
    flex-shrink: 0;
    overflow: hidden;
    border-radius: 4px;
    background: var(--app-hover) url("../assets/tv-default.svg") center / contain no-repeat;
}

.channel-logo img {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: contain;
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

.empty-icon .antenna {
    stroke: var(--app-icon-line);
    stroke-width: 1.4;
}

.empty-icon .screen-line {
    stroke: var(--app-icon-line);
    stroke-width: 1.4;
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

.error-tip {
    position: absolute;
    left: 50%;
    bottom: 16px;
    transform: translateX(-50%);
    max-width: 80%;
    padding: 8px 14px;
    border-radius: 6px;
    font-size: 13px;
    pointer-events: none;
    z-index: 10;
    color: #fff;
    background: rgba(220, 38, 38, 0.85);
}
</style>
