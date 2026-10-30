<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ApiError, api } from '../api/client'
import type { Key } from '../api/types'
import { useAuth } from '../stores/auth'

const router = useRouter()
const auth = useAuth()

const keys = ref<Key[]>([])
const loading = ref(false)
const newKey = ref('')
const addingKey = ref(false)
const newPassword = ref('')
const savingPassword = ref(false)

async function loadKeys(): Promise<void> {
  loading.value = true
  try {
    const res = await api.listKeys()
    keys.value = res.items
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) {
      await handleExpired()
      return
    }
    ElMessage.error(err instanceof ApiError ? err.message : '读取公钥失败')
  } finally {
    loading.value = false
  }
}

async function handleExpired(): Promise<void> {
  ElMessage.warning('登录已过期，请重新登录')
  await auth.logout()
  await router.push('/login')
}

async function addKey(): Promise<void> {
  const text = newKey.value.trim()
  if (!text) {
    ElMessage.warning('先粘贴公钥内容')
    return
  }
  addingKey.value = true
  try {
    const key = await api.addKey(text)
    newKey.value = ''
    ElMessage.success('公钥已添加')
    keys.value = [...keys.value, key]
  } catch (err) {
    if (err instanceof ApiError && err.code === 'account_conflict') {
      ElMessage.error('这把公钥已经绑到别的账号上了')
    } else {
      ElMessage.error(err instanceof ApiError ? err.message : '添加失败')
    }
  } finally {
    addingKey.value = false
  }
}

async function removeKey(key: Key): Promise<void> {
  try {
    await ElMessageBox.confirm(
      '删掉之后就没办法用这把密钥登录了。确认删除？',
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
  } catch (err) {
    if (err instanceof ApiError && err.code === 'last_credential') {
      ElMessage.error('这是最后一种登录凭证，删了就进不来了')
    } else {
      ElMessage.error(err instanceof ApiError ? err.message : '删除失败')
    }
  }
}

async function savePassword(): Promise<void> {
  if (newPassword.value.length < 10) {
    ElMessage.warning('密码至少 10 个字节')
    return
  }
  savingPassword.value = true
  try {
    await api.changePassword(newPassword.value)
    newPassword.value = ''
    ElMessage.success('密码已更新')
  } catch (err) {
    ElMessage.error(err instanceof ApiError ? err.message : '修改失败')
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

function fmt(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString()
}

onMounted(async () => {
  try {
    await auth.refresh()
  } catch {
    await handleExpired()
    return
  }
  await loadKeys()
})
</script>

<template>
  <div class="auth-page">
    <div class="auth-stack" style="max-width: 720px">
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
          <el-table-column label="指纹" min-width="220">
            <template #default="{ row }">
              <span class="fp">{{ shortFingerprint(row.fingerprint) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="添加时间" width="180">
            <template #default="{ row }">
              <span class="hint">{{ fmt(row.created_at) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="" width="80" align="right">
            <template #default="{ row }">
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
          <el-button
            type="primary"
            style="margin-top: 8px"
            :loading="addingKey"
            @click="addKey"
          >
            添加公钥
          </el-button>
        </div>
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
          <p class="hint" style="margin-top: 0">
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
