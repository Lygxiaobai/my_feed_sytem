<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import { track } from '../analytics/track'
import AppShell from '../components/AppShell.vue'
import { ApiError } from '../api/client'
import * as walletApi from '../api/wallet'
import { LOTTERY_PRIZES } from '../api/wallet'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'

const router = useRouter()
const auth = useAuthStore()
const toast = useToastStore()

const busy = ref(false)
const spinning = ref(false)
const claimed = ref(false)
const rotation = ref(0)
const result = ref('')
const tone = ref('' as '' | 'ok' | 'bad')
const showResultModal = ref(false)
const lastWonCoins = ref(0)

const prizes = LOTTERY_PRIZES
const sliceDeg = 360 / prizes.length
const prizeColors = ['#475569', '#059669', '#2563eb', '#7c3aed', '#d97706', '#e11d48']

const wheelStyle = computed(() => ({
  transform: `rotate(${rotation.value}deg)`,
}))

function labelAt(index: number) {
  return `rotate(${index * sliceDeg}deg) translateY(calc(var(--wheel-size) * -0.31))`
}

function landOn(prizeIndex: number) {
  const extraTurns = 6
  const currentTurns = Math.floor(rotation.value / 360)
  rotation.value = (currentTurns + extraTurns) * 360 - prizeIndex * sliceDeg
}

function finishText(coins: number) {
  if (coins === 0) return '这次没有中积分，明天再来！'
  return `恭喜抽中 ${coins} 积分，15 天后过期！`
}

function isTodayBeijing(isoString: string): boolean {
  const d = new Date(isoString)
  if (isNaN(d.getTime())) return false
  const bjTime = new Date(d.getTime() + (d.getTimezoneOffset() + 480) * 60000)
  const now = new Date()
  const nowBj = new Date(now.getTime() + (now.getTimezoneOffset() + 480) * 60000)
  return (
    bjTime.getFullYear() === nowBj.getFullYear() &&
    bjTime.getMonth() === nowBj.getMonth() &&
    bjTime.getDate() === nowBj.getDate()
  )
}

async function checkClaimStatus() {
  try {
    const res = await walletApi.listLedger(20)
    const todayLottery = (res.ledgers ?? []).find(
      (item) => item.biz_type === 'grant_lottery' && isTodayBeijing(item.created_at)
    )
    if (todayLottery) {
      claimed.value = true
      result.value = finishText(todayLottery.amount)
      tone.value = todayLottery.amount === 0 ? '' : 'ok'
    }
  } catch {
    // 账单拉取失败时允许用户尝试抽奖，后端有兜底防重保护
  }
}

onMounted(async () => {
  if (!auth.isLoggedIn) {
    await router.replace('/account')
    return
  }
  await checkClaimStatus()
})

function goBack() {
  if (window.history.length > 1) {
    router.back()
  } else {
    void router.push('/account')
  }
}

async function onDraw() {
  if (busy.value || spinning.value || claimed.value) return
  busy.value = true
  try {
    const res = await walletApi.lottery()
    track('wallet_lottery', { coins: res.coins, prize_index: res.prize_index })
    const idx = res.prize_index
    if (!Number.isInteger(idx) || idx < 0 || idx >= prizes.length) {
      throw new Error('抽奖结果无效')
    }
    lastWonCoins.value = res.coins
    spinning.value = true
    landOn(idx)
    window.setTimeout(() => {
      claimed.value = true
      spinning.value = false
      busy.value = false
      result.value = finishText(res.coins)
      tone.value = res.coins === 0 ? '' : 'ok'
      showResultModal.value = true
      toast.success(result.value)
    }, 4200)
  } catch (e) {
    spinning.value = false
    busy.value = false
    result.value = e instanceof ApiError ? e.message : String(e)
    tone.value = 'bad'
    toast.error(result.value)
    if (e instanceof ApiError && e.message.includes('已经领取')) {
      claimed.value = true
    }
  }
}
</script>

