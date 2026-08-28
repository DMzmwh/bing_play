import { createApp } from "vue";
import App from "./App.vue";
// 通用字体
import "vfonts/Lato.css";
// 等宽字体
import router from "@/router"; // 必须引入路由
import "vfonts/FiraCode.css";
// import "./style.css";

const app = createApp(App);

app.use(router); // ✅ 必须挂载路由
app.mount("#app");
