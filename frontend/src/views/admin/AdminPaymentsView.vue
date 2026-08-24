<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { ApiError } from '../../api/client'
import * as adminApi from '../../api/admin'
import type { AdminPaymentBoard } from '../../api/admin'
import { ORDER_STATUS_LABEL } from '../../api/admin'
import { payMethodLabel } from '../../api/invoice'
import { useToastStore } from '../../stores/toast'

const route = useRoute()
const router = useRouter()
const toast = useToastStore()

const status = ref('')
const outTradeNo = ref('')
const accountId = ref('')
const loading = ref(false)
const board = ref<AdminPaymentBoard | null>(null)

function formatTime(value?: string) {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN')
}

function parsedAccountId() {
  const text = accountId.value.trim()
  if (!text) return undefined
  const id = Number(text)
  return Number.isInteger(id) && id > 0 ? id : undefined
}

async function load(reset = true) {
  if (accountId.value.trim() && parsedAccountId() === undefined) {
    toast.error('请输入有效的账号 ID')
    return
  }
  loading.value = true
  try {
    const offset = reset ? 0 : board.value?.orders.length || 0
    const next = await adminApi.listAdminPayments({
      status: status.value || undefined,
      out_trade_no: outTradeNo.value.trim() || undefined,
      account_id: parsedAccountId(),
      limit: 20,
      offset,
    })
    if (reset || !board.value) {
      board.value = next
    } else {
      board.value = {
        ...next,
        orders: [...board.value.orders, ...next.orders],
      }
    }
    await router.replace({
      path: '/admin/payments',
      query: {
        ...(status.value ? { status: status.value } : {}),
        ...(outTradeNo.value.trim() ? { out_trade_no: outTradeNo.value.trim() } : {}),
        ...(accountId.value.trim() ? { account_id: accountId.value.trim() } : {}),
      },
    })
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '加载支付单失败')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  status.value = String(route.query.status || '')
  outTradeNo.value = String(route.query.out_trade_no || '')
  accountId.value = String(route.query.account_id || '')
  void load()
})
</script>

<template>
  <div>
    <p class="title">支付看板</p>
    <p class="subtle">只读查看充值单。这里不会入账、关单，也不打开 Stripe。</p>

    <div v-if="board" class="cards">
      <div class="card metric">
        <div class="metric-num">{{ board.summary.paid_yuan }}</div>
        <div class="metric-label">已支付 {{ board.summary.paid_count }} 笔（元）</div>
      </div>
      <div class="card metric">
        <div class="metric-num">{{ board.summary.pending_count }}</div>
        <div class="metric-label">待支付</div>
      </div>
      <div class="card metric">
        <div class="metric-num">{{ board.summary.closed_count }}</div>
        <div class="metric-label">已关闭 · 平台抽成 {{ board.summary.platform_cut_coins }}</div>
      </div>
    </div>

    <div class="row search">
      <select v-model="status">
        <option value="">全部状态</option>
        <option value="paid">已支付</option>
        <option value="pending">待支付</option>
        <option value="closed">已关闭</option>
      </select>
      <input v-model.trim="outTradeNo" placeholder="商户单号" @keydown.enter="load()" />
      <input v-model.trim="accountId" inputmode="numeric" placeholder="账号 ID" @keydown.enter="load()" />
      <button class="primary" type="button" :disabled="loading" @click="load()">查询</button>
    </div>

    <div v-if="loading && !board" class="subtle">加载中…</div>
    <div v-else-if="board && board.orders.length === 0" class="card">
      <p class="subtle" style="margin: 0">没有匹配的充值单。</p>
    </div>
    <div v-else-if="board" class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>商户单号</th>
            <th>账号</th>
            <th>金额</th>
            <th>状态</th>
            <th>时间</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in board.orders" :key="item.out_trade_no">
            <td>
              <div class="mono">{{ item.out_trade_no }}</div>
              <div class="subtle">{{ payMethodLabel(item.pay_method) }}</div>
            </td>
            <td>
              <button class="link" type="button" @click="router.push({ path: '/admin/users', query: { id: String(item.account_id) } })">
                {{ item.username || `#${item.account_id}` }}
              </button>
            </td>
            <td>{{ item.yuan }} 元 · {{ item.coins + item.bonus }} 积分</td>
            <td>
              <span class="pill" :class="{ ok: item.status === 'paid', bad: item.status === 'closed' }">
                {{ ORDER_STATUS_LABEL[item.status] || item.status }}
              </span>
            </td>
            <td>
              <div>{{ formatTime(item.paid_at || item.created_at) }}</div>
              <div v-if="item.status === 'pending'" class="subtle">过期 {{ formatTime(item.expire_at) }}</div>
            </td>
          </tr>
        </tbody>
      </table>
      <button v-if="board.has_more" type="button" :disabled="loading" @click="load(false)">加载更多</button>
    </div>
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

.search input,
.search select {
  max-width: 200px;
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

@media (max-width: 720px) {
  .cards {
    grid-template-columns: 1fr;
  }
}
</style>
