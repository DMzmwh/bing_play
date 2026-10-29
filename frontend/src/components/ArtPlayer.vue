<template>
  <div ref="containerRef" class="artplayer-container"></div>
</template>

<script setup lang="ts">
import Artplayer, { type Option } from 'artplayer'
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'

/** ArtPlayer 配置项，container 由组件内部维护，url 可由 props.url 覆盖 */
type PlayerOptions = Omit<Option, 'container' | 'url'> & { url?: string }

const props = withDefaults(defineProps<{
  /** 视频地址，优先级高于 options.url，变化时会自动切换 */
  url?: string
  /** ArtPlayer 配置 */
  options?: PlayerOptions
  /** 是否自动播放，未指定时沿用 options.autoplay */
  autoplay?: boolean
  /** 是否静音，未指定时沿用 options.muted */
  muted?: boolean
  /** 是否存在上一个，false 时按钮置灰 */
  hasPrev?: boolean
  /** 是否存在下一个，false 时按钮置灰 */
  hasNext?: boolean
}>(), {
  url: '',
  options: () => ({}),
  hasPrev: false,
  hasNext: false
})

const emit = defineEmits<{
  ready: [player: Artplayer]
  play: []
  pause: []
  ended: []
  seek: [currentTime: number]
  timeupdate: [currentTime: number]
  fullscreen: [state: boolean]
  fullscreenWeb: [state: boolean]
  prev: []
  next: []
  error: [error: Error, url: string]
}>()

const containerRef = ref<HTMLDivElement | null>(null)
const player = shallowRef<Artplayer | null>(null)

const currentUrl = computed(() => props.url || props.options.url || '')

/** 上一个/下一个按钮图标，使用内联 SVG 以便直接交给 ArtPlayer controls */
const PREV_ICON = `<svg viewBox="0 0 24 24" width="22" height="22" fill="currentColor"><rect x="5" y="5" width="2" height="14"/><path d="M19 5v14L9 12z"/></svg>`
const NEXT_ICON = `<svg viewBox="0 0 24 24" width="22" height="22" fill="currentColor"><rect x="17" y="5" width="2" height="14"/><path d="M5 5v14l10-7z"/></svg>`

/** 从 ArtPlayer 语言包取文案，缺失时回退到默认中文 */
function playerText(key: string, fallback: string): string {
  const { lang, i18n } = props.options
  const table = lang ? i18n?.[lang] : i18n && Object.values(i18n)[0]
  return table?.[key] ?? fallback
}

/** 没有上一个/下一个时置灰并禁用点击 */
function navControlStyle(enabled: boolean) {
  return {
    opacity: enabled ? '1' : '0.4',
    cursor: enabled ? 'pointer' : 'not-allowed'
  }
}

function prevControl() {
  return {
    name: 'prevEpisode',
    position: 'left',
    index: 11,
    html: PREV_ICON,
    tooltip: playerText('Previous', '上一个'),
    style: navControlStyle(props.hasPrev),
    click: () => {
      if (props.hasPrev) emit('prev')
    }
  }
}

function nextControl() {
  return {
    name: 'nextEpisode',
    position: 'left',
    index: 12,
    html: NEXT_ICON,
    tooltip: playerText('Next', '下一个'),
    style: navControlStyle(props.hasNext),
    click: () => {
      if (props.hasNext) emit('next')
    }
  }
}

function createPlayer(): void {
  if (!containerRef.value) return

  const art = new Artplayer({
    ...props.options,
    url: currentUrl.value,
    autoplay: props.autoplay ?? props.options.autoplay ?? false,
    muted: props.muted ?? props.options.muted ?? false,
    container: containerRef.value,
    controls: [...(props.options.controls ?? []), prevControl(), nextControl()]
  })

  art.on('ready', () => emit('ready', art))
  art.on('play', () => emit('play'))
  art.on('pause', () => emit('pause'))
  art.on('video:ended', () => emit('ended'))
  art.on('seek', (time) => emit('seek', time))
  art.on('video:timeupdate', () => emit('timeupdate', art.currentTime))
  art.on('fullscreen', (state) => emit('fullscreen', state))
  art.on('fullscreenWeb', (state) => emit('fullscreenWeb', state))
  // 直接监听原生 video:error，事件立即触发且属于当前视频源
  art.on('video:error', () => {
    const mediaError = art.video.error
    // 带上失败地址，调用方可以忽略切换后迟到的旧错误
    emit('error', new Error(mediaError?.message || ''), art.video.currentSrc || art.url)
  })

  player.value = art
}

onMounted(createPlayer)

watch(currentUrl, async (url) => {
  const art = player.value
  if (!art || !url || art.url === url) return

  try {
    await art.switchUrl(url)
    // 播完后切换下一集时视频处于暂停态，switchUrl 不会自动续播，需要显式播放
    if (props.autoplay ?? props.options.autoplay ?? false) {
      await art.play()
    }
  } catch (error) {
    emit('error', error as Error, url)
  }
})

// 上一集/下一集可用状态变化时更新控制条按钮样式
watch(
  () => [props.hasPrev, props.hasNext] as const,
  () => {
    const art = player.value
    if (!art) return

    art.controls.update(prevControl())
    art.controls.update(nextControl())
  }
)

/** 切换语言后重建实例：ArtPlayer 的 tooltip/提示文案在初始化时写入，只能重建才能全部同步 */
watch(
  () => [props.options.lang, props.options.i18n] as const,
  ([lang, i18n]) => {
    const table = lang ? i18n?.[lang] : i18n && Object.values(i18n)[0]
    // 语言包还没加载完（空表）时跳过，等拿到文案后再重建一次
    if (!player.value || !table || Object.keys(table).length === 0) return
    recreatePlayer()
  },
  { deep: true }
)

/** 重建播放器并恢复播放状态，用于文案变化等必须重新初始化的场景 */
function recreatePlayer(): void {
  const art = player.value
  if (!art) return

  const currentTime = art.currentTime
  const volume = art.volume
  const playbackRate = art.playbackRate
  const playing = art.playing

  art.destroy()
  createPlayer()

  const next = player.value
  if (!next) return

  next.once('ready', () => {
    next.currentTime = currentTime
    next.volume = volume
    next.playbackRate = playbackRate
    if (playing) next.play().catch(() => undefined)
  })
}

onBeforeUnmount(() => {
  player.value?.destroy()
  player.value = null
})

function seek(time: number): void {
  if (player.value) {
    player.value.currentTime = time
  }
}

function setVolume(volume: number): void {
  if (player.value) {
    player.value.volume = Math.min(Math.max(volume, 0), 1)
  }
}

function screenshot(name?: string): Promise<string | undefined> {
  return player.value?.screenshot(name) ?? Promise.resolve(undefined)
}

defineExpose({
  /** 获取 ArtPlayer 实例 */
  getPlayer: (): Artplayer | null => player.value,
  play: (): Promise<void> | undefined => player.value?.play(),
  pause: (): void => player.value?.pause(),
  toggle: (): void => player.value?.toggle(),
  switchUrl: (url: string): Promise<void> | undefined => player.value?.switchUrl(url),
  seek,
  forward: (time: number): void => {
    if (player.value) player.value.forward = time
  },
  backward: (time: number): void => {
    if (player.value) player.value.backward = time
  },
  setVolume,
  screenshot,
  getCurrentTime: (): number => player.value?.currentTime ?? 0,
  getDuration: (): number => player.value?.duration ?? 0
})
</script>

<style scoped>
.artplayer-container {
  width: 100%;
  height: 100%;
  min-height: 300px;
  background: #000;
  overflow: hidden;
}
</style>
