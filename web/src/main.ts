import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import App from './App.vue'
import { router } from './router'
import './styles.css'
import { useAuth } from './stores/auth'

async function start(): Promise<void> {
  try { await useAuth().restore() } catch { /* 未登录或会话失效 */ }
  createApp(App).use(router).use(ElementPlus, { locale: zhCn }).mount('#app')
}
void start()
