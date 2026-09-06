<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import { track } from '../analytics/track'
import AppShell from '../components/AppShell.vue'
import { ApiError } from '../api/client'
import * as walletApi from '../api/wallet'
import type { CheckinDay, CheckinMonth } from '../api/wallet'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'

const WEEKDAYS = ['一', '二', '三', '四', '五', '六', '日'] as const

const router = useRouter()
const auth = useAuthStore()
const toast = useToastStore()

const busy = ref(false)
const loading = ref(false)
const month = ref<CheckinMonth | null>(null)
const todayCoins = ref<number | null>(null)
const result = ref('')
const tone = ref('' as '' | 'ok' | 'bad')

const claimedToday = computed(() => month.value?.claimed_today ?? todayCoins.value !== null)

type CalCell = {
  key: string
  day: number | null
  bizDate: string
  coins: number | null
  isToday: boolean
}

const cells = computed<CalCell[]>(() => {
  const m = month.value
  if (!m) return []
  const prizeByDate = new Map((m.days ?? []).map((d: CheckinDay) => [d.biz_date, d.coins]))
  const lastDay = new Date(Date.UTC(m.year, m.month, 0)).getUTCDate()
  const firstWeekday = weekdayMon0(m.year, m.month, 1)
  const out: CalCell[] = []
  for (let i = 0; i < firstWeekday; i++) {
    out.push({ key: `pad-${i}`, day: null, bizDate: '', coins: null, isToday: false })
  }
  for (let day = 1; day <= lastDay; day++) {
    const bizDate = `${m.year}-${pad2(m.month)}-${pad2(day)}`
    out.push({
      key: bizDate,
      day,
      bizDate,
      coins: prizeByDate.get(bizDate) ?? null,
      isToday: bizDate === m.today,
    })
  }
  return out
})

function pad2(n: number) {
  return String(n).padStart(2, '0')
}

function weekdayMon0(year: number, month: number, day: number) {
  const utcDay = new Date(Date.UTC(year, month - 1, day, 4, 0, 0)).getUTCDay()
  return utcDay === 0 ? 6 : utcDay - 1
}

function applyMonth(data: CheckinMonth) {
  month.value = data
  const today = (data.days ?? []).find((d) => d.biz_date === data.today)
  if (data.claimed_today && today) {
    todayCoins.value = today.coins
    result.value = `今日获得 ${today.coins} 积分`
    tone.value = 'ok'
  }
}

async function loadMonth() {
  loading.value = true
  try {
    applyMonth(await walletApi.checkinMonth())
  } catch (e) {
    result.value = e instanceof ApiError ? e.message : String(e)
    tone.value = 'bad'
    toast.error(result.value)
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  if (!auth.isLoggedIn) {
    await router.replace('/account')
    return
  }
  await loadMonth()
})

function goBack() {
  if (window.history.length > 1) {
    router.back()
  } else {
    void router.push('/account')
  }
}

async function onCheckin() {
  if (busy.value || claimedToday.value) return
  busy.value = true
  try {
    const res = await walletApi.checkin()
    track('wallet_checkin', { coins: res.coins })
    todayCoins.value = res.coins
    result.value = `今日获得 ${res.coins} 积分`
    tone.value = 'ok'
    toast.success(result.value)
    applyMonth(await walletApi.checkinMonth())
  } catch (e) {
    result.value = e instanceof ApiError ? e.message : String(e)
    tone.value = 'bad'
    toast.error(result.value)
    if (e instanceof ApiError && e.message.includes('已经领取')) {
      await loadMonth()
    }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AppShell>
    <div class="card">
      <div class="page-head">
        <button class="ghost compact back-btn mobile-only" type="button" @click="goBack">← 返回</button>
        <p class="title" style="margin: 0">每日签到</p>
        <div style="flex: 1"></div>
        <button class="ghost compact" type="button" @click="router.push('/wallet')">钱包</button>
      </div>
      <p class="subtle" style="margin-top: 10px">
        每个北京时间自然日可签到 1 次，随机获得 1–20 积分（大于 10 的概率 5%），15 天后过期。
      </p>
      <p v-if="month" class="month-label">{{ month.year }} 年 {{ month.month }} 月</p>

      <div class="cal" aria-label="本月签到">
        <div v-for="w in WEEKDAYS" :key="w" class="dow">{{ w }}</div>
        <div
          v-for="cell in cells"
          :key="cell.key"
          class="day"
          :class="{
            empty: cell.day === null,
            today: cell.isToday,
            claimed: cell.coins !== null,
          }"
        >
          <template v-if="cell.day !== null">
            <span class="num">{{ cell.day }}</span>
            <span v-if="cell.coins !== null" class="coins">+{{ cell.coins }}</span>
          </template>
        </div>
      </div>

      <div v-if="result" class="hint" :class="tone">{{ result }}</div>
      <button
        class="primary"
        type="button"
        :disabled="busy || loading || claimedToday"
        style="margin-top: 16px"
        @click="onCheckin"
      >
        {{ claimedToday ? '今日已签到' : '今日签到' }}
      </button>
    </div>
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

.page-head {
  display: flex;
  align-items: center;
  gap: 10px;
}

.month-label {
  margin: 16px 0 10px;
  font-weight: 800;
}

.cal {
  display: grid;
  grid-template-columns: repeat(7, minmax(0, 1fr));
  gap: 6px;
}

.dow {
  text-align: center;
  font-size: 12px;
  color: rgba(var(--fg), 0.56);
  padding: 4px 0;
}

.day {
  min-height: 54px;
  border-radius: 12px;
  border: 1px solid transparent;
  background: rgba(var(--fg), 0.04);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2px;
}

.day.empty {
  background: transparent;
}

.day.today {
  border-color: rgba(37, 244, 238, 0.7);
}

.day.claimed {
  background: rgba(254, 44, 85, 0.16);
  border-color: rgba(254, 44, 85, 0.45);
}

.day.today.claimed {
  border-color: rgba(37, 244, 238, 0.85);
}

.num {
  font-weight: 700;
  font-size: 13px;
}

.coins {
  font-size: 11px;
  font-weight: 800;
  color: #fbbf24;
}

.hint {
  margin-top: 12px;
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
</style>
