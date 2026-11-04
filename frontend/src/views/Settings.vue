<template>
    <div class="settings-page">
        <div class="page-header">
            <div class="header-text">
                <div class="page-title">{{ t('settings.title') }}</div>
            </div>
            <div class="header-actions">
                <n-popconfirm
                    :positive-text="t('common.btn_ok')"
                    :negative-text="t('common.btn_cancel')"
                    @positive-click="onReset"
                >
                    <template #trigger>
                        <n-button quaternary size="small" :disabled="saving">
                            {{ t('settings.reset') }}
                        </n-button>
                    </template>
                    {{ t('settings.reset_confirm') }}
                </n-popconfirm>
                <n-button
                    type="primary"
                    size="small"
                    :loading="saving"
                    :disabled="!ready"
                    @click="onSave"
                >
                    {{ t('settings.save') }}
                </n-button>
            </div>
        </div>

        <div class="settings-scroll">
            <n-alert v-if="loadFailed" class="load-error" type="error">
                {{ t('settings.load_failed') }}
            </n-alert>

            <n-spin :show="loading">
                <div class="settings-body">
                    <section
                        v-for="category in categories"
                        :key="category.key"
                        class="category"
                    >
                        <div class="category-title">{{ t(category.title) }}</div>
                        <div class="setting-list">
                            <div
                                v-for="item in itemsOf(category.key)"
                                :key="item.key"
                                class="setting-item"
                                :class="{ dependent: item.dependsOnKey }"
                            >
                                <div class="setting-info">
                                    <div class="setting-label">{{ t(item.label) }}</div>
                                    <div class="setting-help">{{ t(item.help) }}</div>
                                    <div v-if="fieldErrors[item.key]" class="setting-error">
                                        {{ fieldErrors[item.key] }}
                                    </div>
                                </div>

                                <div class="setting-control">
                                    <n-switch
                                        v-if="item.type === 'switch'"
                                        :value="draftBool(item.key)"
                                        @update:value="(value) => setDraft(item.key, value ? 'true' : 'false')"
                                    />

                                    <div
                                        v-else-if="item.type === 'select'"
                                        class="control-inline"
                                    >
                                        <n-select
                                            class="control-select"
                                            :value="draft[item.key]"
                                            :options="selectOptions(item)"
                                            @update:value="(value) => onSelectChange(item.key, value)"
                                        />
                                        <n-button
                                            v-if="item.key === SettingKeys.language"
                                            secondary
                                            :loading="exportingLocale"
                                            :disabled="saving"
                                            @click="onExportLocaleTemplate"
                                        >
                                            {{ t('settings.export_locale_template') }}
                                        </n-button>
                                    </div>

                                    <n-input-number
                                        v-else-if="item.type === 'number'"
                                        class="control-number"
                                        :value="draftNumber(item)"
                                        :min="item.min"
                                        :max="item.max"
                                        :step="item.step"
                                        @update:value="(value) => setDraft(item.key, value === null ? '' : String(value))"
                                    />

                                    <div
                                        v-else-if="item.type === 'text'"
                                        class="control-path"
                                    >
                                        <n-input
                                            class="control-text"
                                            :value="draft[item.key]"
                                            :placeholder="item.defaultValue"
                                            @update:value="(value) => setDraft(item.key, value)"
                                            @blur="onTextBlur(item.key)"
                                        />
                                        <n-button
                                            v-if="item.key === SettingKeys.downloadDir"
                                            secondary
                                            :loading="pickingDir"
                                            :disabled="saving"
                                            @click="onPickDownloadDir"
                                        >
                                            {{ t('settings.browse') }}
                                        </n-button>
                                    </div>

                                    <n-color-picker
                                        v-else-if="item.type === 'color'"
                                        :value="draft[item.key]"
                                        :modes="['hex']"
                                        :show-alpha="false"
                                        @update:value="(value) => onColorChange(item.key, value)"
                                    />
                                </div>
                            </div>
                        </div>
                    </section>
                </div>
            </n-spin>
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useMessage } from 'naive-ui'
import { i18nState, setLang, t } from '@/i18n'
import {
    SelectDownloadDir,
    ValidateDownloadDir
} from '../../bindings/github.com/bing/bing-play/internal/service/settingsservice'
import { ExportLocaleTemplate } from '../../bindings/github.com/bing/bing-play/internal/i18n/i18nservice'
import type { SettingOption } from '../../bindings/github.com/bing/bing-play/internal/service/models'
import {
    SettingKeys,
    clearSettingsPreview,
    loadSettings,
    previewSettingValue,
    resetSettings,
    saveSettings,
    settingValue,
    settingsState
} from '@/settings'

