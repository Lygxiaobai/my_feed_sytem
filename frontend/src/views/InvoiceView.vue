<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'

import AppShell from '../components/AppShell.vue'
import { ApiError } from '../api/client'
import * as invoiceApi from '../api/invoice'
import type { EligibleOrder, Invoice, InvoiceProfile } from '../api/invoice'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'

const router = useRouter()
const auth = useAuthStore()
const toast = useToastStore()

const loading = ref(false)
const saving = ref(false)
const applying = ref('')
const profile = reactive<InvoiceProfile>({
  title: '',
  email: '',
  bank_name: '',
  bank_account: '',
  address: '',
  phone: '',
})
const eligible = ref<EligibleOrder[]>([])
const invoices = ref<Invoice[]>([])
const applyingOrder = ref<EligibleOrder | null>(null)
const viewing = ref<Invoice | null>(null)

function formatTime(raw?: string) {
  if (!raw) return ''
  const d = new Date(raw)
  if (Number.isNaN(d.getTime())) return raw
  return d.toLocaleString('zh-CN')
}

function fillProfile(src: InvoiceProfile) {
  profile.title = src.title || ''
  profile.email = src.email || ''
  profile.bank_name = src.bank_name || ''
  profile.bank_account = src.bank_account || ''
  profile.address = src.address || ''
  profile.phone = src.phone || ''
}

function headerPayload() {
  return {
    title: profile.title.trim(),
    email: profile.email.trim(),
    bank_name: profile.bank_name?.trim() || '',
    bank_account: profile.bank_account?.trim() || '',
    address: profile.address?.trim() || '',
    phone: profile.phone?.trim() || '',
  }
}

async function loadAll() {
  if (!auth.isLoggedIn) return
  loading.value = true
  try {
    const [profileRes, eligibleRes, listRes] = await Promise.all([
      invoiceApi.invoiceProfile(),
      invoiceApi.listEligibleOrders(),
      invoiceApi.listMyInvoices(),
    ])
    fillProfile(profileRes.profile)
    eligible.value = eligibleRes.orders ?? []
    invoices.value = listRes.invoices ?? []
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : String(e))
  } finally {
    loading.value = false
  }
}

async function saveProfile() {
  if (saving.value) return
  saving.value = true
  try {
    const res = await invoiceApi.saveInvoiceProfile(headerPayload())
    fillProfile(res.profile)
    toast.success('抬头已保存')
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : String(e))
  } finally {
    saving.value = false
  }
}

function startApply(order: EligibleOrder) {
  applyingOrder.value = order
}

async function confirmApply() {
  const order = applyingOrder.value
  if (!order || applying.value) return
  applying.value = order.out_trade_no
  try {
    const res = await invoiceApi.applyInvoice({
      ...headerPayload(),
      out_trade_no: order.out_trade_no,
    })
    applyingOrder.value = null
    viewing.value = res.invoice
    toast.success('已开具消费凭证')
    await loadAll()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : String(e))
  } finally {
    applying.value = ''
  }
}

function printSheet() {
  window.print()
}

onMounted(() => {
  if (!auth.isLoggedIn) {
    void router.push('/account')
    return
  }
  void loadAll()
})
</script>

