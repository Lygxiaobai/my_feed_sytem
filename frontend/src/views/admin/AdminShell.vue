<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'

import { ApiError } from '../../api/client'
import * as adminApi from '../../api/admin'
import Toaster from '../../components/Toaster.vue'
import { useAuthStore } from '../../stores/auth'
import { useToastStore } from '../../stores/toast'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const toast = useToastStore()

const ready = ref(false)
const pending = ref(0)

const nav: { to: string; label: string; exact?: boolean }[] = [
  { to: '/admin', label: '概览', exact: true },
  { to: '/admin/reports', label: '举报队列' },
  { to: '/admin/videos', label: '视频看板' },
  { to: '/admin/users', label: '用户看板' },
  { to: '/admin/invoices', label: '发票' },
  { to: '/admin/payments', label: '支付' },
  { to: '/admin/balances', label: '余额' },
  { to: '/admin/ops', label: '运维' },
]

const operator = computed(() => auth.claims?.username || `账号 #${auth.claims?.account_id ?? 0}`)

function navActive(to: string, exact?: boolean) {
  if (exact) return route.path === to
  return route.path === to || route.path.startsWith(`${to}/`)
}

async function refreshPending() {
  try {
    const overview = await adminApi.adminOverview()
    pending.value = overview.pending_reports
  } catch {
    pending.value = 0
  }
}

onMounted(async () => {
  if (!auth.isLoggedIn) {
    await router.replace('/account')
    return
  }
  try {
    const access = await adminApi.adminAccess()
    if (!access.allowed) {
      toast.error('没有管理后台权限')
      await router.replace('/account')
      return
    }
    ready.value = true
    void refreshPending()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '无法打开管理后台')
    await router.replace('/account')
  }
})
</script>

<template>
  <div v-if="ready" class="admin">
    <aside class="rail">
      <div class="brand">
        <strong>管理后台</strong>
        <span>审核员工作台</span>
      </div>
      <nav>
        <RouterLink
          v-for="item in nav"
          :key="item.to"
          :to="item.to"
          class="nav-item"
          :class="{ active: navActive(item.to, item.exact) }"
        >
          {{ item.label }}
          <em v-if="item.to === '/admin/reports' && pending > 0">{{ pending }}</em>
        </RouterLink>
      </nav>
      <div class="rail-foot">
        <p class="who">{{ operator }}</p>
        <RouterLink to="/account">返回站点</RouterLink>
      </div>
    </aside>
    <main class="stage">
      <RouterView v-slot="{ Component }">
        <component :is="Component" @changed="refreshPending" />
      </RouterView>
    </main>
    <Toaster />
  </div>
</template>

<style scoped>
.admin {
  min-height: var(--app-height);
  display: grid;
  grid-template-columns: 220px minmax(0, 1fr);
  background: var(--bg);
  color: var(--text);
}

.rail {
  display: flex;
  flex-direction: column;
  padding: 20px 16px;
  border-right: 1px solid var(--border);
  background: var(--chrome);
}

.brand {
  display: grid;
  gap: 2px;
  margin-bottom: 20px;
}

.brand strong {
  font-size: 16px;
}

.brand span,
.who {
  font-size: 12px;
  color: var(--muted);
}

nav {
  display: grid;
  gap: 6px;
}

.nav-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px;
  border-radius: 12px;
  text-decoration: none;
  color: var(--text);
}

.nav-item:hover {
  background: var(--fill);
  text-decoration: none;
}

.nav-item.active {
  background: var(--nav-active);
  font-weight: 700;
}

.nav-item em {
  font-style: normal;
  min-width: 20px;
  padding: 0 6px;
  border-radius: 999px;
  background: rgba(254, 44, 85, 0.18);
  color: var(--primary);
  font-size: 12px;
  text-align: center;
}

.rail-foot {
  margin-top: auto;
  display: grid;
  gap: 8px;
}

.stage {
  min-width: 0;
  padding: 24px;
}

@media (max-width: 900px) {
  .admin {
    grid-template-columns: 1fr;
  }

  .rail {
    border-right: 0;
    border-bottom: 1px solid var(--border);
    padding: 12px;
  }

  nav {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .stage {
    padding: 14px 12px 24px;
  }
}
</style>
