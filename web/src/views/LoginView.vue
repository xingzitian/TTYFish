<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ApiError, api } from '../api/client'
import type { SSHChallenge } from '../api/types'
import { useAuth } from '../stores/auth'

const router = useRouter()
const auth = useAuth()

const sshHost = import.meta.env.VITE_SSH_HOST ?? '127.0.0.1'
const sshPort = import.meta.env.VITE_SSH_PORT ?? '12331'

// ---- SSH 挑战登录 ----
const sshUsername = ref('')
const challenge = ref<SSHChallenge | null>(null)
const secondsLeft = ref(0)
const starting = ref(false)
let pollTimer: number | undefined
let tickTimer: number | undefined

// ---- 密码登录 ----
const passwordForm = ref({ username: '', password: '' })
const loggingIn = ref(false)

const command = computed(() => {
  if (!challenge.value) return ''
  return `ssh -p ${sshPort} ${challenge.value.username}@${sshHost} auth ${challenge.value.code}`
})

function stopTimers(): void {
  if (pollTimer !== undefined) window.clearTimeout(pollTimer)
  if (tickTimer !== undefined) window.clearInterval(tickTimer)
  pollTimer = undefined
  tickTimer = undefined
}

onBeforeUnmount(stopTimers)

async function startSSH(): Promise<void> {
  const username = sshUsername.value.trim()
  if (!username) {
    ElMessage.warning('先填注册时确定的用户名')
    return
  }
  starting.value = true
  try {
    challenge.value = await api.startSSHChallenge(username)
    secondsLeft.value = challenge.value.expires_in
    tickTimer = window.setInterval(() => {
      secondsLeft.value -= 1
      if (secondsLeft.value <= 0) {
        stopTimers()
        challenge.value = null
        ElMessage.warning('确认码过期了，重新获取一次')
      }
    }, 1000)
    schedulePoll(challenge.value.poll_interval_ms)
  } catch (err) {
    ElMessage.error(err instanceof ApiError ? err.message : '获取确认码失败')
  } finally {
    starting.value = false
  }
}

function schedulePoll(delay: number): void {
  pollTimer = window.setTimeout(async () => {
    const current = challenge.value
    if (!current) return
    try {
      const result = await api.pollSSHChallenge(current.poll_token)
      if (result.state === 'ok') {
        stopTimers()
        challenge.value = null
        auth.loginWithSession(result.session)
        ElMessage.success('登录成功')
        await router.push('/account')
        return
      }
      if (result.state === 'expired') {
        stopTimers()
        challenge.value = null
        ElMessage.warning('确认码已失效，重新获取一次')
        return
      }
      schedulePoll(current.poll_interval_ms)
    } catch (err) {
      // 网络抖动：不要立刻放弃，退避后再试
      if (err instanceof ApiError && err.status === 429) {
        ElMessage.warning('请求太频繁，稍等一下')
        schedulePoll(current.poll_interval_ms * 2)
        return
      }
      schedulePoll(current.poll_interval_ms)
    }
  }, delay)
}

function cancelChallenge(): void {
  stopTimers()
  challenge.value = null
}

async function copyCommand(): Promise<void> {
  try {
    await navigator.clipboard.writeText(command.value)
    ElMessage.success('命令已复制')
  } catch {
    ElMessage.warning('复制失败，手动选中吧')
  }
}

async function loginWithPassword(): Promise<void> {
  const { username, password } = passwordForm.value
  if (!username || !password) {
    ElMessage.warning('用户名和密码都要填')
    return
  }
  loggingIn.value = true
  try {
    await auth.loginWithPassword(username, password)
    ElMessage.success('登录成功')
    await router.push('/account')
  } catch (err) {
    ElMessage.error(err instanceof ApiError ? err.message : '登录失败')
  } finally {
    loggingIn.value = false
  }
}
</script>

<template>
  <div class="auth-page">
    <div class="auth-stack">
      <h1 class="page-title">登录</h1>
      <p class="page-sub">用注册时确定的用户名登录，支持 SSH 密钥或账号密码。</p>

      <el-card class="card" shadow="never">
        <template #header>
          <strong>用 SSH 密钥登录</strong>
        </template>

        <template v-if="!challenge">
          <p class="hint" style="margin-top: 0">
            需要在电脑终端里完成一次确认。第一次用请先
            <router-link to="/register">创建账号</router-link> 并绑定公钥。
          </p>
          <div class="actions">
            <el-input
              v-model="sshUsername"
              placeholder="用户名"
              style="max-width: 240px"
              @keyup.enter="startSSH"
            />
            <el-button type="primary" :loading="starting" @click="startSSH">获取确认命令</el-button>
          </div>
        </template>

        <template v-else>
          <p class="hint" style="margin-top: 0">在终端里执行这条命令：</p>
          <code class="cmd">{{ command }}</code>
          <p class="hint">
            还剩 <b>{{ secondsLeft }} 秒</b>，只能用一次。确认后这个页面会自动跳转。
          </p>
          <div class="actions">
            <el-button @click="copyCommand">复制命令</el-button>
            <el-button text @click="cancelChallenge">取消</el-button>
          </div>
        </template>
      </el-card>

      <el-card class="card" shadow="never">
        <template #header>
          <strong>用密码登录</strong>
        </template>
        <el-form label-position="top" @submit.prevent="loginWithPassword">
          <el-form-item label="用户名">
            <el-input v-model="passwordForm.username" autocomplete="username" />
          </el-form-item>
          <el-form-item label="密码">
            <el-input
              v-model="passwordForm.password"
              type="password"
              show-password
              autocomplete="current-password"
            />
          </el-form-item>
          <el-button type="primary" :loading="loggingIn" @click="loginWithPassword">
            登录
          </el-button>
        </el-form>
      </el-card>

      <p class="hint" style="text-align: center">
        还没有账号？<router-link to="/register">注册</router-link>
      </p>
    </div>
  </div>
</template>
