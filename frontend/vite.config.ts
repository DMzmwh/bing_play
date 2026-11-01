import { defineConfig } from "vite";
import path from "path";
import vue from "@vitejs/plugin-vue";
import wails from "@wailsio/runtime/plugins/vite";
import AutoImport from "unplugin-auto-import/vite";
import { NaiveUiResolver } from "unplugin-vue-components/resolvers";
import Components from "unplugin-vue-components/vite";

// https://vitejs.dev/config/
export default defineConfig({
  server: {
    // 开发端口固定 9245：wails3 dev -port 与 build/Taskfile.yml 的 --port 使用同一个值，
    // 只有 wails3 显式注入 WAILS_VITE_PORT 时才跟随，避免两端端口不一致。
    // strictPort 保证端口被占用时直接报错，而不是悄悄换一个端口启动。
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },

  plugins: [
    vue(),

    wails("./bindings"),

    // 自动导入 API
    AutoImport({
      imports: [
        "vue",
        "vue-router",
        {
          "naive-ui": [
            "useDialog",
            "useMessage",
            "useNotification",
            "useLoadingBar",
            "useThemeVars",
          ],
        },
      ],
      dts: true,
    }),

    // 自动导入 Naive UI 组件
    Components({
      resolvers: [NaiveUiResolver()],
      dts: true,
    }),
  ],

  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
});
