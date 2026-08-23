<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'

import AppShell from '../components/AppShell.vue'
import { ApiError } from '../api/client'
import * as accountApi from '../api/account'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'

const router = useRouter()
const auth = useAuthStore()
const toast = useToastStore()

const busy = ref(false)
const form = reactive({ oldPassword: '', newPassword: '' })

async function goLogin() {
  await router.push('/account')
}

async function submit() {
  if (!auth.isLoggedIn) {
    toast.error('请先登录')
    await router.push('/account')
    return
  }
  if (busy.value) return

  const oldPassword = form.oldPassword.trim()
  const newPassword = form.newPassword.trim()
  if (!oldPassword || !newPassword) {
    toast.error('请把信息填完整')
    return
  }

  busy.value = true
  try {
    await accountApi.changePassword(oldPassword, newPassword)
    toast.success('密码已修改，请重新登录')
    await router.push('/account')
  } catch (e) {
    const msg = e instanceof ApiError ? e.message : String(e)
    toast.error(msg)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AppShell>
    <div class="login-wrap">
      <div v-if="!auth.isLoggedIn" class="card login-card">
        <p class="title">修改密码</p>
        <p class="subtle">登录后再回来填写当前密码和新密码。</p>
        <button class="primary" type="button" style="margin-top: 12px" @click="goLogin">去登录</button>
      </div>

      <div v-else class="card login-card">
        <p class="title">修改密码</p>
        <p class="subtle">改密后当前登录会失效，需要重新登录。</p>
        <div class="grid" style="margin-top: 12px">
          <div>
            <label>当前密码</label>
            <input v-model.trim="form.oldPassword" type="password" autocomplete="current-password" />
          </div>
          <div>
            <label>新密码</label>
            <input
              v-model.trim="form.newPassword"
              type="password"
              autocomplete="new-password"
              @keydown.enter="submit"
            />
          </div>
          <button class="primary" type="button" :disabled="busy" @click="submit">保存</button>
        </div>
        <button class="linkish" type="button" @click="router.push('/settings')">返回设置</button>
      </div>
    </div>
  </AppShell>
</template>

<style scoped>
.login-wrap {
  display: grid;
  justify-items: center;
  align-content: start;
  padding: clamp(56px, 14vh, 160px) 16px 40px;
}

.login-card {
  width: min(420px, 100%);
}

.linkish {
  margin-top: 14px;
  border: 0;
  background: transparent;
  color: rgba(var(--fg), 0.62);
  padding: 0;
  min-height: 0;
  cursor: pointer;
  text-align: left;
}

.linkish:hover {
  background: transparent;
  color: rgba(var(--fg), 0.9);
}
</style>
