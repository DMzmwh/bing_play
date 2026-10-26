<!-- <template>
  <div ref="containerRef" class="dplayer-container"></div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import DPlayer from 'dplayer'
import type { DPlayerOptions } from 'dplayer'
import 'dplayer/dist/DPlayer.min.css'

type DPlayerVueOptions = Omit<DPlayerOptions, 'container'>

interface Props {
  options: DPlayerVueOptions
}

const props = defineProps<Props>()

const containerRef = ref<HTMLElement>()
let player: DPlayer | null = null

onMounted(() => {
  if (!containerRef.value) {
    return
  }

  player = new DPlayer({
    ...props.options,
    container: containerRef.value
  })
})

onBeforeUnmount(() => {
  player?.destroy()
  player = null
})

function play() {
  player?.play()
}

function pause() {
  player?.pause()
}

function seek(time: number) {
  player?.seek(time)
}

function switchVideo(
  url: string,
  type: 'normal' | 'hls' | 'dash' | 'flv' = 'normal'
) {
}

function getCurrentTime(): number {
  return player?.video.currentTime ?? 0
}

function getDuration(): number {
  return player?.video.duration ?? 0
}

function getPlayer(): DPlayer | null {
  return player
}

defineExpose({
  getPlayer,
  play,
  pause,
  seek,
  switchVideo,
  getCurrentTime,
  getDuration
})
</script>

<style scoped>
.dplayer-container {
  width: 100%;
  height: 100%;
}
</style> -->