<template>
    <div class="resources">
        <div class="toolbar">
            <div class="toolbar-left">
                <n-input
                    v-model:value="keyword"
                    class="search"
                    clearable
                    size="small"
                    :placeholder="t('resources.search_placeholder')"
                />
                <n-select
                    v-model:value="sortValue"
                    class="sort"
                    size="small"
                    :options="sortOptions"
                />
            </div>
            <div class="toolbar-right">
                <span v-if="checkedIds.length" class="selected">
                    {{ t("resources.batch_selected", { Count: checkedIds.length }) }}
                </span>
                <n-button
                    size="small"
                    type="primary"
                    :loading="downloading"
                    :disabled="!checkedIds.length"
                    @click="batchDownload"
                >
                    {{ t("resources.batch_download") }}
                </n-button>
                <div class="total">{{ t("resources.total", { Count: total }) }}</div>
            </div>
        </div>

        <n-alert
            v-if="loadError"
            class="error"
            type="error"
            closable
            @close="loadError = false"
        >
            {{ t("resources.load_failed") }}
        </n-alert>

        <div class="table-wrap">
            <n-data-table
                v-model:checked-row-keys="checkedIds"
                remote
                flex-height
                size="small"
                :bordered="false"
                :loading="loading"
                :columns="columns"
                :data="items"
                :row-key="rowKey"
                :pagination="pagination"
                style="height: 100%"
            >
                <template #empty>
                    <div class="empty">
                        <n-empty :description="emptyTitle" />
                        <div class="empty-hint">{{ emptyHint }}</div>
                    </div>
                </template>
            </n-data-table>
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed, h, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { NButton, NIcon, NTag, useMessage } from 'naive-ui'
import type { DataTableColumns, PaginationProps } from 'naive-ui'
import { CopyOutline } from '@vicons/ionicons5'
import { t } from '@/i18n'
import { SettingKeys, loadSettings, settingNumber, saveSettingValue } from '@/settings'
import { ListCrawled } from '../../bindings/github.com/bing/bing-play/internal/service/resourceservice'
import { DownloadResources } from '../../bindings/github.com/bing/bing-play/internal/service/torrentservice'
import type {
    CrawledResourceItem,
    CrawledResourceQuery
} from '../../bindings/github.com/bing/bing-play/internal/service/models'

/** 搜索输入防抖时间，避免每敲一个字都查一次库 */
const SEARCH_DEBOUNCE = 300

const items = ref<CrawledResourceItem[]>([])
const total = ref(0)
const loading = ref(false)
const loadError = ref(false)
const keyword = ref('')
const sortValue = ref('latest')
const page = ref(1)
// 默认每页条数来自系统设置，用户切换分页大小时自动保存
const pageSize = ref(settingNumber(SettingKeys.resourcePageSize, 100))
// 多选下载：选中行的资源 ID，切换页码后自动清空，避免与分页数据不一致
const checkedIds = ref<number[]>([])
const downloading = ref(false)

const message = useMessage()

/** 排序选项：值直接对应后端的排序字段与方向 */
const sortOptions = computed(() =>
    ['latest', 'oldest', 'largest', 'smallest'].map((key) => ({
        label: t(`resources.sort.${key}`),
        value: key
    }))
)

/** 把排序选项转换成后端参数 */
function sortParams(): Pick<CrawledResourceQuery, 'sortBy' | 'sortOrder'> {
    switch (sortValue.value) {
        case 'oldest':
            return { sortBy: 'create_time', sortOrder: 'asc' }
        case 'largest':
            return { sortBy: 'total_size', sortOrder: 'desc' }
        case 'smallest':
            return { sortBy: 'total_size', sortOrder: 'asc' }
        default:
            return { sortBy: 'create_time', sortOrder: 'desc' }
    }
}