/** 设置项最小值/最大值校验失败时返回的国际化 key */
const errorKeyMap: Record<string, string> = {
    [SettingKeys.language]: 'settings.error.language',
    [SettingKeys.downloadDir]: 'settings.error.download_dir',
    [SettingKeys.themeMode]: 'settings.error.theme_mode',
    [SettingKeys.crawlerPort]: 'settings.error.port',
    [SettingKeys.resourcePageSize]: 'settings.error.page_size',
    [SettingKeys.accentColor]: 'settings.error.color',
    [SettingKeys.sidebarWidth]: 'settings.error.sidebar_width'
}

const message = useMessage()

/** 表单草稿：保存前只改这里，不直接写数据库 */
const draft = reactive<Record<string, string>>({})
/** 单个设置项的校验错误 */
const fieldErrors = reactive<Record<string, string>>({})
const loading = computed(() => settingsState.loading)
const saving = computed(() => settingsState.saving)
const ready = computed(() => settingsState.loaded)
const loadFailed = ref(false)
/** 目录选择器是否正在打开，避免重复点击 */
const pickingDir = ref(false)
/** 语言模板是否正在导出 */
const exportingLocale = ref(false)

const categories = computed(() => settingsState.categories)

/** 设置页可见项：隐藏项与依赖未满足的项不展示 */
const visibleItems = computed(() =>
    settingsState.items.filter((item) => {
        if (item.hidden) return false
        if (item.dependsOnKey) {
            return draft[item.dependsOnKey] === (item.dependsOnValue ?? '')
        }
        return true
    })
)

/** 分组下的设置项 */
function itemsOf(categoryKey: string) {
    return visibleItems.value.filter((item) => item.category === categoryKey)
}

function syncDraft() {
    for (const item of settingsState.items) {
        draft[item.key] = item.value
    }
}

function setDraft(key: string, value: string) {
    draft[key] = value
    delete fieldErrors[key]
}

function draftBool(key: string): boolean {
    return draft[key] === 'true'
}

function draftNumber(item: { key: string; defaultValue: string }): number {
    const value = Number(draft[item.key] ?? item.defaultValue)
    return Number.isFinite(value) ? value : Number(item.defaultValue)
}

function selectOptions(item: { options: readonly SettingOption[] | null }) {
    return (item.options ?? []).map((option) => ({
        // 后端下发的标签可能是国际化 key，t() 对非 key 的原生名称会原样返回
        label: t(option.label),
        value: option.value
    }))
}

/** 下拉切换：语言立即生效，主题实时预览，其它项只更新草稿 */
function onSelectChange(key: string, value: string | number | null) {
    if (value === null) return
    const next = String(value)
    setDraft(key, next)
    if (key === SettingKeys.themeMode) {
        previewSettingValue(key, next)
    }
    if (key === SettingKeys.language && next !== i18nState.lang) {
        void setLang(next).catch((error) => {
            console.warn('切换语言失败:', error)
            message.error(t('settings.save_failed'))
        })
    }
}

/** 导出当前语言包作为扩展模板，用户可据此新增或覆盖文案 */
async function onExportLocaleTemplate() {
    if (exportingLocale.value) return
    exportingLocale.value = true
    try {
        const path = await ExportLocaleTemplate()
        message.success(t('settings.export_locale_success', { Path: path }))
    } catch (error) {
        console.warn('导出语言模板失败:', error)
        message.error(t('settings.export_locale_failed'))
    } finally {
        exportingLocale.value = false
    }
}

