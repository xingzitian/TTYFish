<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ApiError, api } from '../api/client'
import type { Key, SessionInfo } from '../api/types'
import { useAuth } from '../stores/auth'

const router = useRouter()
const auth = useAuth()

const keys = ref<Key[]>([])
const loading = ref(false)
const newKey = ref('')
const newKeyName = ref('')
const addingKey = ref(false)

const sessions = ref<SessionInfo[]>([])
const sessionsLoading = ref(false)

const newPassword = ref('')
const revokeOthers = ref(true)
const savingPassword = ref(false)

async function handleExpired(): Promise<void> {
  ElMessage.warning('登录已过期，请重新登录')
  await auth.logout()
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

async function loadSessions(): Promise<void> {
  sessionsLoading.value = true
  try {
    sessions.value = (await api.listSessions()).items
  } catch (err) {
    fail(err, '读取登录会话失败')
  } finally {
    sessionsLoading.value = false
  }
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
    ElMessage.warning('这就是当前这条登录，要退出请用右上角的“退出”')
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
    ElMessage.success(revoked_count > 0 ? `已退出其他 ${revoked_count} 个登录` : '没有其他登录')
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

async function doLogout(): Promise<void> {
  await auth.logout()
  await router.push('/login')
}

function shortFingerprint(fp: string): string {
  return fp.length > 28 ? `${fp.slice(0, 28)}…` : fp
}

function fmt(iso: string | null): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString()
}

function methodLabel(m: string): string {
  if (m === 'password') return '密码'
  if (m === 'ssh_key') return 'SSH 密钥'
  return m
}

onMounted(async () => {
  try {
    await auth.refresh()
  } catch {
    await handleExpired()
    return
  }
  await Promise.all([loadKeys(), loadSessions()])
})
</script>

<template>
  <div class="auth-page">
    <div class="auth-stack" style="max-width: 780px">
      <div style="display: flex; align-items: baseline; gap: 12px; flex-wrap: wrap">
        <h1 class="page-title" style="margin: 0">我的账号</h1>
        <span class="hint">
          @{{ auth.state.account?.username }}
          <el-button text size="small" @click="doLogout">退出</el-button>
        </span>
      </div>
      <p class="page-sub">
        账号 ID <span class="fp">{{ auth.state.account?.id }}</span>
      </p>

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
            <strong>登录会话</strong>
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

        <el-table :data="sessions" size="small" empty-text="没有登录记录">
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
          <el-table-column label="客户端" min-width="160">
            <template #default="{ row }">
              <span class="hint">{{ row.user_agent || '—' }}</span>
            </template>
          </el-table-column>
          <el-table-column label="最后活跃" width="170">
            <template #default="{ row }">
              <span class="hint">{{ fmt(row.last_seen_at) }}</span>
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
