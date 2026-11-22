import { defineConfig } from "vite";
import path from "path";
import vue from "@vitejs/plugin-vue";
import wails from "@wailsio/runtime/plugins/vite";
import AutoImport from "unplugin-auto-import/vite";
import { NaiveUiResolver } from "unplugin-vue-components/resolvers";
import Components from "unplugin-vue-components/vite";

// https://vitejs.dev/config/
export default defineConfig(({ mode }) => ({
  server: {
    // 开发端口固定 9245：wails3 dev -port 与 build/Taskfile.yml 的 --port 使用同一个值，
    // 只有 wails3 显式注入 WAILS_VITE_PORT 时才跟随，避免两端端口不一致。
    // strictPort 保证端口被占用时直接报错，而不是悄悄换一个端口启动。
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },

  // IS_TV：Android TV 构建使用 --mode tv（见 package.json 的 build:tv），
  // 同时兼容直接注入 VITE_IS_TV=true 的方式。
  // 编译期常量，Rollup 据此做死代码消除；桌面端构建时为 false，行为不变。
  define: {
    IS_TV: JSON.stringify(mode === "tv" || process.env.VITE_IS_TV === "true"),
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
}));
