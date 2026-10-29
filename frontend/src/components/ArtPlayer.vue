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
}>(), {
  url: '',
  options: () => ({})
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
  error: [error: Error]
}>()

const containerRef = ref<HTMLDivElement | null>(null)
const player = shallowRef<Artplayer | null>(null)

const currentUrl = computed(() => props.url || props.options.url || '')

function createPlayer(): void {
  if (!containerRef.value) return

  const art = new Artplayer({
    ...props.options,
    url: currentUrl.value,
    autoplay: props.autoplay ?? props.options.autoplay ?? false,
    muted: props.muted ?? props.options.muted ?? false,
    container: containerRef.value
  })

  art.on('ready', () => emit('ready', art))
  art.on('play', () => emit('play'))
  art.on('pause', () => emit('pause'))
  art.on('video:ended', () => emit('ended'))
  art.on('seek', (time) => emit('seek', time))
  art.on('video:timeupdate', () => emit('timeupdate', art.currentTime))
  art.on('fullscreen', (state) => emit('fullscreen', state))
  art.on('fullscreenWeb', (state) => emit('fullscreenWeb', state))
  art.on('error', (error) => emit('error', error))

  player.value = art
}

onMounted(createPlayer)

watch(currentUrl, (url) => {
  const art = player.value
  if (!art || !url || art.url === url) return

  art.switchUrl(url).catch((error: Error) => emit('error', error))
})

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
