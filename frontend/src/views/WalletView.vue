<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { track } from '../analytics/track'
import AppShell from '../components/AppShell.vue'
import { ApiError } from '../api/client'
import * as walletApi from '../api/wallet'
import type { RechargeOrder, WalletLedger, WalletSummary } from '../api/wallet'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const toast = useToastStore()

const loading = ref(false)
const busy = ref(false)
const summary = ref<WalletSummary | null>(null)
const packages = ref<walletApi.RechargePackage[]>([])
const ledgers = ref<WalletLedger[]>([])
const customYuan = ref('')
const payingNo = ref('')
const payYuanAmount = ref(0)
const checkoutURL = ref('')
const orderHint = reactive({
  text: '',
  tone: '' as '' | 'ok' | 'bad',
})

let pollTimer: number | undefined
let pollSeq = 0
const paying = computed(() => payingNo.value !== '')
const POLL_INTERVAL_MS = 2000
const POLL_MAX_TRIES = 5
const PROCESSING_HINT = '支付处理中…'
const UNPAID_HINT = '未完成支付，可重新充值'

function formatTime(raw?: string) {
  if (!raw) return ''
  const d = new Date(raw)
  if (Number.isNaN(d.getTime())) return raw
  return d.toLocaleString()
}

async function loadWallet() {
  if (!auth.isLoggedIn) return
  loading.value = true
  try {
    const [sumRes, ledgerRes] = await Promise.all([walletApi.summary(), walletApi.listLedger()])
    summary.value = sumRes.summary
    packages.value = sumRes.packages ?? []
    ledgers.value = ledgerRes.ledgers ?? []
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : String(e))
  } finally {
    loading.value = false
  }
}

function packageCoins(pkg: walletApi.RechargePackage) {
  return pkg.yuan * 10 + pkg.bonus
}

async function startPay(yuan: number) {
  if (!auth.isLoggedIn) {
    toast.error('请先登录')
    void router.push('/account')
    return
  }
  if (busy.value || paying.value) return
  if (!Number.isInteger(yuan) || yuan < 1) {
    toast.error('请输入不少于 1 的整元金额')
    return
  }
  busy.value = true
  orderHint.text = ''
  orderHint.tone = ''
  try {
    const res = await walletApi.createRecharge(yuan)
    if (!res.checkout_url) {
      throw new Error('未拿到支付信息')
    }
    payingNo.value = res.order.out_trade_no
    payYuanAmount.value = res.order.yuan
    checkoutURL.value = res.checkout_url
    window.location.assign(res.checkout_url)
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : String(e))
  } finally {
    busy.value = false
  }
}

function stopPoll() {
  pollSeq += 1
  if (pollTimer !== undefined) {
    window.clearTimeout(pollTimer)
    pollTimer = undefined
  }
}

function clearPaying() {
  payingNo.value = ''
  payYuanAmount.value = 0
  checkoutURL.value = ''
}

function markUnpaid(text = UNPAID_HINT) {
  orderHint.text = text
  orderHint.tone = 'bad'
  clearPaying()
}

function closePay() {
  stopPoll()
  clearPaying()
  if (orderHint.text === PROCESSING_HINT) {
    markUnpaid()
  }
}

function openStripe() {
  if (!checkoutURL.value) {
    toast.error('Stripe 页面暂不可用')
    return
  }
  window.location.assign(checkoutURL.value)
}

function payCustom() {
  void startPay(Number(customYuan.value))
}

function canceledReturn(raw: unknown) {
  const v = String(raw ?? '').trim().toLowerCase()
  return v === '1' || v === 'true' || v === 'yes'
}

async function settleReturnedOrder(outTradeNo: string, canceled: boolean) {
  const seq = ++pollSeq
  if (!canceled) {
    orderHint.text = PROCESSING_HINT
    orderHint.tone = ''
  }
  let tries = 0
  const tick = async () => {
    if (seq !== pollSeq) return
    tries += 1
    try {
      const res = await walletApi.queryRecharge(outTradeNo)
      if (seq !== pollSeq) return
      const order: RechargeOrder = res.order
      if (order.status === 'paid') {
        orderHint.text = `充值成功，到账 ${order.coins + order.bonus} 积分，可开个人发票`
        orderHint.tone = 'ok'
        track('wallet_recharge', { yuan: order.yuan, coins: order.coins, bonus: order.bonus })
        clearPaying()
        await loadWallet()
        return
      }
      if (order.status === 'closed') {
        markUnpaid('订单已关闭，请重新充值')
        return
      }
    } catch (e) {
      if (seq !== pollSeq) return
      orderHint.text = e instanceof ApiError ? e.message : '查询订单失败'
      orderHint.tone = 'bad'
      clearPaying()
      return
    }
    if (seq !== pollSeq) return
    // 取消回跳或短轮询后仍 pending，按未支付收掉，避免「支付处理中」挂住。
    if (canceled || tries >= POLL_MAX_TRIES) {
      markUnpaid()
      return
    }
    pollTimer = window.setTimeout(() => {
      void tick()
    }, POLL_INTERVAL_MS)
  }
  await tick()
}

onMounted(() => {
  if (!auth.isLoggedIn) {
    void router.push('/account')
    return
  }
  const returnedNo = String(route.query.out_trade_no ?? '').trim()
  if (returnedNo) {
    void settleReturnedOrder(returnedNo, canceledReturn(route.query.canceled))
    void router.replace({ path: '/wallet' })
  }
  void loadWallet()
})

