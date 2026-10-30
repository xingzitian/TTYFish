<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ApiError, api } from '../api/client'

const router = useRouter()

const form = ref({
  username: '',
  publicKey: '',
  password: '',
})
const submitting = ref(false)

// 和后端规则一致：小写字母开头，3~20 位，允许数字、下划线、短横线
const usernamePattern = /^[a-z][a-z0-9_-]{2,19}$/

const usernameError = computed(() => {
  if (!form.value.username) return ''
  return usernamePattern.test(form.value.username)
    ? ''
    : '3–20 位小写字母、数字、下划线或短横线，以字母开头'
})

const canSubmit = computed(() => {
  if (!usernamePattern.test(form.value.username)) return false
  // 密码和公钥至少填一个
  return form.value.password.length > 0 || form.value.publicKey.trim().length > 0
})

async function submit(): Promise<void> {
  if (!canSubmit.value) {
    ElMessage.warning('密码和公钥至少填一项')
    return
  }
  submitting.value = true
  try {
    await api.register({
      username: form.value.username,
      password: form.value.password || undefined,
      public_key: form.value.publicKey.trim() || undefined,
    })
    ElMessage.success('注册成功，去登录吧')
    await router.push('/login')
  } catch (err) {
    if (err instanceof ApiError) {
      if (err.code === 'account_conflict') {
        ElMessage.error('这个用户名或公钥已经被占用了')
      } else if (err.status === 429) {
        ElMessage.error('操作太频繁，等一分钟再试')
      } else {
        ElMessage.error(err.message)
      }
    } else {
      ElMessage.error('注册失败')
    }
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="auth-page">
    <div class="auth-stack">
      <h1 class="page-title">注册账号</h1>
      <p class="page-sub">用户名注册后固定，所有登录方式都用它。</p>

      <el-card class="card" shadow="never">
        <el-form label-position="top" @submit.prevent="submit">
          <el-form-item label="固定用户名" :error="usernameError">
            <el-input
              v-model="form.username"
              placeholder="例如 alice"
              autocomplete="username"
              maxlength="20"
            />
          </el-form-item>

          <el-form-item label="SSH 公钥（可选）">
            <el-input
              v-model="form.publicKey"
              type="textarea"
              :rows="3"
              placeholder="ssh-ed25519 AAAA…"
            />
            <p class="hint" style="margin: 6px 0 0">
              粘贴电脑上 <code>.ssh/id_ed25519.pub</code> 的完整内容。没有密钥可以运行
              <code>ssh-keygen -t ed25519</code> 生成。<b>只提交 .pub 公钥，私钥留在自己电脑上。</b>
            </p>
          </el-form-item>

          <el-form-item label="密码（可选，10–72 字节）">
            <el-input
              v-model="form.password"
              type="password"
              show-password
              autocomplete="new-password"
            />
          </el-form-item>

          <p class="hint" style="margin-top: 0">公钥和密码至少填写一项。</p>

          <el-button type="primary" :loading="submitting" @click="submit">注册</el-button>
        </el-form>
      </el-card>

      <p class="hint" style="text-align: center">
        已经有账号？<router-link to="/login">去登录</router-link>
      </p>
    </div>
  </div>
</template>
