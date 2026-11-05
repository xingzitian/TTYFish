<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ApiError, api } from '../api/client'
import type { Key, Points, SessionInfo, TerminalInfo, LoginGroup } from '../api/types'
import { useAuth } from '../stores/auth'

const router = useRouter()
const auth = useAuth()

const keys = ref<Key[]>([])
const loading = ref(false)
const newKey = ref('')
const newKeyName = ref('')
const addingKey = ref(false)

const points = ref<Points | null>(null)
const sessions = ref<SessionInfo[]>([])
const sessionsLoading = ref(false)
const terminal = ref<TerminalInfo | null>(null)
const historyChannel = ref<'web' | 'ssh'>('ssh')
const history = ref<LoginGroup[]>([])
const historyTotal = ref(0)
const historyPage = ref(1)
const historyLoading = ref(false)
let historyRequest = 0
const clock = ref(Date.now())
let clockTimer: number | undefined
let devicesTimer: number | undefined
let disposed = false

const newPassword = ref('')
const revokeOthers = ref(true)
const savingPassword = ref(false)
const savingPasswordLogin = ref(false)

async function handleExpired(): Promise<void> {
  ElMessage.warning('登录已过期，请重新登录')
  auth.clear()
  await router.push('/login')
}

function fail(err: unknown, fallback: string): void {
  if (err instanceof ApiError) {
    if (err.status === 401) {
      void handleExpired()
      return
    }
    if (err.status === 429 && err.retryAfter) {
      ElMessage.error(`请求太频繁，请等 ${err.retryAfter} 秒后再试`)
      return
    }
    ElMessage.error(err.message)
    return
  }
  ElMessage.error(fallback)
}

async function loadKeys(): Promise<void> {
  loading.value = true
  try {
    keys.value = (await api.listKeys()).items
  } catch (err) {
    fail(err, '读取公钥失败')
  } finally {
    loading.value = false
  }
}

async function loadPoints(): Promise<void> {
  try {
    points.value = await api.points()
  } catch (err) {
    // 余额取不到不影响其它功能，安静降级成占位
    fail(err, '读取 PT 余额失败')
  }
}

async function loadSessions(): Promise<void> {
  sessionsLoading.value = true
  try {
    sessions.value = (await api.listSessions()).items
    terminal.value = (await api.onlineTerminal()).terminal
  } catch (err) {
    fail(err, '读取登录会话失败')
  } finally {
    sessionsLoading.value = false
  }
}

async function loadHistory(): Promise<void> {
  const request = ++historyRequest
  historyLoading.value = true
  try {
    const result = await api.loginHistory(historyChannel.value, (historyPage.value - 1) * 10)
    if (request === historyRequest) { history.value = result.items; historyTotal.value = result.total }
  } catch (err) {
    if (request === historyRequest) fail(err, '读取登录记录失败')
  } finally {
    if (request === historyRequest) historyLoading.value = false
  }
}

function changeHistoryChannel(channel: string | number): void {
  historyChannel.value = channel === 'web' ? 'web' : 'ssh'
  historyPage.value = 1
  void loadHistory()
}

function endLabel(reason: string): string {
  if (reason === 'closed') return '已断开'
  if (reason === 'replaced') return '已被接管'
  if (reason === 'lost') return '连接失联或已失效（时间为最后活动）'
  return '进行中或状态未知'
}

async function addKey(): Promise<void> {
  const text = newKey.value.trim()
  if (!text) {
    ElMessage.warning('先粘贴公钥内容')
    return
  }
  addingKey.value = true
  try {
    const key = await api.addKey(text, newKeyName.value.trim())
    newKey.value = ''
    newKeyName.value = ''
    keys.value = [...keys.value, key]
    ElMessage.success('公钥已添加')
  } catch (err) {
    if (err instanceof ApiError && err.code === 'account_conflict') {
      ElMessage.error('这把公钥已经绑到别的账号上了')
    } else {
      fail(err, '添加失败')
    }
  } finally {
    addingKey.value = false
  }
}

async function renameKey(key: Key): Promise<void> {
  let name: string
  try {
    const result = await ElMessageBox.prompt('给这把密钥起个名字，方便区分设备', '重命名', {
      inputValue: key.name,
      inputPlaceholder: '例如：家里的台式机',
      confirmButtonText: '保存',
      cancelButtonText: '取消',
    })
    name = result.value ?? ''
  } catch {
    return
  }
  try {
    const updated = await api.renameKey(key.fingerprint, name.trim())
    keys.value = keys.value.map((k) => (k.fingerprint === updated.fingerprint ? updated : k))
    ElMessage.success('已重命名')
  } catch (err) {
    fail(err, '重命名失败')
  }
}