onUnmounted(() => {
  stopPoll()
})
</script>

<template>
  <AppShell>
    <div class="card">
      <p class="title" style="margin: 0">钱包</p>
      <div class="tools">
        <button class="ghost compact" type="button" @click="router.push('/invoice')">发票</button>
        <button class="ghost compact" type="button" @click="router.push('/checkin')">签到</button>
        <button class="ghost compact" type="button" @click="router.push('/lottery')">抽奖</button>
      </div>
      <div v-if="orderHint.text" class="hint" :class="orderHint.tone" style="margin-top: 12px">
        <span>{{ orderHint.text }}</span>
        <button v-if="orderHint.tone === 'ok'" class="ghost" type="button" @click="router.push('/invoice')">去开票</button>
      </div>
      <div v-if="loading && !summary" class="subtle" style="margin-top: 12px">加载中…</div>
      <template v-else-if="summary">
        <div class="balance">{{ summary.available_coins }} <span class="unit">积分</span></div>
        <p v-if="summary.expiring_soon_coins > 0" class="subtle warn">
          3 天内将过期 {{ summary.expiring_soon_coins }} 积分
          <template v-if="summary.next_expire_at">
            ，最近一笔 {{ summary.next_expire_coins }} 积分将于 {{ formatTime(summary.next_expire_at) }} 过期
          </template>
        </p>
        <p v-else class="subtle">暂无即将过期的积分</p>
      </template>
    </div>

    <div class="card">
      <p class="title">充值</p>
      <p class="subtle">按元支付，1 元 = 10 积分。档位赠送一并进入充值余额，不过期。点档位即打开 Stripe，测试卡 4242 4242 4242 4242，到账后可开个人发票。</p>
      <div class="pkg-grid">
        <button
          v-for="pkg in packages"
          :key="pkg.yuan"
          class="pkg"
          type="button"
          :disabled="busy || paying"
          @click="startPay(pkg.yuan)"
        >
          <div class="pkg-yuan">{{ pkg.yuan }} 元</div>
          <div class="pkg-coin">到账 {{ packageCoins(pkg) }} 积分</div>
          <div v-if="pkg.bonus > 0" class="pkg-bonus">含赠送 {{ pkg.bonus }}</div>
        </button>
      </div>
      <div class="row" style="margin-top: 12px">
        <input v-model.trim="customYuan" type="number" min="1" step="1" placeholder="自定义整元，无赠送" />
        <button class="primary" type="button" :disabled="busy || paying" @click="payCustom">充值</button>
      </div>
    </div>

    <div class="card">
      <p class="title">流水</p>
      <div v-if="ledgers.length === 0" class="subtle">暂无流水</div>
      <div v-for="item in ledgers" :key="item.id" class="ledger">
        <div>
          <div>{{ walletApi.ledgerLabel(item.biz_type) }}</div>
          <div class="subtle">{{ formatTime(item.created_at) }}</div>
        </div>
        <div class="ledger-amt" :class="{ plus: item.amount > 0, minus: item.amount < 0 }">
          {{ item.amount > 0 ? '+' : '' }}{{ item.amount }}
        </div>
      </div>
    </div>

    <Teleport to="body">
      <div v-if="paying" class="pay-mask" @click.self="closePay">
        <div class="pay-dialog" role="dialog" aria-modal="true" aria-labelledby="pay-title">
          <p id="pay-title" class="title" style="margin: 0">Stripe 充值 {{ payYuanAmount }} 元</p>
          <p class="subtle">请在 Stripe 页面用测试卡完成支付。到期后此页会确认到账。</p>
          <p class="subtle">付款后请保持此窗口，到账由服务器确认。</p>
          <button v-if="checkoutURL" class="primary" type="button" @click="openStripe">打开 Stripe</button>
          <button class="ghost" type="button" @click="closePay">关闭</button>
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

.ghost.compact {
  padding: 6px 12px;
  border-radius: 999px;
  font-size: 13px;
  min-height: 32px;
}

.tools {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}

.balance {
  margin-top: 12px;
  font-size: 36px;
  font-weight: 800;
}

.unit {
  font-size: 16px;
  font-weight: 500;
  color: rgba(var(--fg), 0.64);
}

.warn {
  color: #fbbf24;
}

.hint {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 10px 12px;
  border-radius: 12px;
  background: rgba(var(--fg), 0.06);
}

.hint.ok {
  color: #86efac;
}

.hint.bad {
  color: #fda4af;
}

.pkg-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  margin-top: 12px;
}

.pkg {
  text-align: left;
  padding: 14px;
}

.pkg-yuan {
  font-weight: 700;
}

.pkg-coin,
.pkg-bonus {
  font-size: 13px;
  color: rgba(var(--fg), 0.64);
}

.pkg-bonus {
  color: #fbbf24;
}

.pay-mask {
  position: fixed;
  inset: 0;
  z-index: 40;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgba(0, 0, 0, 0.62);
}

.pay-dialog {
  width: min(360px, 100%);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 20px;
  border-radius: 16px;
  border: 1px solid rgba(var(--fg), 0.14);
  background: var(--surface);
}

.pay-dialog .primary,
.pay-dialog .ghost {
  width: 100%;
}

.ledger {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 0;
  border-top: 1px solid rgba(var(--fg), 0.08);
}

.ledger-amt.plus {
  color: #86efac;
}

.ledger-amt.minus {
  color: #fda4af;
}
</style>
