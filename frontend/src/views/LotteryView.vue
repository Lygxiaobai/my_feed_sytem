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

const prizes = LOTTERY_PRIZES
const sliceDeg = 360 / prizes.length

const wheelStyle = computed(() => ({
  transform: `rotate(${rotation.value}deg)`,
}))

onMounted(async () => {
  if (!auth.isLoggedIn) {
    await router.replace('/account')
  }
})

function labelAt(index: number) {
  return `rotate(${index * sliceDeg}deg) translateY(-118px)`
}

function landOn(prizeIndex: number) {
  const extraTurns = 6
  const currentTurns = Math.floor(rotation.value / 360)
  rotation.value = (currentTurns + extraTurns) * 360 - prizeIndex * sliceDeg
}

function finishText(coins: number) {
  if (coins === 0) return '这次没有中积分，明天再来'
  return `抽中 ${coins} 积分，15 天后过期`
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
    spinning.value = true
    landOn(idx)
    window.setTimeout(() => {
      claimed.value = true
      spinning.value = false
      busy.value = false
      result.value = finishText(res.coins)
      tone.value = res.coins === 0 ? '' : 'ok'
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
    <div class="card">
      <div class="page-head">
        <p class="title" style="margin: 0">每日抽奖</p>
        <button class="ghost compact" type="button" @click="router.push('/wallet')">钱包</button>
      </div>
      <p class="subtle" style="margin-top: 10px">
        免费抽 1 次，每个北京时间自然日一次。中奖积分 15 天后过期。可与签到同一天领取。
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
            {{ item.label }}
          </div>
        </div>
        <button
          class="hub"
          type="button"
          :disabled="busy || spinning || claimed"
          @click="onDraw"
        >
          {{ claimed ? '今日已抽过' : spinning ? '开奖中' : '抽一次' }}
        </button>
      </div>

      <div class="odds">
        <div v-for="item in prizes" :key="item.label" class="odd">
          <div class="odd-coin">{{ item.label }}</div>
          <div class="subtle">{{ item.chance }}</div>
        </div>
      </div>
      <div v-if="result" class="hint" :class="tone">{{ result }}</div>
    </div>
  </AppShell>
</template>

<style scoped>
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

.page-head {
  display: flex;
  align-items: center;
  gap: 10px;
}

.stage {
  position: relative;
  width: min(300px, 100%);
  aspect-ratio: 1;
  margin: 22px auto 8px;
}

.pointer {
  position: absolute;
  top: -6px;
  left: 50%;
  z-index: 3;
  width: 0;
  height: 0;
  border-left: 12px solid transparent;
  border-right: 12px solid transparent;
  border-top: 22px solid #fbbf24;
  transform: translateX(-50%);
  filter: drop-shadow(0 2px 4px rgba(0, 0, 0, 0.4));
}

.wheel {
  width: 100%;
  height: 100%;
  border-radius: 50%;
  border: 8px solid rgba(255, 255, 255, 0.16);
  background: conic-gradient(
    from -30deg,
    #3f3348 0deg 60deg,
    #2b4a3d 60deg 120deg,
    #2b3f55 120deg 180deg,
    #4a3d2b 180deg 240deg,
    #553344 240deg 300deg,
    #7a2438 300deg 360deg
  );
  box-shadow: inset 0 0 0 10px rgba(0, 0, 0, 0.18);
  transition: transform 4s cubic-bezier(0.12, 0.7, 0.16, 1);
}

.wheel.spinning {
  pointer-events: none;
}

.hub {
  position: absolute;
  top: 50%;
  left: 50%;
  z-index: 2;
  width: 88px;
  height: 88px;
  transform: translate(-50%, -50%);
  display: grid;
  place-items: center;
  padding: 10px;
  border: 4px solid rgba(255, 255, 255, 0.88);
  border-radius: 50%;
  background: #fe2c55;
  color: #fff;
  font-size: 14px;
  font-weight: 800;
  line-height: 1.2;
  text-align: center;
  cursor: pointer;
  box-shadow: 0 8px 20px rgba(254, 44, 85, 0.36);
}

.hub:disabled {
  cursor: default;
  background: #8a3144;
  box-shadow: none;
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
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.45);
}

.odds {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
  margin-top: 14px;
}

.odd {
  padding: 10px 8px;
  border-radius: 14px;
  border: 1px solid var(--border);
  background: var(--fill);
  text-align: center;
}

.odd-coin {
  font-weight: 800;
  font-size: 13px;
}

.hint {
  margin-top: 12px;
  padding: 10px 12px;
  border-radius: 12px;
  background: var(--fill);
}

.hint.ok {
  color: #86efac;
}

.hint.bad {
  color: #fda4af;
}

@media (max-width: 640px) {
  .odds {
    grid-template-columns: 1fr 1fr;
  }
}
</style>
