import { reactive, readonly } from 'vue'
import {
    GetCurrentLang,
    GetLangInfo,
    GetLangListV2,
    GetPlayerLang,
    SetLang
} from '../../bindings/github.com/bing/bing-play/internal/i18n/i18nservice'
import type { LocaleInfo } from '../../bindings/github.com/bing/bing-play/internal/i18n/models'

/** 语言包缓存：key -> 模板文本，key 按分类点号命名，例如 video.drop_tip -> 松开鼠标播放视频 */
const state = reactive({
    /** 当前生效的语言代码，例如 zh-CN / en */
    lang: '',
    /** 可选语言列表，用于语言切换下拉框 */
    locales: [] as LocaleInfo[],
    messages: {} as Record<string, string>,
    /** ArtPlayer 内置文案：key 为 ArtPlayer 原生 i18n key，例如 Play / Fullscreen */
    playerMessages: {} as Record<string, string>
})

/** 已发起的文案请求，避免重复调用；强制刷新时置空 */
let pending: Promise<void> | undefined

async function fetchMessages(): Promise<void> {
    try {
        const [flat, player] = await Promise.all([GetLangInfo(), GetPlayerLang()])
        state.messages = JSON.parse(flat) as Record<string, string>
        // 后端返回的是可选值映射，过滤掉空值后再交给播放器
        state.playerMessages = Object.entries(player ?? {}).reduce<Record<string, string>>(
            (acc, [key, value]) => {
                if (value) acc[key] = value
                return acc
            },
            {}
        )
    } catch (error) {
        console.warn('load i18n messages failed:', error)
    }
}

/** 加载当前语言的全部文案；force 为 true 时重新拉取（切换语言后使用） */
export function loadMessages(force = false): Promise<void> {
    if (force) pending = undefined
    pending ??= fetchMessages()
    return pending
}

/** 应用启动时初始化：当前语言 + 可选语言列表 + 文案 */
export async function initI18n(): Promise<void> {
    try {
        const [lang, locales] = await Promise.all([GetCurrentLang(), GetLangListV2()])
        state.lang = lang
        state.locales = locales ?? []
    } catch (error) {
        console.warn('load i18n locale list failed:', error)
    }
    await loadMessages()
}

/** 切换语言：后端持久化后，重新拉取对应语言的文案 */
export async function setLang(lang: string): Promise<void> {
    await SetLang(lang)
    state.lang = lang
    await loadMessages(true)
}

/** 填充模板参数，例如 "你好，{{.Name}}" + { Name: '张三' } */
function format(template: string, params?: Record<string, unknown>): string {
    if (!params) return template
    return template.replace(/\{\{\s*\.?(\w+)\s*\}\}/g, (raw, key: string) =>
        key in params ? String(params[key]) : raw
    )
}

/** 获取提示文案，key 不存在时回退为 key 本身 */
export function t(key: string, params?: Record<string, unknown>): string {
    const template = state.messages[key]
    return template ? format(template, params) : key
}

/** 只读状态，供组件读取当前语言和可选语言列表 */
export const i18nState = readonly(state)