<template>
  <AppShell>
    <div class="no-print">
      <div class="card">
        <div class="row" style="justify-content: space-between">
          <p class="title" style="margin: 0">发票</p>
          <button class="ghost" type="button" @click="router.push('/wallet')">返回钱包</button>
        </div>
        <p class="subtle">
          只能对已到账的充值开个人发票，提交后立即得到站内消费凭证。测试环境用钱包里的 Stripe 测试卡付成功即可，不必开 Stripe Invoices 产品。这不是税务机关监制的增值税发票，开票也不会再次入账。
        </p>
      </div>

      <div class="card">
        <p class="title">开票抬头</p>
        <label>姓名</label>
        <input v-model="profile.title" maxlength="80" placeholder="个人姓名" />
        <label>接收邮箱</label>
        <input v-model="profile.email" maxlength="120" placeholder="用于接收开票通知" />
        <details class="more">
          <summary>更多信息（选填）</summary>
          <label>开户银行</label>
          <input v-model="profile.bank_name" maxlength="64" />
          <label>银行账号</label>
          <input v-model="profile.bank_account" maxlength="64" />
          <label>地址</label>
          <input v-model="profile.address" maxlength="160" />
          <label>电话</label>
          <input v-model="profile.phone" maxlength="32" />
        </details>
        <button class="primary" type="button" :disabled="saving" @click="saveProfile">保存抬头</button>
      </div>

      <div class="card">
        <p class="title">可开票订单</p>
        <div v-if="loading && eligible.length === 0" class="subtle">加载中…</div>
        <div v-else-if="eligible.length === 0" class="subtle">
          暂无已支付且未开票的充值单。请先到钱包用 Stripe 测试卡完成一笔充值，页面确认到账后再回来。
        </div>
        <div v-for="order in eligible" :key="order.out_trade_no" class="line">
          <div>
            <div>{{ order.yuan }} 元 · {{ order.coins + order.bonus }} 积分</div>
            <div class="subtle">{{ invoiceApi.payMethodLabel(order.pay_method) }} · {{ formatTime(order.paid_at) }}</div>
          </div>
          <button class="ghost" type="button" @click="startApply(order)">申请开票</button>
        </div>
      </div>

      <div class="card">
        <p class="title">我的发票</p>
        <div v-if="invoices.length === 0" class="subtle">还没有发票</div>
        <button
          v-for="item in invoices"
          :key="item.invoice_no"
          class="line as-btn"
          type="button"
          @click="viewing = item"
        >
          <div>
            <div>{{ item.invoice_no }}</div>
            <div class="subtle">{{ item.yuan }} 元 · {{ item.title }} · {{ formatTime(item.created_at) }}</div>
          </div>
        </button>
      </div>
    </div>

    <Teleport to="body">
      <div v-if="applyingOrder" class="mask no-print" @click.self="applyingOrder = null">
        <div class="dialog" role="dialog" aria-modal="true">
          <p class="title" style="margin: 0">确认开票</p>
          <p class="subtle">{{ applyingOrder.yuan }} 元 · {{ profile.title || '未填姓名' }}</p>
          <p class="subtle">金额以到账订单为准，提交后立即开具，不能改这笔的金额。</p>
          <button class="primary" type="button" :disabled="!!applying" @click="confirmApply">开具消费凭证</button>
          <button class="ghost" type="button" @click="applyingOrder = null">取消</button>
        </div>
      </div>
    </Teleport>

    <Teleport to="body">
      <div v-if="viewing" class="mask print-root" @click.self="viewing = null">
        <div class="dialog wide" role="dialog" aria-modal="true">
          <article class="sheet">
            <p class="title" style="margin: 0">消费凭证</p>
            <p class="subtle">{{ viewing.invoice_no }}</p>
            <p>抬头 {{ viewing.title }}</p>
            <p>金额 {{ viewing.yuan }} 元，到账 {{ viewing.coins + viewing.bonus }} 积分</p>
            <p>支付 {{ invoiceApi.payMethodLabel(viewing.pay_method) }} · {{ formatTime(viewing.paid_at) }}</p>
            <p>邮箱 {{ viewing.email }}</p>
            <p class="subtle">订单 {{ viewing.out_trade_no }}</p>
            <p class="subtle">本凭证由站点开具，非正式税务发票。入账只认充值通知，不认本页。</p>
          </article>
          <button class="primary" type="button" @click="printSheet">打印</button>
          <button class="ghost" type="button" @click="viewing = null">关闭</button>
        </div>
      </div>
    </Teleport>
  </AppShell>
</template>

<style scoped>
.ghost {
  border: 1px solid rgba(var(--fg), 0.14);
  background: var(--fill);
  color: rgba(var(--fg), 0.86);
  border-radius: 12px;
  padding: 10px 12px;
  cursor: pointer;
}

.primary {
  margin-top: 12px;
}

.more {
  margin: 10px 0;
}

.more summary {
  cursor: pointer;
  color: rgba(var(--fg), 0.64);
}

.line {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  padding: 12px 0;
  border-top: 1px solid rgba(var(--fg), 0.08);
}

.line.as-btn {
  width: 100%;
  text-align: left;
  background: transparent;
  border: 0;
  border-top: 1px solid rgba(var(--fg), 0.08);
  border-radius: 0;
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
  width: min(400px, 100%);
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 20px;
  border-radius: 16px;
  border: 1px solid rgba(var(--fg), 0.14);
  background: var(--surface);
}

.dialog.wide {
  width: min(480px, 100%);
}

.sheet {
  padding: 8px 0;
}

@media print {
  .no-print,
  .print-root .ghost,
  .print-root .primary {
    display: none !important;
  }

  :global(.dy-shell) {
    display: none !important;
  }

  .print-root {
    position: static;
    background: #fff;
    padding: 0;
  }

  .dialog {
    width: 100%;
    border: 0;
    box-shadow: none;
    background: #fff;
    color: #111;
  }
}
</style>
