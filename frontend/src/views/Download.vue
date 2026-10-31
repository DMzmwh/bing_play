<template>
    <div class="download">
        <div class="toolbar">
            <div class="titles">
                <div class="title">{{ t('download.title') }}</div>
                <div class="subtitle">{{ t('download.subtitle') }}</div>
            </div>
            <div class="toolbar-right">
                <span v-if="options" class="config">
                    {{
                        t('download.config', {
                            MaxConcurrent: options.maxConcurrent,
                            MaxRetries: options.maxRetries
                        })
                    }}
                </span>
                <n-button size="small" :loading="loading" @click="refresh">
                    {{ t('download.refresh') }}
                </n-button>
            </div>
        </div>

        <n-alert
            v-if="loadError"
            class="error"
            type="error"
            closable
            @close="loadError = false"
        >
            {{ t('download.load_failed') }}
        </n-alert>

        <div class="table-wrap">
            <n-tabs
                v-model:value="tab"
                class="tabs"
                type="line"
                :pane-wrapper-style="{ flex: 1, minHeight: 0 }"
                :pane-style="{ height: '100%' }"
            >
                <n-tab-pane
                    name="active"
                    :tab="`${t('download.tab_active')} ${activeJobs.length}`"
                >
                    <n-data-table
                        flex-height
                        size="small"
                        :bordered="false"
                        :columns="activeColumns"
                        :data="activeJobs"
                        :row-key="rowKey"
                        :pagination="false"
                        style="height: 100%"
                    >
                        <template #empty>
                            <div class="empty">
                                <n-empty :description="t('download.empty_title')" />
                                <div class="empty-hint">{{ t('download.empty_hint') }}</div>
                            </div>
                        </template>
                    </n-data-table>
                </n-tab-pane>

                <n-tab-pane name="completed" :tab="t('download.tab_completed')">
                    <n-data-table
                        flex-height
                        size="small"
                        :bordered="false"
                        :columns="completedColumns"
                        :data="completedJobs"
                        :row-key="rowKey"
                        :pagination="false"
                        style="height: 100%"
                    >
                        <template #empty>
                            <div class="empty">
                                <n-empty :description="t('download.completed_empty_title')" />
                                <div class="empty-hint">{{ t('download.completed_empty_hint') }}</div>
                            </div>
                        </template>
                    </n-data-table>
                </n-tab-pane>
            </n-tabs>
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed, h, onBeforeUnmount, onMounted, ref } from 'vue'
import { NButton, NPopconfirm, NProgress, NTag, useMessage } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import { Events } from '@wailsio/runtime'
import { t } from '@/i18n'
import {
    Cancel,
    GetOptions,
    ListActive,
    ListCompleted,
    Pause,
    Resume,
    Retry,
    SetPriority
} from '../../bindings/github.com/bing/bing-play/internal/service/torrentservice'
import type { DownloadOptions } from '../../bindings/github.com/bing/bing-play/internal/service/models'
import type { TorrentJobVO } from '../../bindings/github.com/bing/bing-play/internal/torrentmgr/models/vo/models'

/** 与后端 download.go 保持一致的事件名 */
const PROGRESS_EVENT = 'download:progress'
const STATE_EVENT = 'download:state-changed'
/** 事件之外的低频兜底刷新间隔，防止事件漏推导致列表滞后 */
const FALLBACK_REFRESH_MS = 5000

/** 无法估算剩余时间的状态 */
const INACTIVE_STATUSES = new Set(['download_meta_info', 'queued', 'paused', 'failed', 'cancelled'])

/** 下载中的任务：仅来自后端内存，包含获取元信息、排队、下载、暂停、失败与已取消 */
const activeJobs = ref<TorrentJobVO[]>([])
/** 已完成的任务：仅在打开页面时查询一次数据库 */
const completedJobs = ref<TorrentJobVO[]>([])
const options = ref<DownloadOptions | null>(null)
const loading = ref(false)
const loadError = ref(false)
/** 当前标签页：下载中（未完成）/ 下载完成 */
const tab = ref<'active' | 'completed'>('active')

const message = useMessage()

