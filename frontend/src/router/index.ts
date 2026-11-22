import Tv from "@/views/Tv.vue";
import { createRouter, createWebHashHistory, type RouteRecordRaw } from "vue-router";

// Android TV 构建（IS_TV=true）只注册直播页面；桌面端保留全部页面。
// 非 Tv 的页面用动态 import，这样 TV 构建时 Rollup 会消除 else 分支，
// 这些页面的 chunk 不会被打包进 TV 产物，桌面端行为不变（只是按需拆 chunk）。
const routes: RouteRecordRaw[] = IS_TV
  ? [
      { path: "/", redirect: "/tv" },
      { path: "/tv", component: Tv },
    ]
  : [
      { path: "/", redirect: "/play" },
      { path: "/play", component: () => import("@/views/Play.vue") },
      { path: "/tv", component: Tv },
      { path: "/download", component: () => import("@/views/Download.vue") },
      { path: "/library", component: () => import("@/views/Library.vue") },
      { path: "/resources", component: () => import("@/views/Resources.vue") },
      { path: "/find", component: () => import("@/views/Find.vue") },
      { path: "/settings", component: () => import("@/views/Settings.vue") },
    ];

export default createRouter({
  history: createWebHashHistory(),
  routes,
});