async function removeKey(key: Key): Promise<void> {
  try {
    await ElMessageBox.confirm(
      '删掉之后就没办法用这把密钥登录了，关联的登录会话也会一起失效。确认删除？',
      '删除公钥',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  try {
    await api.deleteKey(key.fingerprint)
    keys.value = keys.value.filter((k) => k.fingerprint !== key.fingerprint)
    ElMessage.success('已删除')
    await loadSessions()
  } catch (err) {
    if (err instanceof ApiError && err.code === 'last_credential') {
      ElMessage.error('这是最后一种登录凭证，删了就进不来了')
    } else {
      fail(err, '删除失败')
    }
  }
}

async function revokeOne(session: SessionInfo): Promise<void> {
  if (session.current) {
    ElMessage.warning('这就是当前设备 / 浏览器，要退出请用右上角的“退出”')
    return
  }
  try {
    await api.revokeSession(session.id)
    sessions.value = sessions.value.filter((s) => s.id !== session.id)
    ElMessage.success('已退出该设备')
  } catch (err) {
    fail(err, '退出失败')
  }
}

async function revokeAllOthers(): Promise<void> {
  try {
    const { revoked_count } = await api.revokeOtherSessions()
    ElMessage.success(revoked_count > 0 ? `已退出其他 ${revoked_count} 台设备 / 浏览器` : '没有其他登录')
    await loadSessions()
  } catch (err) {
    fail(err, '操作失败')
  }
}

async function savePassword(): Promise<void> {
  if (newPassword.value.length < 10) {
    ElMessage.warning('密码至少 10 个字节')
    return
  }
  savingPassword.value = true
  try {
    await api.changePassword(newPassword.value, revokeOthers.value)
    newPassword.value = ''
    ElMessage.success(revokeOthers.value ? '密码已更新，其他设备已退出' : '密码已更新')
    await loadSessions()
  } catch (err) {
    fail(err, '修改失败')
  } finally {
    savingPassword.value = false
  }
}

async function setPasswordLoginEnabled(enabled: boolean): Promise<void> {
  if (!enabled && keys.value.length === 0) {
    ElMessage.warning('关闭前请先绑定 SSH 公钥')
    return
  }
  if (!enabled) {
    try {
      await ElMessageBox.confirm(
        '关闭后，这个账号只能通过已绑定的 SSH 公钥登录。密码不会删除。',
        '关闭密码登录',
        { type: 'warning', confirmButtonText: '关闭密码登录', cancelButtonText: '取消' },
      )
    } catch {
      return
    }
  }
  savingPasswordLogin.value = true
  try {
    await api.setPasswordLoginEnabled(enabled)
    await auth.refresh()
    ElMessage.success(enabled ? '密码登录已开启' : '密码登录已关闭')
  } catch (err) {
    if (err instanceof ApiError && err.code === 'last_credential') {
      ElMessage.error('关闭密码登录前请先绑定 SSH 公钥')
    } else {
      fail(err, '设置失败')
    }
  } finally {
    savingPasswordLogin.value = false
  }
}

async function doLogout(): Promise<void> {
  try {
    await auth.logout()
    await router.push('/login')
  } catch {
    ElMessage.error('退出失败，请检查网络后重试')
  }
}

function shortFingerprint(fp: string): string {
  return fp.length > 28 ? `${fp.slice(0, 28)}…` : fp
}

function fmt(iso: string | null): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString()
}

function activityLabel(iso: string): string {
  const seen = Date.parse(iso)
  return Number.isFinite(seen) && clock.value - seen < 5 * 60 * 1000 ? '近期活跃' : '暂不活跃'
}

function methodLabel(m: string): string {
  if (m === 'password') return '密码'
  if (m === 'ssh_key') return 'SSH 密钥'
  if (m === 'ssh_challenge') return 'SSH 确认'
  return m
}

onMounted(async () => {
  clockTimer = window.setInterval(() => { clock.value = Date.now() }, 60_000)
  try {
    await auth.refresh()
  } catch {
    await handleExpired()
    return
  }
  await Promise.all([loadKeys(), loadSessions(), loadPoints(), loadHistory()])
  if (disposed) return
  devicesTimer = window.setInterval(() => {
    void Promise.all([api.listSessions(), api.onlineTerminal()]).then(([web, ssh]) => {
      sessions.value = web.items
      terminal.value = ssh.terminal
    }).catch(() => { /* 下次刷新再试 */ })
  }, 15_000)
})
onUnmounted(() => {
  disposed = true
  if (clockTimer !== undefined) window.clearInterval(clockTimer)
  if (devicesTimer !== undefined) window.clearInterval(devicesTimer)
})
</script>

<template>
  <div class="auth-page">
    <div class="auth-stack" style="max-width: 780px">
      <div style="display: flex; align-items: baseline; gap: 12px; flex-wrap: wrap">
        <h1 class="page-title" style="margin: 0">我的账号</h1>
        <span class="hint">
          @{{ auth.state.account?.username }}
        </span>
        <el-tag v-if="points" type="warning" effect="dark" round>
          {{ points.balance_pt }} PT
        </el-tag>
        <el-tag v-else type="info" effect="plain" round>— PT</el-tag>
        <el-button text size="small" @click="doLogout">退出</el-button>
      </div>
      <p class="page-sub">
        账号 ID <span class="fp">{{ auth.state.account?.id }}</span>
      </p>

      <el-card class="card" shadow="never">
        <template #header>
          <strong>密码登录</strong>
        </template>
        <p class="hint">
          当前：{{ auth.state.account?.password_login_enabled ? '已开启' : '已关闭' }}。
          关闭后只能通过已绑定的 SSH 公钥登录，密码凭证会保留。
        </p>
        <el-button
          :type="auth.state.account?.password_login_enabled ? 'danger' : 'primary'"
          :loading="savingPasswordLogin"
          :disabled="!auth.state.account?.password_login_enabled && !keys.length"
          @click="setPasswordLoginEnabled(!auth.state.account?.password_login_enabled)"
        >
          {{ auth.state.account?.password_login_enabled ? '关闭密码登录' : '开启密码登录' }}
        </el-button>
        <p v-if="auth.state.account?.password_login_enabled && !keys.length" class="hint">
          请先添加 SSH 公钥，才能关闭密码登录。
        </p>
      </el-card>

      <el-card class="card" shadow="never">
        <template #header>
          <div style="display: flex; justify-content: space-between; align-items: center">
            <strong>SSH 公钥</strong>
            <el-button text size="small" :loading="loading" @click="loadKeys">刷新</el-button>
          </div>
        </template>

        <el-table :data="keys" size="small" empty-text="还没有绑定公钥">
          <el-table-column label="名称" width="140">
            <template #default="{ row }">
              <span>{{ row.name || '（未命名）' }}</span>
            </template>
          </el-table-column>
          <el-table-column label="指纹" min-width="200">
            <template #default="{ row }">
              <span class="fp">{{ shortFingerprint(row.fingerprint) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="最后使用" width="170">
            <template #default="{ row }">
              <span class="hint">{{ fmt(row.last_used_at) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="" width="130" align="right">
            <template #default="{ row }">
              <el-button text size="small" @click="renameKey(row)">改名</el-button>
              <el-button text type="danger" size="small" @click="removeKey(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>

        <div style="margin-top: 16px">
          <el-input
            v-model="newKey"
            type="textarea"
            :rows="2"
            placeholder="粘贴 .ssh/id_ed25519.pub 的完整内容"
          />
          <div style="display: flex; gap: 8px; margin-top: 8px; align-items: center">
            <el-input
              v-model="newKeyName"
              placeholder="名称（可选），例如：家里的台式机"
              style="max-width: 260px"
            />
            <el-button type="primary" :loading="addingKey" @click="addKey">添加公钥</el-button>
          </div>
        </div>
      </el-card>

      <el-card class="card" shadow="never">
        <template #header>
          <div style="display: flex; justify-content: space-between; align-items: center">
            <strong>Web 登录设备</strong>
            <div>
              <el-button text size="small" :loading="sessionsLoading" @click="loadSessions">
                刷新
              </el-button>
              <el-button text size="small" type="danger" @click="revokeAllOthers">
                退出其他所有登录
              </el-button>
            </div>
          </div>
        </template>

        <el-table :data="sessions" size="small" empty-text="没有有效的 Web 登录">
          <el-table-column label="方式" width="100">
            <template #default="{ row }">
              <span>{{ methodLabel(row.auth_method) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="来源" min-width="150">
            <template #default="{ row }">
              <span class="fp">{{ row.source_ip || '—' }}</span>
              <el-tag v-if="row.current" size="small" type="success" style="margin-left: 6px">
                当前
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="设备 / 浏览器" min-width="160">
            <template #default="{ row }">
              <span class="hint">{{ row.user_agent || '—' }}</span>
            </template>
          </el-table-column>
          <el-table-column label="最后活跃" width="170">
            <template #default="{ row }">
              <span class="hint">{{ fmt(row.last_seen_at) }}</span>
              <span class="hint"> · {{ activityLabel(row.last_seen_at) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="" width="80" align="right">
            <template #default="{ row }">
              <el-button
                v-if="!row.current"
                text
                type="danger"
                size="small"
                @click="revokeOne(row)"
              >
                退出
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-card>

      <el-card class="card" shadow="never">
        <template #header><strong>当前 SSH 终端</strong></template>
        <p v-if="terminal" class="hint">
          来源：{{ terminal.source_ip || '—' }} · {{ methodLabel(terminal.auth_method) }} · 连接时间：{{ fmt(terminal.started_at) }} · 最后心跳：{{ fmt(terminal.last_seen_at) }}
          <span v-if="terminal.credential_fingerprint"> · 密钥：{{ terminal.key_name || shortFingerprint(terminal.credential_fingerprint) }}</span>
        </p>
        <p v-else class="hint">当前没有在线终端</p>
      </el-card>

      <el-card class="card" shadow="never">
        <template #header>
          <div style="display: flex; justify-content: space-between; align-items: center">
            <strong>最近成功登录</strong>
            <el-button text size="small" :loading="historyLoading" @click="loadHistory">刷新</el-button>
          </div>
        </template>
        <p class="hint">只显示启用记录后的成功登录，保留 30 天、最多 1000 条。来源和密钥不能确定物理设备。</p>
        <el-radio-group :model-value="historyChannel" @change="changeHistoryChannel">
          <el-radio-button value="ssh">SSH</el-radio-button>
          <el-radio-button value="web">Web</el-radio-button>
        </el-radio-group>
        <el-table v-loading="historyLoading" :data="history" size="small" empty-text="暂无记录" style="width: 100%; margin-top: 12px">
          <el-table-column type="expand">
            <template #default="{ row }">
              <div style="padding: 8px 20px">
                <p v-for="event in row.events" :key="event.id" class="hint">
                  {{ fmt(event.started_at) }} · {{ methodLabel(event.auth_method) }} · {{ event.source_ip || '来源未知' }}
                  <template v-if="historyChannel === 'ssh'"> · {{ endLabel(event.end_reason) }}<template v-if="event.ended_at">：{{ fmt(event.ended_at) }}</template></template>
                  <template v-if="event.key_name"> · {{ event.key_name }}</template>
                  <span v-if="event.credential_fingerprint" style="overflow-wrap: anywhere"> · {{ event.credential_fingerprint }}</span>
                  <template v-if="historyChannel === 'web' && event.user_agent"> · {{ event.user_agent }}</template>
                </p>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="最近登录" min-width="160"><template #default="{ row }">{{ fmt(row.latest_at) }}</template></el-table-column>
          <el-table-column label="来源 / 设备" min-width="200">
            <template #default="{ row }"><span class="hint">{{ row.source_ip || '来源未知' }}<template v-if="historyChannel === 'ssh' && row.credential_fingerprint"> · {{ row.key_name || shortFingerprint(row.credential_fingerprint) }}</template><template v-if="historyChannel === 'web' && row.user_agent"> · {{ row.user_agent }}</template></span></template>
          </el-table-column>
          <el-table-column prop="count" label="次数" width="70" />
        </el-table>
        <el-pagination v-if="historyTotal > 10" v-model:current-page="historyPage" :page-size="10" :total="historyTotal" layout="prev, pager, next" @current-change="loadHistory" />
      </el-card>

      <el-card class="card" shadow="never">
        <template #header>
          <strong>设置或修改密码</strong>
        </template>
        <el-form label-position="top">
          <el-form-item label="新密码（10–72 字节）">
            <el-input
              v-model="newPassword"
              type="password"
              show-password
              autocomplete="new-password"
            />
          </el-form-item>
          <el-checkbox v-model="revokeOthers" style="margin-bottom: 8px">
            同时退出其他设备上的登录
          </el-checkbox>
          <p class="hint" style="margin: 0 0 8px">
            公钥和密码至少保留一种，否则会把自己锁在门外。
          </p>
          <el-button type="primary" :loading="savingPassword" @click="savePassword">
            保存密码
          </el-button>
        </el-form>
      </el-card>
    </div>
  </div>
</template>
