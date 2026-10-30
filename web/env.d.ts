/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** API 根地址；留空表示同源（开发时由 vite 代理） */
  readonly VITE_API_BASE?: string
  /** 终端确认命令里显示的服务器地址，例如 159.75.78.7 */
  readonly VITE_SSH_HOST?: string
  /** 终端确认命令里显示的端口，例如 12331 */
  readonly VITE_SSH_PORT?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<Record<string, unknown>, Record<string, unknown>, unknown>
  export default component
}
