<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { ApiError } from '../../api/client'
import * as adminApi from '../../api/admin'
import type { AdminBalanceBoard, AdminBalanceDetail } from '../../api/admin'
import { LOT_SOURCE_LABEL } from '../../api/admin'
import { useToastStore } from '../../stores/toast'

const route = useRoute()
const router = useRouter()
const toast = useToastStore()

const query = ref('')
const loading = ref(false)
const board = ref<AdminBalanceBoard | null>(null)
const detail = ref<AdminBalanceDetail | null>(null)

function formatTime(value?: string) {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN')
}

function parseQuery(raw: string) {
  const text = raw.trim()
  if (!text) return {}
  if (/^\d+$/.test(text)) return { id: Number(text) }
  return { username: text }
}

async function load(reset = true) {
  loading.value = true
  try {
    const parsed = parseQuery(query.value)
    const offset = reset ? 0 : board.value?.balances.length || 0
    const next = await adminApi.listAdminBalances({
      ...parsed,
      limit: 20,
      offset,
    })
    if (reset || !board.value) {
      board.value = next
    } else {
      board.value = {
        ...next,
        balances: [...board.value.balances, ...next.balances],
      }
    }
    await router.replace({
      path: '/admin/balances',
      query: parsed.id
        ? { id: String(parsed.id) }
        : parsed.username
          ? { username: parsed.username }
          : {},
    })
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '加载余额失败')
  } finally {
    loading.value = false
  }
}

async function openBalance(accountId: number) {
  try {
    const res = await adminApi.getAdminBalance(accountId)
    detail.value = res.balance
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '打开余额失败')
  }
}

onMounted(() => {
  query.value = String(route.query.id || route.query.username || '')
  void load()
})
</script>

<template>
  <div>
    <p class="title">用户余额看板</p>
    <p class="subtle">只读查看当前可花积分。这里不会改余额，过期批次也不会在打开时落流水。</p>

    <div v-if="board" class="cards">
      <div class="card metric">
        <div class="metric-num">{{ board.summary.available_coins }}</div>
        <div class="metric-label">可花积分合计</div>
      </div>
      <div class="card metric">
        <div class="metric-num">{{ board.summary.accounts_with_balance }}</div>
        <div class="metric-label">有余额账号</div>
      </div>
      <div class="card metric">
        <div class="metric-num">{{ board.summary.expiring_soon_coins }}</div>
        <div class="metric-label">三天内到期</div>
      </div>
    </div>

    <div class="row search">
      <input v-model.trim="query" placeholder="账号 ID / 用户名" @keydown.enter="load()" />
      <button class="primary" type="button" :disabled="loading" @click="load()">查询</button>
    </div>

    <div v-if="loading && !board" class="subtle">加载中…</div>
    <div v-else-if="board && board.balances.length === 0" class="card">
      <p class="subtle" style="margin: 0">没有匹配的余额。</p>
    </div>
    <div v-else-if="board" class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>账号</th>
            <th>可花积分</th>
            <th>即将过期</th>
            <th>最近到期</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in board.balances" :key="item.account_id">
            <td>
              <button class="link" type="button" @click="openBalance(item.account_id)">
                {{ item.username || `#${item.account_id}` }}
              </button>
              <div class="subtle">#{{ item.account_id }}</div>
            </td>
            <td>{{ item.available_coins }}</td>
            <td>{{ item.expiring_soon_coins }}</td>
            <td>{{ formatTime(item.next_expire_at) }}</td>
          </tr>
        </tbody>
      </table>
      <button v-if="board.has_more" type="button" :disabled="loading" @click="load(false)">加载更多</button>
    </div>

    <Teleport to="body">
      <div v-if="detail" class="mask" @click.self="detail = null">
        <div class="dialog" role="dialog" aria-modal="true">
          <p class="title" style="margin: 0">{{ detail.username }} · #{{ detail.account_id }}</p>
          <p>可花 {{ detail.summary.available_coins }} · 三天内到期 {{ detail.summary.expiring_soon_coins }}</p>
          <p v-if="detail.summary.next_expire_at" class="subtle">
            最近到期 {{ formatTime(detail.summary.next_expire_at) }} · {{ detail.summary.next_expire_coins }} 积分
          </p>
          <p v-if="detail.lots.length === 0" class="subtle">当前没有可花批次。</p>
          <div v-for="(lot, index) in detail.lots" :key="index" class="lot">
            <strong>{{ lot.remaining }}</strong>
            <span>{{ LOT_SOURCE_LABEL[lot.source] || lot.source }}</span>
            <span class="subtle">{{ lot.expire_at ? `到期 ${formatTime(lot.expire_at)}` : '不过期' }}</span>
          </div>
          <div class="row">
            <button type="button" @click="router.push({ path: '/admin/users', query: { id: String(detail.account_id) } })">查看用户</button>
            <button type="button" @click="detail = null">关闭</button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.cards {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin: 16px 0;
}

.metric-num {
  font-size: 24px;
  font-weight: 800;
}

.metric-label {
  margin-top: 4px;
  color: var(--muted);
  font-size: 13px;
}

.search input {
  max-width: 240px;
}

.table-wrap {
  overflow-x: auto;
}

table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

th,
td {
  padding: 10px 8px;
  text-align: left;
  border-bottom: 1px solid var(--border);
  vertical-align: top;
}

.link {
  padding: 0;
  min-height: 0;
  border: 0;
  background: transparent;
  color: inherit;
  text-decoration: underline;
}

.mask {
  position: fixed;
  inset: 0;
  z-index: 40;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgba(0, 0, 0, 0.62);
}

.dialog {
  width: min(480px, 100%);
  display: grid;
  gap: 8px;
  padding: 20px;
  border-radius: 16px;
  border: 1px solid var(--border);
  background: var(--surface);
}

.lot {
  display: grid;
  grid-template-columns: 64px 88px minmax(0, 1fr);
  gap: 8px;
  align-items: baseline;
  padding: 8px 0;
  border-top: 1px solid var(--border);
}

@media (max-width: 720px) {
  .cards {
    grid-template-columns: 1fr;
  }
}
</style>