/** 列表排序权重，与后端 statusRank 保持一致：进行中的任务靠前 */
function statusRank(status: string): number {
    switch (status) {
        case 'downloading':
            return 0
        case 'download_meta_info':
            return 1
        case 'queued':
            return 2
        case 'paused':
            return 3
        case 'failed':
            return 4
        case 'completed':
            return 5
        case 'cancelled':
            return 6
        default:
            return 7
    }
}

/** 任务排序比较器：状态权重 → 优先级 → 创建时间 */
function compareJobs(left: TorrentJobVO, right: TorrentJobVO): number {
    const rankDiff = statusRank(left.status) - statusRank(right.status)
    if (rankDiff !== 0) return rankDiff
    if (left.priority !== right.priority) return right.priority - left.priority
    return right.createdTime.localeCompare(left.createdTime)
}

/** 判断两条任务数据是否有实际变化，忽略对象引用差异 */
function isSameJob(left: TorrentJobVO, right: TorrentJobVO): boolean {
    return (
        left.infoHash === right.infoHash &&
        left.name === right.name &&
        left.fileName === right.fileName &&
        left.status === right.status &&
        left.progress === right.progress &&
        left.downloaded === right.downloaded &&
        left.totalSize === right.totalSize &&
        left.downloadSpeed === right.downloadSpeed &&
        left.uploadSpeed === right.uploadSpeed &&
        left.peers === right.peers &&
        left.etaSeconds === right.etaSeconds &&
        left.priority === right.priority &&
        left.retryCount === right.retryCount &&
        left.errorMessage === right.errorMessage &&
        left.savePath === right.savePath &&
        left.completedTime === right.completedTime
    )
}

/** 合并接口数据：无实际变化时返回 null；变化时复用未变化对象，减少重绘 */
function mergeRows(current: TorrentJobVO[], rows: TorrentJobVO[]): TorrentJobVO[] | null {
    const next = rows.filter((row): row is TorrentJobVO => row != null)
    next.sort(compareJobs)

    const unchanged =
        next.length === current.length &&
        next.every((row, index) => isSameJob(current[index], row))
    if (unchanged) return null

    const previous = new Map(current.map((job) => [job.infoHash, job]))
    return next.map((row) => {
        const old = previous.get(row.infoHash)
        return old && isSameJob(old, row) ? old : row
    })
}

/** 应用下载中任务数据（来自内存），无变化时不触发重绘 */
function applyActiveRows(rows: TorrentJobVO[]) {
    const next = mergeRows(activeJobs.value, rows)
    if (next) activeJobs.value = next
}

/** 应用已完成任务数据（来自数据库），无变化时不触发重绘 */
function applyCompletedRows(rows: TorrentJobVO[]) {
    const next = mergeRows(completedJobs.value, rows)
    if (next) completedJobs.value = next
}

/** 更新下载中列表中的单个任务，数据未变化时直接跳过 */
function upsertActive(payload: TorrentJobVO) {
    const index = activeJobs.value.findIndex((job) => job.infoHash === payload.infoHash)
    if (index >= 0) {
        if (isSameJob(activeJobs.value[index], payload)) return
        activeJobs.value[index] = payload
    } else {
        activeJobs.value.push(payload)
    }
    activeJobs.value.sort(compareJobs)
}

/** 更新已完成列表中的单个任务，数据未变化时直接跳过 */
function upsertCompleted(payload: TorrentJobVO) {
    const index = completedJobs.value.findIndex((job) => job.infoHash === payload.infoHash)
    if (index >= 0) {
        if (isSameJob(completedJobs.value[index], payload)) return
        completedJobs.value[index] = payload
    } else {
        completedJobs.value.push(payload)
    }
    completedJobs.value.sort(compareJobs)
}

/** 事件更新：完成的任务移入已完成列表，其余保持在下载中列表 */
function upsert(payload: TorrentJobVO | null | undefined) {
    if (!payload?.infoHash) return

    if (payload.status === 'completed') {
        const index = activeJobs.value.findIndex((job) => job.infoHash === payload.infoHash)
        if (index >= 0) activeJobs.value.splice(index, 1)
        upsertCompleted(payload)
        return
    }

    upsertActive(payload)
}