<template>
  <AppShell>
    <div class="lottery-page">
      <div class="card lottery-card">
        <div class="page-head">
          <button class="ghost compact back-btn mobile-only" type="button" @click="goBack">
            ← 返回
          </button>
          <p class="title" style="margin: 0">每日抽奖</p>
          <div style="flex: 1"></div>
          <button class="ghost compact" type="button" @click="router.push('/wallet')">钱包</button>
        </div>
        <p class="subtle lottery-desc">
          免费抽 1 次，每个自然日限 1 次。中奖积分 15 天后过期，可与签到同时领取。
        </p>

        <div class="stage">
          <div class="pointer" aria-hidden="true"></div>
          <div class="wheel" :class="{ spinning }" :style="wheelStyle">
            <div
              v-for="(item, i) in prizes"
              :key="item.label"
              class="slice-label"
              :style="{ transform: labelAt(i) }"
            >
              <span class="slice-text">{{ item.label }}</span>
            </div>
          </div>
          <button
            class="hub"
            type="button"
            :disabled="busy || spinning || claimed"
            @click="onDraw"
          >
            <span class="hub-text">{{ claimed ? '已抽过' : spinning ? '开奖中' : '立即抽奖' }}</span>
            <span v-if="!claimed && !spinning" class="hub-sub">免费 1 次</span>
            <span v-else-if="claimed" class="hub-sub">明日再来</span>
          </button>
        </div>

        <div v-if="result" class="hint" :class="tone">{{ result }}</div>

        <div class="odds-card">
          <div class="odds-title">中奖概率公示</div>
          <div class="odds-grid">
            <div
              v-for="(item, i) in prizes"
              :key="item.label"
              class="odd-item"
            >
              <span class="odd-badge" :style="{ backgroundColor: prizeColors[i] }"></span>
              <div class="odd-meta">
                <span class="odd-label">{{ item.label }}</span>
                <span class="odd-chance">{{ item.chance }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 移动端中奖庆祝弹窗 -->
    <Teleport to="body">
      <div v-if="showResultModal" class="lottery-modal-mask" @click="showResultModal = false">
        <div class="lottery-modal-content" @click.stop>
          <div class="lottery-modal-icon">
            {{ lastWonCoins > 0 ? '🎉' : '💫' }}
          </div>
          <h3 class="lottery-modal-title">
            {{ lastWonCoins > 0 ? '恭喜中奖！' : '谢谢参与' }}
          </h3>
          <p class="lottery-modal-msg">
            {{ result }}
          </p>
          <button class="primary lottery-modal-btn" type="button" @click="showResultModal = false">
            {{ lastWonCoins > 0 ? '收下奖励' : '我知道了' }}
          </button>
        </div>
      </div>
    </Teleport>
  </AppShell>
</template>

<style scoped>
.lottery-page {
  max-width: 520px;
  margin: 0 auto;
}

.lottery-card {
  overflow: hidden;
}

.ghost {
  border: 1px solid var(--border);
  background: var(--fill);
  color: var(--text);
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

.back-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.page-head {
  display: flex;
  align-items: center;
  gap: 10px;
}

.lottery-desc {
  margin-top: 10px;
  font-size: 13px;
  line-height: 1.5;
}

.stage {
  --wheel-size: min(300px, 82vw);
  position: relative;
  width: var(--wheel-size);
  height: var(--wheel-size);
  aspect-ratio: 1;
  margin: 22px auto 16px;
  user-select: none;
  -webkit-user-select: none;
}

.pointer {
  position: absolute;
  top: -10px;
  left: 50%;
  z-index: 10;
  width: 0;
  height: 0;
  border-left: 14px solid transparent;
  border-right: 14px solid transparent;
  border-top: 26px solid #fbbf24;
  transform: translateX(-50%);
  filter: drop-shadow(0 3px 6px rgba(0, 0, 0, 0.45));
}

.wheel {
  width: 100%;
  height: 100%;
  border-radius: 50%;
  border: 8px solid rgba(255, 255, 255, 0.25);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.2), inset 0 0 0 4px rgba(0, 0, 0, 0.25);
  background: conic-gradient(
    from -30deg,
    #475569 0deg 60deg,
    #059669 60deg 120deg,
    #2563eb 120deg 180deg,
    #7c3aed 180deg 240deg,
    #d97706 240deg 300deg,
    #e11d48 300deg 360deg
  );
  box-sizing: border-box;
  transition: transform 4s cubic-bezier(0.15, 0.85, 0.25, 1);
}

.wheel.spinning {
  pointer-events: none;
}

.slice-label {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 72px;
  margin-left: -36px;
  margin-top: -10px;
  text-align: center;
  font-size: 12px;
  font-weight: 800;
  color: #fff;
  text-shadow: 0 1px 3px rgba(0, 0, 0, 0.65);
  pointer-events: none;
  line-height: 1.2;
}

.slice-text {
  display: block;
  white-space: nowrap;
}

.hub {
  position: absolute;
  top: 50%;
  left: 50%;
  z-index: 5;
  width: calc(var(--wheel-size) * 0.32);
  height: calc(var(--wheel-size) * 0.32);
  min-width: 76px;
  min-height: 76px;
  max-width: 96px;
  max-height: 96px;
  transform: translate(-50%, -50%);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 6px;
  border: 4px solid rgba(255, 255, 255, 0.95);
  border-radius: 50%;
  background: linear-gradient(135deg, #fe2c55, #ff5778);
  color: #fff;
  cursor: pointer;
  box-shadow: 0 6px 18px rgba(254, 44, 85, 0.45);
  touch-action: manipulation;
  -webkit-tap-highlight-color: transparent;
  transition: transform 0.15s, box-shadow 0.15s;
}

.hub:active:not(:disabled) {
  transform: translate(-50%, -50%) scale(0.95);
}

.hub:disabled {
  cursor: default;
  background: #64748b;
  border-color: rgba(255, 255, 255, 0.5);
  box-shadow: none;
}

.hub-text {
  font-size: 13px;
  font-weight: 800;
  line-height: 1.15;
}

.hub-sub {
  font-size: 10px;
  opacity: 0.85;
  margin-top: 2px;
  font-weight: 600;
}

.odds-card {
  margin-top: 18px;
  padding: 12px;
  border-radius: 14px;
  background: var(--fill);
  border: 1px solid var(--border);
}

.odds-title {
  font-size: 12px;
  font-weight: 700;
  color: var(--muted);
  margin-bottom: 8px;
}

.odds-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 6px;
}

@media (max-width: 480px) {
  .odds-grid {
    grid-template-columns: repeat(3, 1fr);
    gap: 6px;
  }
}

.odd-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 8px;
  border-radius: 10px;
  background: var(--surface);
  border: 1px solid var(--border);
}

