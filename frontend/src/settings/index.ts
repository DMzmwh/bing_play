import { computed, reactive, readonly } from 'vue'
import {
    GetSettings,
    ResetSettings,
    SaveSettings
} from '../../bindings/github.com/bing/bing-play/internal/service/settingsservice'
import type {
    SettingCategory,
    SettingItem,
    SettingsSaveResult
} from '../../bindings/github.com/bing/bing-play/internal/service/models'

/** 与后端 internal/settings/keys.go 保持一致的逻辑设置键名 */
export const SettingKeys = {
    language: 'language',
    downloadDir: 'download.dir',
    crawlerEnabled: 'crawler.enabled',
    crawlerPort: 'crawler.port',
    themeMode: 'ui.theme_mode',
    accentColor: 'ui.accent_color',
    resourcePageSize: 'ui.resource_page_size',
    sidebarWidth: 'ui.sidebar_width'
} as const

/** 默认强调色，未加载设置或值非法时使用 */
const DEFAULT_ACCENT = '#3b82f6'
/** 默认界面主题 */
const DEFAULT_THEME = 'dark'

const state = reactive({
    loading: false,
    saving: false,
    loaded: false,
    categories: [] as SettingCategory[],
    items: [] as SettingItem[],
    /** 已保存的正式值 */
    values: {} as Record<string, string>
})

/** 设置页里的临时预览值：修改颜色时立即生效，离开设置页时清除 */
const preview = reactive<Record<string, string>>({})

/** 已发起的读取请求，避免多个页面重复调用后端 */
let pending: Promise<void> | undefined

/** 应用全局强调色，供 Layout / Play / Settings 等页面共用 */
function applyAccent(color: string) {
    const safe = /^#[0-9a-fA-F]{6}$/.test(color) ? color : DEFAULT_ACCENT
    document.documentElement.style.setProperty('--app-accent', safe)
    document.documentElement.style.setProperty('--app-accent-soft', `${safe}1f`)
}

/** 切换深色/浅色主题：通过 data-theme 驱动全局 CSS 变量并同步原生控件配色 */
function applyTheme(mode: string) {
    const safe = mode === 'light' ? 'light' : 'dark'
    document.documentElement.dataset.theme = safe
    document.documentElement.style.colorScheme = safe
}

/** 把一组设置值应用到界面：强调色 + 主题模式 */
export function applySettingsValues(values: Record<string, string>) {
    applyAccent(values[SettingKeys.accentColor] || DEFAULT_ACCENT)
    applyTheme(values[SettingKeys.themeMode] || DEFAULT_THEME)
}

/** 恢复为已保存设置对应的界面效果 */
export function applyCommittedSettings() {
    applySettingsValues(state.values)
}

/** 实时预览设置界面效果，不写入数据库 */
export function previewSettingValue(key: string, value: string) {
    preview[key] = value
    if (key === SettingKeys.accentColor) applyAccent(value)
    if (key === SettingKeys.themeMode) applyTheme(value)
}

/** 清除所有临时预览，并恢复已保存的界面效果 */
export function clearSettingsPreview() {
    for (const key of Object.keys(preview)) delete preview[key]
    applyCommittedSettings()
}

function replaceSnapshot(categories: SettingCategory[] | null, items: SettingItem[] | null) {
    state.categories = categories ?? []
    state.items = items ?? []

    const values: Record<string, string> = {}
    for (const item of state.items) {
        values[item.key] = item.value
    }
    state.values = values
    state.loaded = true
    applySettingsValues(values)
}

async function fetchSettings(): Promise<void> {
    state.loading = true
    try {
        const snapshot = await GetSettings()
        replaceSnapshot(snapshot?.categories ?? null, snapshot?.items ?? null)
    } catch (error) {
        console.warn('加载设置失败:', error)
    } finally {
        state.loading = false
    }
}

/** 读取设置；force 为 true 时强制重新拉取 */
export function loadSettings(force = false): Promise<void> {
    if (force) pending = undefined
    pending ??= fetchSettings()
    return pending
}

/** 读取已保存的字符串设置值 */
export function settingValue(key: string, fallback = ''): string {
    return state.values[key] ?? fallback
}

/** 读取数字设置值，非法时使用 fallback */
export function settingNumber(key: string, fallback: number): number {
    const parsed = Number(state.values[key])
    return Number.isFinite(parsed) && state.values[key] !== '' ? parsed : fallback
}

/** 保存设置，成功后同步本地状态与界面效果 */
export async function saveSettings(values: Record<string, string>): Promise<SettingsSaveResult | null> {
    state.saving = true
    try {
        const result = await SaveSettings(values)
        if (result?.success && result.snapshot) {
            replaceSnapshot(result.snapshot.categories, result.snapshot.items)
        }
        return result
    } finally {
        state.saving = false
    }
}

/**
 * 单个设置项就地保存（例如资源页切换每页条数、播放页调整列表宽度）。
 * 返回是否保存成功，调用方负责错误提示。
 */
export async function saveSettingValue(key: string, value: string): Promise<boolean> {
    try {
        const result = await saveSettings({ [key]: value })
        return Boolean(result?.success)
    } catch (error) {
        console.warn('保存设置项失败:', key, error)
        return false
    }
}

/** 重置为默认值，成功后同步本地状态与界面效果 */
export async function resetSettings(): Promise<void> {
    state.saving = true
    try {
        const snapshot = await ResetSettings()
        replaceSnapshot(snapshot?.categories ?? null, snapshot?.items ?? null)
    } finally {
        state.saving = false
    }
}

/** 只读状态：页面渲染设置分组、设置项与已保存值 */
export const settingsState = readonly(state)

/** 当前生效的强调色（预览值优先），供 Naive UI 主题与播放器使用 */
export const activeAccentColor = computed(
    () => preview[SettingKeys.accentColor] || state.values[SettingKeys.accentColor] || DEFAULT_ACCENT
)

/** 当前生效的界面主题（预览值优先），供 Naive UI 组件切换深色/浅色 */
export const activeThemeMode = computed<'dark' | 'light'>(() => {
    const mode = preview[SettingKeys.themeMode] || state.values[SettingKeys.themeMode] || DEFAULT_THEME
    return mode === 'light' ? 'light' : 'dark'
})