/**
 * 拉取下载中任务：只读取后端内存，不查询数据库。
 * showLoading 仅用于首次加载和手动刷新，定时轮询完全静默。
 */
async function loadActive(showLoading = false) {
    console.log('加载下载中任务')
    
    if (showLoading) loading.value = true

    const previousStatuses = new Map(activeJobs.value.map((job) => [job.infoHash, job.status]))
    try {
        applyActiveRows((await ListActive()) ?? [])
        if (loadError.value) loadError.value = false

        // 兜底：任务离开下载中列表且不是手动取消，说明已完成但事件可能漏推，补一次已完成查询
        const stillActive = new Set(activeJobs.value.map((job) => job.infoHash))
        const completedMissed = Array.from(previousStatuses.entries()).some(
            ([infoHash, status]) => !stillActive.has(infoHash) && status !== 'cancelled'
        )
        if (completedMissed) void loadCompleted()
    } catch (error) {
        console.warn('加载下载任务失败:', error)
        // 定时刷新失败不打断界面，仅首次加载或手动刷新时提示
        if (showLoading && !loadError.value) loadError.value = true
    } finally {
        if (showLoading) loading.value = false
    }
}

/** 拉取已完成任务：仅在打开页面时查询一次数据库，不参与定时刷新 */
async function loadCompleted(showError = false) {
    try {
        applyCompletedRows((await ListCompleted()) ?? [])
    } catch (error) {
        console.warn('加载已完成任务失败:', error)
        if (showError && !loadError.value) loadError.value = true
    }
}

/** 手动刷新：只刷新下载中列表，已完成列表保持页面打开时的结果 */
function refresh() {
    void loadActive(true)
}

/** 拉取下载参数，用于展示并发与重试策略 */
async function loadOptions() {
    try {
        options.value = await GetOptions()
    } catch (error) {
        console.warn('加载下载参数失败:', error)
    }
}

/** 把异常转成可展示的文本 */
function reasonOf(error: unknown): string {
    return error instanceof Error ? error.message : String(error)
}

/** 执行任务操作，失败时给出国际化提示 */
async function runAction(action: () => Promise<void>) {
    try {
        await action()
    } catch (error) {
        message.error(t('download.action_failed', { Reason: reasonOf(error) }))
    }
}