/** 颜色选择器实时预览强调色 */
function onColorChange(key: string, value: string | null) {
    if (!value) return
    setDraft(key, value)
    previewSettingValue(key, value)
}

/** 打开系统目录选择器，回填绝对路径并立即校验 */
async function onPickDownloadDir() {
    if (pickingDir.value) return
    pickingDir.value = true
    try {
        const selected = await SelectDownloadDir(
            t('settings.download_dir_dialog_title'),
            draft[SettingKeys.downloadDir] ?? ''
        )
        if (!selected) return // 用户取消
        setDraft(SettingKeys.downloadDir, selected)
        await validateDownloadDirField()
    } catch (error) {
        console.warn('选择下载目录失败:', error)
        message.error(t('settings.download_dir_pick_failed'))
    } finally {
        pickingDir.value = false
    }
}

/** 文本输入框失焦时做即时校验（目前仅下载目录） */
async function onTextBlur(key: string) {
    if (key === SettingKeys.downloadDir) {
        await validateDownloadDirField()
    }
}

/** 调用后端校验下载目录是否可用，并展示到对应设置项下方 */
async function validateDownloadDirField(): Promise<boolean> {
    const value = (draft[SettingKeys.downloadDir] ?? '').trim()
    if (!value) {
        fieldErrors[SettingKeys.downloadDir] = t('settings.error.download_dir')
        return false
    }
    try {
        const errorKey = await ValidateDownloadDir(value)
        if (errorKey) {
            fieldErrors[SettingKeys.downloadDir] = t(errorKey)
            return false
        }
        delete fieldErrors[SettingKeys.downloadDir]
        return true
    } catch (error) {
        console.warn('校验下载目录失败:', error)
        fieldErrors[SettingKeys.downloadDir] = t('settings.error.download_dir')
        return false
    }
}

/** 保存前按后端下发的控件元数据做基础合法性校验 */
function validateDraft(): boolean {
    let valid = true
    for (const item of visibleItems.value) {
        const value = draft[item.key] ?? ''
        const error = validateItem(item, value)
        if (error) {
            fieldErrors[item.key] = t(error)
            valid = false
        }
    }
    return valid
}

function validateItem(
    item: { key: string; type: string; min: number; max: number; options: readonly SettingOption[] | null },
    value: string
): string {
    if (item.type === 'number') {
        const parsed = Number(value)
        if (!value || !Number.isFinite(parsed) || parsed < item.min || parsed > item.max) {
            return errorKeyMap[item.key] ?? 'settings.save_failed'
        }
    }
    if (item.type === 'text' && !value.trim()) {
        return errorKeyMap[item.key] ?? 'settings.save_failed'
    }
    if (item.type === 'color' && !/^#[0-9a-fA-F]{6}$/.test(value)) {
        return errorKeyMap[item.key] ?? 'settings.error.color'
    }
    if (item.type === 'select') {
        const options = item.options ?? []
        if (!options.some((option) => option.value === value)) {
            return errorKeyMap[item.key] ?? 'settings.save_failed'
        }
    }
    return ''
}

async function onSave() {
    for (const key of Object.keys(fieldErrors)) delete fieldErrors[key]

    if (!validateDraft()) {
        message.error(t('settings.save_failed'))
        return
    }

    const values: Record<string, string> = {}
    for (const item of visibleItems.value) {
        const value = draft[item.key] ?? item.defaultValue
        values[item.key] = item.type === 'text' ? value.trim() : value
    }

    try {
        const result = await saveSettings(values)
        if (!result) {
            message.error(t('settings.save_failed'))
            return
        }
        if (!result.success) {
            for (const item of result.errors ?? []) {
                fieldErrors[item.key] = t(item.message)
            }
            const first = result.errors?.[0]?.message
            message.error(t(first || 'settings.save_failed'))
            return
        }

        const language = draft[SettingKeys.language] ?? settingValue(SettingKeys.language, 'en')
        if (language !== i18nState.lang) {
            await setLang(language)
        }

        clearSettingsPreview()
        syncDraft()
        message.success(t('settings.save_success'))
    } catch (error) {
        console.warn('保存设置失败:', error)
        message.error(t('settings.save_failed'))
    }
}