/** 字节数转成易读大小，只做单位换算，不涉及文案 */
function formatSize(bytes: number): string {
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

/** 状态文案：直接使用后端派生的展示态，缺失时显示未知 */
function statusLabel(item: CrawledResourceItem): string {
    const key = `resources.status.${item.displayState || 'unknown'}`
    const text = t(key)
    return text !== key ? text : t('resources.status.unknown')
}

/** 状态标签颜色，按展示态语义区分 */
function statusType(item: CrawledResourceItem): 'success' | 'info' | 'warning' | 'error' | 'default' {
    switch (item.displayState) {
        case 'completed':
        case 'local_ready':
            return 'success'
        case 'downloading':
        case 'fetching_meta':
        case 'queued':
        case 'verifying':
        case 'retrying':
            return 'info'
        case 'paused':
        case 'file_lost':
        case 'corrupted':
        case 'local_lost':
            return 'warning'
        case 'failed':
            return 'error'
        default:
            return 'default'
    }
}

/** 写入剪贴板：优先 Clipboard API，不可用时回退 execCommand，兼容 WebView 与旧浏览器 */
async function copyToClipboard(text: string): Promise<boolean> {
    if (navigator.clipboard && window.isSecureContext) {
        try {
            await navigator.clipboard.writeText(text)
            return true
        } catch {
            // 权限被拒绝时继续走回退方案
        }
    }

    try {
        const textarea = document.createElement('textarea')
        textarea.value = text
        textarea.setAttribute('readonly', '')
        textarea.style.position = 'fixed'
        textarea.style.top = '-9999px'
        textarea.style.opacity = '0'
        document.body.appendChild(textarea)
        textarea.select()
        const ok = document.execCommand('copy')
        document.body.removeChild(textarea)
        return ok
    } catch {
        return false
    }
}

/** 复制磁力链接，并按结果给出国际化提示 */
async function copyMagnet(url: string) {
    if (!url) return

    if (await copyToClipboard(url)) {
        message.success(t('resources.copy_success'))
    } else {
        message.error(t('resources.copy_failed'))
    }
}

/** 批量下载选中的资源：后端按资源 ID 入队，返回新增 / 跳过 / 失败统计 */
async function batchDownload() {
    if (!checkedIds.value.length) {
        message.warning(t('resources.batch_empty'))
        return
    }

    downloading.value = true
    try {
        const result = await DownloadResources(checkedIds.value, 0)
        if (!result) return

        message.success(
            t('resources.batch_result', {
                Added: result.added,
                Skipped: result.skipped,
                Failed: result.failed
            })
        )
        if (result.failed > 0 && result.errors?.length) {
            message.warning(
                t('resources.batch_failed', { Reason: result.errors.join(' / ') })
            )
        }

        checkedIds.value = []
        // 状态已变化，重新拉取当前页以展示最新下载状态
        void load()
    } catch (error) {
        console.warn('批量下载失败:', error)
        message.error(error instanceof Error ? error.message : String(error))
    } finally {
        downloading.value = false
    }
}

/** 行号：跨页连续编号，第 2 页第 1 条为 pageSize + 1 */
function rowIndex(index: number): number {
    return (page.value - 1) * pageSize.value + index + 1
}

function rowKey(row: CrawledResourceItem) {
    return row.id
}

const columns = computed<DataTableColumns<CrawledResourceItem>>(() => {
    const base: DataTableColumns<CrawledResourceItem> = [
        {
            type: 'selection',
            width: 42
        },
        {
            key: 'index',
            title: t('resources.col.index'),
            width: 64,
            align: 'center',
            render: (_row, index) =>
                h('span', { class: 'row-index' }, String(rowIndex(index)))
        },
        {
            key: 'name',
            title: t('resources.col.name'),
            ellipsis: { tooltip: true },
            minWidth: 200
        },
        {
            key: 'totalSize',
            title: t('resources.col.size'),
            width: 110,
            render: (row) => formatSize(row.totalSize)
        }
    ]

    // InfoHash 列同时承载磁力链接的复制入口，复制内容为完整磁力链接
    base.push({
        key: 'infoHash',
        title: t('resources.col.info_hash'),
        minWidth: 190,
        render: (row) =>
            h('div', { class: 'hash-cell' }, [
                h('span', { class: 'hash-value', title: row.infoHash }, row.infoHash || '-'),
                row.sourceUrl
                    ? h(
                          NButton,
                          {
                              class: 'copy-btn',
                              size: 'tiny',
                              quaternary: true,
                              circle: true,
                              title: t('resources.copy'),
                              'aria-label': t('resources.copy'),
                              onClick: () => void copyMagnet(row.sourceUrl)
                          },
                          {
                              icon: () => h(NIcon, { size: 14 }, { default: () => h(CopyOutline) })
                          }
                      )
                    : null
            ])
    })

    base.push({
        key: 'status',
        title: t('resources.col.status'),
        width: 110,
        render: (row) =>
            h(
                NTag,
                { size: 'small', bordered: false, type: statusType(row) },
                { default: () => statusLabel(row) }
            )
    })
    base.push({
        key: 'createdTime',
        title: t('resources.col.created'),
        width: 160,
        render: (row) => row.createdTime || '-'
    })

    return base
})

const pagination = computed<PaginationProps>(() => ({
    page: page.value,
    pageSize: pageSize.value,
    itemCount: total.value,
    showSizePicker: true,
    pageSizes: pageSizeOptions,
    onChange: (nextPage: number) => {
        page.value = nextPage
        void load()
    },
    onUpdatePageSize: (nextSize: number) => {
        pageSize.value = nextSize
        page.value = 1
        // 分页大小由用户在此处选择，直接保存到系统设置
        void saveSettingValue(SettingKeys.resourcePageSize, String(nextSize))
        void load()
    }
}))

const emptyTitle = computed(() =>
    keyword.value ? t('resources.search_empty_title') : t('resources.empty_title')
)

const emptyHint = computed(() =>
    keyword.value ? t('resources.search_empty_hint') : t('resources.empty_hint')
)

/** 请求当前页数据；失败时保留旧数据并给出提示 */
async function load() {
    loading.value = true
    loadError.value = false

    try {
        const result = await ListCrawled({
            keyword: keyword.value.trim(),
            page: page.value,
            pageSize: pageSize.value,
            ...sortParams()
        })
        items.value = result?.items ?? []
        total.value = result?.total ?? 0
        // 当前页数据已替换，清空上一页的选择
        checkedIds.value = []
    } catch (error) {
        console.warn('加载种子资源列表失败:', error)
        loadError.value = true
    } finally {
        loading.value = false
    }
}

/** 关键词变化后回到第一页重新查询 */
let searchTimer: ReturnType<typeof setTimeout> | undefined
watch(keyword, () => {
    if (searchTimer) clearTimeout(searchTimer)
    searchTimer = setTimeout(() => {
        page.value = 1
        void load()
    }, SEARCH_DEBOUNCE)
})

/** 排序变化后回到第一页重新查询 */
watch(sortValue, () => {
    page.value = 1
    void load()
})

/** 分页每页条数只显示数字，标签直接使用数字文本 */
const pageSizeOptions = [50, 100, 200].map((size) => ({
    label: String(size),
    value: size
}))

onMounted(async () => {
    // 先读取设置，确保默认分页大小是最新保存的值
    await loadSettings()
    pageSize.value = settingNumber(SettingKeys.resourcePageSize, 100)
    await load()
})

onBeforeUnmount(() => {
    if (searchTimer) clearTimeout(searchTimer)
})
</script>

<style scoped>
.resources {
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
    gap: 12px;
    flex-wrap: wrap;
    flex-shrink: 0;
    margin-bottom: 10px;
}

.toolbar-left {
    display: flex;
    align-items: center;
    gap: 10px;
    flex: 1;
    min-width: 0;
}

.search {
    max-width: 320px;
    min-width: 160px;
}

.sort {
    width: 150px;
}

.total {
    color: var(--app-text-muted);
    font-size: 12px;
    white-space: nowrap;
}

.toolbar-right {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-shrink: 0;
}

.selected {
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

/* 行号列，等宽数字避免跳动 */
.row-index {
    color: var(--app-text-dim);
    font-variant-numeric: tabular-nums;
}

/* InfoHash 单元格：哈希文本 + 复制磁力链接按钮 */
.hash-cell {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
}

.hash-value {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--app-text-muted);
    font-family: Consolas, monospace;
    font-size: 12px;
    user-select: text;
}

.copy-btn {
    flex-shrink: 0;
}

@media (max-width: 720px) {
    .toolbar-left {
        flex-wrap: wrap;
    }

    .search,
    .sort {
        flex: 1;
        max-width: none;
        width: auto;
    }
}
</style>
