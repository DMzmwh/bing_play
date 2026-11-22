/// <reference types="vite/client" />

// 由 vite.config.ts 的 define 注入：桌面端为 false，Android TV 构建为 true。
declare const IS_TV: boolean;
