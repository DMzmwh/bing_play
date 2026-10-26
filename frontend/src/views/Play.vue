<template>
    <div class="play">
        <!-- 左侧播放列表 -->
        <div class="left-panel">
            <div class="title">播放列表 [3]</div>

            <div class="search">
                <n-input size="small" placeholder="请输入搜索关键词" />
            </div>

            <div class="list">
                <div class="group">
                    <div class="group-title">除恶 [16]</div>
                    <div
                        v-for="i in 50"
                        :key="i"
                        class="episode"
                        :class="{ active: i === 1 }"
                    >
                        <span>{{ i.toString().padStart(2, '0') }} 第{{ i }}集</span>
                        <span class="time">00:00:00</span>
                    </div>
                </div>

                <div class="group small">
                    <div>牛气有为 [26]</div>
                </div>

                <div class="group small">
                    <div>低智商犯罪 [24]</div>
                </div>
            </div>
        </div>

        <!-- 中间播放区 -->
        <div class="center">
            <ArtPlayer
                :url="`http://127.0.0.1:9090/video/local/11`"
                :options="{ fullscreenWeb:true, theme: '#3b82f6' }"
                autoplay
                @fullscreen-web="syncWindowMaximise"
                />
            <!-- 底部控制条 -->
            <!-- <PlayerPanel /> -->
        </div>
    </div>
</template>

<script lang="ts" setup>
import { Window } from '@wailsio/runtime'

// 进入播放器网页全屏前窗口是否已最大化，用于退出时恢复
let wasMaximised = false

async function syncWindowMaximise(state: boolean) {
    try {
        if (state) {
            wasMaximised = await Window.IsMaximised()
            if (!wasMaximised) {
                await Window.Maximise()
            }
        } else if (!wasMaximised) {
            await Window.UnMaximise()
        }
    } catch (error) {
        console.warn('同步窗口最大化失败：', error)
    }
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
    flex: 0 0 260px; /* ✅ flex简写：grow 0 shrink 0 basis 260px，固定宽度 */
    min-height: 0;
    border-right: 1px solid #1d2733;
    padding: 10px;
    overflow-y: auto;
    overflow-x: hidden;
}

.title {
    color: #fff;
    margin-bottom: 10px;
    flex-shrink: 0; // 固定不压缩
}

.search {
    border-radius: 6px;
    flex-shrink: 0; // 固定不压缩
    margin-bottom: 8px;
}

.search :deep(.n-input) {
    background: #111827;
    border-radius: 6px;
}


.group-title {
    margin-top: 10px;
    color: #999;
    font-size: 12px;
}

.episode {
    display: flex;
    justify-content: space-between;
    padding: 6px;
    font-size: 12px;
    cursor: pointer;
    color: #bbb;
}

.episode:hover {
    background: #182230;
}

.episode.active {
    background: #1e2a3a;
    color: #fff;
}

.time {
    color: #666;
}

.group.small {
    > div {
        margin-top: 10px;
        color: #999;
        font-size: 12px;
    }
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