.odd-badge {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.odd-meta {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.odd-label {
  font-size: 11px;
  font-weight: 700;
  color: var(--text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.odd-chance {
  font-size: 10px;
  color: var(--muted);
}

.hint {
  margin-top: 12px;
  padding: 10px 12px;
  border-radius: 12px;
  background: var(--fill);
  font-size: 13px;
  text-align: center;
}

.hint.ok {
  color: #86efac;
}

.hint.bad {
  color: #fda4af;
}

/* 结果弹窗样式 */
.lottery-modal-mask {
  position: fixed;
  inset: 0;
  z-index: 1000;
  background: rgba(0, 0, 0, 0.65);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  backdrop-filter: blur(4px);
  animation: fadeIn 0.2s ease-out;
}

.lottery-modal-content {
  width: min(320px, 88vw);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 20px;
  padding: 24px 20px 20px;
  text-align: center;
  box-shadow: 0 20px 40px rgba(0, 0, 0, 0.3);
  animation: popIn 0.3s cubic-bezier(0.175, 0.885, 0.32, 1.275);
}

.lottery-modal-icon {
  font-size: 48px;
  margin-bottom: 8px;
}

.lottery-modal-title {
  font-size: 18px;
  font-weight: 800;
  margin: 0 0 8px;
  color: var(--text);
}

.lottery-modal-msg {
  font-size: 14px;
  color: var(--muted);
  line-height: 1.5;
  margin: 0 0 20px;
}

.lottery-modal-btn {
  width: 100%;
  height: 42px;
  border-radius: 12px;
  font-size: 15px;
  font-weight: 700;
}

@keyframes fadeIn {
  from { opacity: 0; }
  to { opacity: 1; }
}

@keyframes popIn {
  from { opacity: 0; transform: scale(0.85); }
  to { opacity: 1; transform: scale(1); }
}
</style>