/** 字节数转成易读大小，单位符号为通用写法，无需翻译 */
function formatSize(bytes: number | null | undefined): string {
    if (!bytes || bytes <= 0) return '-'

    const units = ['B', 'KB', 'MB', 'GB', 'TB']
    let value = bytes
    let index = 0
    while (value >= 1024 && index < units.length - 1) {
        value /= 1024
        index += 1
    }

    return `${value.toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

/** 下载速度 */
function formatSpeed(bytes: number | null | undefined): string {
    if (!bytes || bytes <= 0) return '-'
    return `${formatSize(bytes)}/s`
}

/** 剩余时间，按秒 / 分 / 小时分段展示 */
function formatEta(job: TorrentJobVO): string {
    if (job.status === 'completed') return '-'
    if (INACTIVE_STATUSES.has(job.status) || job.etaSeconds < 0) {
        return t('download.eta_unknown')
    }

    const seconds = job.etaSeconds
    if (seconds < 60) return t('download.eta_seconds', { Seconds: seconds })
    if (seconds < 3600) {
        return t('download.eta_minutes', {
            Minutes: Math.floor(seconds / 60),
            Seconds: seconds % 60
        })
    }
    return t('download.eta_hours', {
        Hours: Math.floor(seconds / 3600),
        Minutes: Math.floor((seconds % 3600) / 60)
    })
}

/** 状态文案复用种子资源列表的状态 key */
function statusLabel(job: TorrentJobVO): string {
    const text = t(`resources.status.${job.status}`)
    return text !== `resources.status.${job.status}` ? text : t('resources.status.unknown')
}

/** 状态标签颜色 */
function statusType(job: TorrentJobVO): 'success' | 'info' | 'warning' | 'error' | 'default' {
    switch (job.status) {
        case 'completed':
            return 'success'
        case 'downloading':
        case 'download_meta_info':
        case 'queued':
            return 'info'
        case 'paused':
            return 'warning'
        case 'failed':
            return 'error'
        default:
            return 'default'
    }
}

/** 是否处于活跃状态，可暂停 */
function isActive(job: TorrentJobVO): boolean {
    return job.status === 'download_meta_info' || job.status === 'queued' || job.status === 'downloading'
}

/** 是否可继续（暂停后） */
function canResume(job: TorrentJobVO): boolean {
    return job.status === 'paused'
}

/** 是否可重试 */
function canRetry(job: TorrentJobVO): boolean {
    return job.status === 'failed' || job.status === 'cancelled'
}

function rowKey(row: TorrentJobVO) {
    return row.infoHash
}

/** 生成小号文本按钮 */
function textButton(label: string, onClick: () => void, type: 'default' | 'error' = 'default') {
    return h(
        NButton,
        {
            size: 'tiny',
            quaternary: true,
            type,
            onClick
        },
        { default: () => label }
    )
}

/** 下载中任务的表格列 */
const activeColumns = computed<DataTableColumns<TorrentJobVO>>(() => [
    {
        key: 'name',
        title: t('resources.col.name'),
        minWidth: 220,
        ellipsis: { tooltip: true },
        render: (row) => row.name || row.fileName || row.infoHash || '-'
    },
    {
        key: 'status',
        title: t('resources.col.status'),
        width: 160,
        render: (row) =>
            h('div', { class: 'status-cell' }, [
                h(
                    NTag,
                    { size: 'small', bordered: false, type: statusType(row) },
                    { default: () => statusLabel(row) }
                ),
                row.retryCount > 0
                    ? h(
                          'span',
                          { class: 'retry' },
                          t('download.retry_count', { Count: row.retryCount })
                      )
                    : null,
                row.errorMessage
                    ? h('span', { class: 'error-text', title: t(row.errorMessage) }, t(row.errorMessage))
                    : null
            ])
    },
    {
        key: 'progress',
        title: t('download.progress_label'),
        width: 180,
        render: (row) =>
            h('div', { class: 'progress-cell' }, [
                h(NProgress, {
                    type: 'line',
                    percentage: Math.min(100, Math.max(0, row.progress)),
                    height: 6,
                    showIndicator: false,
                    status: row.status === 'failed' ? 'error' : 'default'
                }),
                h('span', { class: 'percent' }, `${row.progress.toFixed(1)}%`)
            ])
    },
    {
        key: 'size',
        title: t('download.item_size_label'),
        width: 170,
        render: (row) =>
            row.totalSize > 0
                ? t('download.item_size', {
                      Downloaded: formatSize(row.downloaded),
                      Total: formatSize(row.totalSize)
                  })
                : formatSize(row.downloaded)
    },
    {
        key: 'speed',
        title: t('download.speed_label'),
        width: 110,
        render: (row) => formatSpeed(row.downloadSpeed)
    },
    {
        key: 'eta',
        title: t('download.eta_label'),
        width: 120,
        render: (row) => formatEta(row)
    },
    {
        key: 'priority',
        title: t('download.priority_label'),
        width: 100,
        render: (row) =>
            h('div', { class: 'priority-cell' }, [
                h('span', { class: 'priority-value' }, String(row.priority)),
                textButton('↑', () => void runAction(() => SetPriority(row.infoHash, row.priority + 1))),
                textButton('↓', () => void runAction(() => SetPriority(row.infoHash, row.priority - 1)))
            ])
    },
    {
        key: 'actions',
        title: t('download.actions_label'),
        width: 210,
        // 固定在右侧，表格横向滚动或窗口变窄时操作按钮始终可见
        fixed: 'right',
        render: (row) =>
            h('div', { class: 'actions' }, [
                isActive(row)
                    ? textButton(t('download.action.pause'), () => void runAction(() => Pause(row.infoHash)))
                    : null,
                canResume(row)
                    ? textButton(t('download.action.resume'), () => void runAction(() => Resume(row.infoHash)))
                    : null,
                canRetry(row)
                    ? textButton(t('download.action.retry'), () => void runAction(() => Retry(row.infoHash)))
                    : null,
                h(
                    NPopconfirm,
                    {
                        onPositiveClick: () => runAction(() => Cancel(row.infoHash))
                    },
                    {
                        trigger: () =>
                            textButton(t('download.action.cancel'), () => {}, 'error'),
                        default: () => t('download.cancel_confirm')
                    }
                )
            ])
    }
])

/** 下载完成任务只展示结果信息，不提供暂停 / 取消操作 */
const completedColumns = computed<DataTableColumns<TorrentJobVO>>(() => [
    {
        key: 'name',
        title: t('resources.col.name'),
        minWidth: 220,
        ellipsis: { tooltip: true },
        render: (row) => row.name || row.fileName || row.infoHash || '-'
    },
    {
        key: 'size',
        title: t('download.item_size_label'),
        width: 170,
        render: (row) => formatSize(row.totalSize || row.downloaded)
    },
    {
        key: 'completedTime',
        title: t('download.completed_time_label'),
        width: 170,
        render: (row) => row.completedTime || '-'
    }
])

let offProgress: (() => void) | undefined
let offState: (() => void) | undefined
let refreshTimer: ReturnType<typeof setInterval> | undefined

onMounted(async () => {
    offProgress = Events.On(PROGRESS_EVENT, (event) =>
        upsert(event.data as TorrentJobVO)
    )
    offState = Events.On(STATE_EVENT, (event) =>
        upsert(event.data as TorrentJobVO)
    )

    await Promise.all([loadActive(true), loadCompleted(true), loadOptions()])

    // 仅轮询下载中任务（内存数据）；已完成列表不参与定时刷新
    refreshTimer = setInterval(() => {
        if (!document.hidden) void loadActive()
    }, FALLBACK_REFRESH_MS)
})

onBeforeUnmount(() => {
    offProgress?.()
    offState?.()
    if (refreshTimer) clearInterval(refreshTimer)
})
</script>

<style scoped>
.download {
    flex: 1;
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 12px 16px 10px;
    box-sizing: border-box;
    overflow: hidden;
}

.toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    flex-shrink: 0;
    margin-bottom: 10px;
}

.titles {
    display: flex;
    align-items: baseline;
    gap: 10px;
    min-width: 0;
}

.title {
    font-size: 15px;
    font-weight: 600;
    color: var(--app-text);
}

.subtitle {
    font-size: 12px;
    color: var(--app-text-muted);
    white-space: nowrap;
}

.toolbar-right {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-shrink: 0;
}

.config {
    color: var(--app-text-muted);
    font-size: 12px;
    white-space: nowrap;
}

.error {
    flex-shrink: 0;
    margin-bottom: 10px;
}

.table-wrap {
    flex: 1;
    min-height: 0;
    overflow: hidden;
    box-sizing: border-box;
    background: var(--app-panel);
    border: 1px solid var(--app-border);
    border-radius: 10px;
}

.tabs {
    height: 100%;
    display: flex;
    flex-direction: column;
}

.tabs :deep(.n-tabs-nav) {
    flex-shrink: 0;
    padding: 0 12px;
}

.empty {
    padding: 48px 0;
    display: flex;
    flex-direction: column;
    align-items: center;
}

.empty-hint {
    margin-top: 8px;
    font-size: 12px;
    color: var(--app-text-dim);
}

.status-cell {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 2px;
}

.retry {
    font-size: 11px;
    color: var(--app-text-dim);
}

.error-text {
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 11px;
    color: #e88080;
}

.progress-cell {
    display: flex;
    align-items: center;
    gap: 8px;
}

.percent {
    flex-shrink: 0;
    font-size: 12px;
    color: var(--app-text-muted);
    font-variant-numeric: tabular-nums;
}

.priority-cell {
    display: flex;
    align-items: center;
    gap: 2px;
}

.priority-value {
    min-width: 24px;
    font-size: 12px;
    color: var(--app-text-muted);
    font-variant-numeric: tabular-nums;
}

.actions {
    display: flex;
    align-items: center;
    gap: 2px;
    flex-wrap: wrap;
}
</style>
