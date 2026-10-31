import Download from "@/views/Download.vue";
import Find from "@/views/Find.vue";
import Library from "@/views/Library.vue";
import Play from "@/views/Play.vue";
import Resources from "@/views/Resources.vue";
import Settings from "@/views/Settings.vue";
import { createRouter, createWebHistory } from "vue-router";

export default createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/", redirect: "/play" },
    { path: "/play", component: Play },
    { path: "/download", component: Download },
    { path: "/library", component: Library },
    { path: "/resources", component: Resources },
    { path: "/find", component: Find },
    { path: "/settings", component: Settings },
  ],
});