async function onReset() {
    for (const key of Object.keys(fieldErrors)) delete fieldErrors[key]

    try {
        await resetSettings()

        const language = settingValue(SettingKeys.language, 'en')
        if (language !== i18nState.lang) {
            await setLang(language)
        }

        clearSettingsPreview()
        syncDraft()
        message.success(t('settings.reset_success'))
    } catch (error) {
        console.warn('重置设置失败:', error)
        message.error(t('settings.reset_failed'))
    }
}

onMounted(async () => {
    // 每次进入设置页都重新读取，确保顶部语言切换等外部修改能回显
    await loadSettings(true)
    loadFailed.value = !settingsState.loaded
    syncDraft()
})

onBeforeUnmount(() => {
    // 未保存的颜色/宽度预览恢复为已保存值
    clearSettingsPreview()

    // 未保存的语言预览恢复为已保存语言
    const language = settingValue(SettingKeys.language, '')
    if (language && language !== i18nState.lang) {
        void setLang(language).catch((error) => console.warn('恢复语言失败:', error))
    }
})
</script>

<style scoped>
.settings-page {
    flex: 1;
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    background: var(--app-bg);
}

/* 顶部操作区固定，不随设置项滚动 */
.page-header {
    flex: 0 0 auto;
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
    flex-wrap: wrap;
    padding: 16px 24px 14px;
    box-sizing: border-box;
    background: var(--app-bg);
}

/* 仅设置项内容区域滚动，高度自适应剩余空间 */
.settings-scroll {
    flex: 1;
    min-height: 0;
    overflow-x: hidden;
    overflow-y: auto;
    padding: 16px 24px 24px;
    box-sizing: border-box;
    scrollbar-gutter: stable;
}

.page-title {
    font-size: 18px;
    font-weight: 600;
    color: var(--app-text);
}

.header-actions {
    display: flex;
    align-items: center;
    gap: 10px;
}

.load-error {
    margin-bottom: 12px;
}

.settings-body {
    display: flex;
    flex-direction: column;
    gap: 16px;
    width: 100%;
    min-width: 0;
}

.category {
    background: var(--app-panel);
    border: 1px solid var(--app-border);
    border-radius: 10px;
    padding: 4px 18px 6px;
}

.category-title {
    padding: 12px 0 10px;
    font-size: 13px;
    font-weight: 600;
    color: var(--app-text-strong);
    border-bottom: 1px solid var(--app-border);
}

.setting-list {
    display: flex;
    flex-direction: column;
}

.setting-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 20px;
    padding: 14px 0;
    border-bottom: 1px solid var(--app-border-soft);
}

.setting-item:last-child {
    border-bottom: none;
}

.setting-info {
    flex: 1;
    min-width: 0;
}

.setting-label {
    font-size: 13px;
    color: var(--app-text-strong);
}

.setting-help {
    margin-top: 4px;
    font-size: 12px;
    line-height: 1.5;
    color: var(--app-text-dim);
}

.setting-error {
    margin-top: 6px;
    font-size: 12px;
    color: var(--app-danger, #f87171);
}

.setting-control {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    justify-content: flex-end;
    min-width: 180px;
}

.control-select,
.control-number {
    width: 180px;
}

.control-inline {
    display: flex;
    align-items: center;
    gap: 8px;
}

.control-path {
    display: flex;
    align-items: center;
    gap: 8px;
}

.control-text {
    width: 320px;
}

/* 依赖项：缩进显示在开启项下方 */
.setting-item.dependent {
    padding-left: 16px;
}

@media (max-width: 640px) {
    .page-header {
        padding: 14px 14px 12px;
    }

    .settings-scroll {
        padding: 12px 14px 20px;
    }

    .header-actions {
        width: 100%;
        justify-content: flex-end;
    }

    .setting-item {
        align-items: flex-start;
        flex-direction: column;
        gap: 10px;
    }

    .setting-control,
    .control-select,
    .control-number,
    .control-path,
    .control-text {
        width: 100%;
    }

    .setting-control {
        justify-content: flex-start;
        min-width: 0;
    }
}
</style>
