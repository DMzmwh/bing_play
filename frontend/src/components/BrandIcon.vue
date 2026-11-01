<template>
    <!--
        品牌图标（图片资源）：以字母 B 为原型，竖笔画 + 上下两个字碗构成 B，
        字碗底部的两条斜笔画对应“兵”字下部的“八”，整体即为“兵”的抽象字形。
        - nav：带渐变底板的导航栏主图标；
        - mark：无底板、仅保留字形的配套图标。
        深浅主题各一套 PNG，由 data-theme 驱动切换，无需监听主题变化。
    -->
    <span class="brand-icon" :style="{ width: px, height: px }">
        <img class="brand-icon__img brand-icon__img--dark" :src="darkSrc" :alt="label" draggable="false" />
        <img class="brand-icon__img brand-icon__img--light" :src="lightSrc" :alt="label" draggable="false" />
    </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'

import navDark from '@/assets/icons/nav-dark.png'
import navLight from '@/assets/icons/nav-light.png'
import markDark from '@/assets/icons/mark-dark.png'
import markLight from '@/assets/icons/mark-light.png'

const props = withDefaults(
    defineProps<{
        /** nav：带底板的主图标；mark：无底板的配套图标 */
        variant?: 'nav' | 'mark'
        /** 显示尺寸（像素），按 2 倍图输出，高分屏下依然清晰 */
        size?: number
        /** 无障碍读屏文案 */
        label?: string
    }>(),
    {
        variant: 'mark',
        size: 24,
        label: 'Bing Play',
    }
)

const px = computed(() => `${props.size}px`)
const darkSrc = computed(() => (props.variant === 'nav' ? navDark : markDark))
const lightSrc = computed(() => (props.variant === 'nav' ? navLight : markLight))
</script>

<style scoped>
.brand-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    line-height: 0;
}

.brand-icon__img {
    width: 100%;
    height: 100%;
    display: block;
    object-fit: contain;
    user-select: none;
}

/* 默认深色主题使用深色版，浅色主题切换到浅色版 */
.brand-icon__img--light {
    display: none;
}

/* 必须把整个选择器包进 :global，否则 scoped 编译会丢失后代选择器 */
:global(:root[data-theme='light'] .brand-icon__img--light) {
    display: block;
}

:global(:root[data-theme='light'] .brand-icon__img--dark) {
    display: none;
}
</style>
