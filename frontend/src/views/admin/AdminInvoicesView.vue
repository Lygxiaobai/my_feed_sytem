<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { ApiError } from '../../api/client'
import * as adminApi from '../../api/admin'
import type { AdminInvoice, AdminInvoiceBoard } from '../../api/admin'
import { payMethodLabel } from '../../api/invoice'
import { useToastStore } from '../../stores/toast'

const route = useRoute()
const router = useRouter()
const toast = useToastStore()

const invoiceNo = ref('')
const outTradeNo = ref('')
const accountId = ref('')
const loading = ref(false)
const board = ref<AdminInvoiceBoard | null>(null)
const viewing = ref<AdminInvoice | null>(null)

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
    const offset = reset ? 0 : board.value?.invoices.length || 0
    const next = await adminApi.listAdminInvoices({
      invoice_no: invoiceNo.value.trim() || undefined,
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
        invoices: [...board.value.invoices, ...next.invoices],
      }
    }
    await router.replace({
      path: '/admin/invoices',
      query: {
        ...(invoiceNo.value.trim() ? { invoice_no: invoiceNo.value.trim() } : {}),
        ...(outTradeNo.value.trim() ? { out_trade_no: outTradeNo.value.trim() } : {}),
        ...(accountId.value.trim() ? { account_id: accountId.value.trim() } : {}),
      },
    })
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '加载发票失败')
  } finally {
    loading.value = false
  }
}

async function openInvoice(item: AdminInvoice) {
  try {
    const res = await adminApi.getAdminInvoice(item.invoice_no)
    viewing.value = res.invoice
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '打开发票失败')
  }
}

onMounted(() => {
  invoiceNo.value = String(route.query.invoice_no || '')
  outTradeNo.value = String(route.query.out_trade_no || '')
  accountId.value = String(route.query.account_id || '')
  void load()
})
</script>

<template>
  <div>
    <p class="title">发票看板</p>
    <p class="subtle">只读查看已开具的个人消费凭证。这里不能开票、驳回，也不会入账。</p>

    <div v-if="board" class="cards">
      <div class="card metric">
        <div class="metric-num">{{ board.summary.issued_count }}</div>
        <div class="metric-label">已开具</div>
      </div>
      <div class="card metric">
        <div class="metric-num">{{ board.summary.yuan_total }}</div>
        <div class="metric-label">开票金额（元）</div>
      </div>
      <div class="card metric">
        <div class="metric-num">{{ board.summary.coins_total }}</div>
        <div class="metric-label">对应积分</div>
      </div>
    </div>

    <div class="row search">
      <input v-model.trim="invoiceNo" placeholder="发票号" @keydown.enter="load()" />
      <input v-model.trim="outTradeNo" placeholder="商户单号" @keydown.enter="load()" />
      <input v-model.trim="accountId" inputmode="numeric" placeholder="账号 ID" @keydown.enter="load()" />
      <button class="primary" type="button" :disabled="loading" @click="load()">查询</button>
    </div>

    <div v-if="loading && !board" class="subtle">加载中…</div>
    <div v-else-if="board && board.invoices.length === 0" class="card">
      <p class="subtle" style="margin: 0">没有匹配的发票。</p>
    </div>
    <div v-else-if="board" class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>发票号</th>
            <th>账号</th>
            <th>金额</th>
            <th>抬头</th>
            <th>开具时间</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in board.invoices" :key="item.invoice_no">
            <td>
              <button class="link" type="button" @click="openInvoice(item)">{{ item.invoice_no }}</button>
              <div class="subtle">{{ item.out_trade_no }}</div>
            </td>
            <td>
              <button class="link" type="button" @click="router.push({ path: '/admin/users', query: { id: String(item.account_id) } })">
                {{ item.username || `#${item.account_id}` }}
              </button>
            </td>
            <td>{{ item.yuan }} 元 · {{ item.coins + item.bonus }} 积分</td>
            <td>{{ item.title }}<div class="subtle">{{ item.email }}</div></td>
            <td>{{ formatTime(item.issued_at || item.created_at) }}</td>
          </tr>
        </tbody>
      </table>
      <button v-if="board.has_more" type="button" :disabled="loading" @click="load(false)">加载更多</button>
    </div>

    <Teleport to="body">
      <div v-if="viewing" class="mask" @click.self="viewing = null">
        <div class="dialog" role="dialog" aria-modal="true">
          <p class="title" style="margin: 0">消费凭证</p>
          <p class="subtle">{{ viewing.invoice_no }} · {{ viewing.username || `#${viewing.account_id}` }}</p>
          <p>抬头 {{ viewing.title }}</p>
          <p>金额 {{ viewing.yuan }} 元，到账 {{ viewing.coins + viewing.bonus }} 积分</p>
          <p>支付 {{ payMethodLabel(viewing.pay_method) }} · {{ formatTime(viewing.paid_at) }}</p>
          <p>邮箱 {{ viewing.email }}</p>
          <p class="subtle">订单 {{ viewing.out_trade_no }}</p>
          <p class="subtle">本凭证由站点开具，非正式税务发票。入账只认充值通知，不认本页。</p>
          <button type="button" @click="viewing = null">关闭</button>
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

@media (max-width: 720px) {
  .cards {
    grid-template-columns: 1fr;
  }
}
</style>
