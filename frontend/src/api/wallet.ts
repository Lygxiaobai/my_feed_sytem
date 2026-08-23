import { postJson } from './client'

export type WalletSummary = {
  available_coins: number
  expiring_soon_coins: number
  next_expire_at?: string
  next_expire_coins?: number
}

export type RechargePackage = {
  yuan: number
  bonus: number
}

export type WalletLedger = {
  id: number
  biz_type: string
  amount: number
  created_at: string
}

export type RechargeOrder = {
  out_trade_no: string
  yuan: number
  coins: number
  bonus: number
  status: 'pending' | 'paid' | 'closed'
  expire_at?: string
  paid_at?: string
}

export type TipRecord = {
  id: number
  from_account_id: number
  from_username: string
  to_account_id: number
  video_id: number
  coins: number
  received: number
  cut: number
  created_at: string
}

export type CheckinDay = {
  biz_date: string
  coins: number
}

export type CheckinMonth = {
  year: number
  month: number
  today: string
  claimed_today: boolean
  days: CheckinDay[]
}

export const LOTTERY_PRIZES = [
  { coins: 0, chance: '50%', label: '谢谢参与' },
  { coins: 2, chance: '25%', label: '2 积分' },
  { coins: 5, chance: '12%', label: '5 积分' },
  { coins: 10, chance: '8%', label: '10 积分' },
  { coins: 20, chance: '4%', label: '20 积分' },
  { coins: 50, chance: '1%', label: '50 积分' },
] as const

export async function summary() {
  return postJson<{ summary: WalletSummary; packages: RechargePackage[] }>('/wallet/summary', {}, { authRequired: true })
}

export async function listLedger(limit = 20, offset = 0) {
  return postJson<{ ledgers: WalletLedger[] }>('/wallet/ledger', { limit, offset }, { authRequired: true })
}

export async function createRecharge(yuan: number) {
  return postJson<{
    order: RechargeOrder
    method: 'stripe'
    checkout_url?: string
  }>('/wallet/recharge/create', { yuan }, { authRequired: true })
}

export async function queryRecharge(outTradeNo: string) {
  return postJson<{ order: RechargeOrder }>('/wallet/recharge/query', { out_trade_no: outTradeNo }, { authRequired: true })
}

export async function checkin() {
  return postJson<{ coins: number }>('/wallet/checkin', {}, { authRequired: true })
}

export async function checkinMonth() {
  return postJson<CheckinMonth>('/wallet/checkin/month', {}, { authRequired: true })
}

export async function lottery() {
  return postJson<{ coins: number; prize_index: number }>('/wallet/lottery', {}, { authRequired: true })
}

export async function tip(videoId: number, coins: number) {
  return postJson<{ tip: TipRecord }>('/wallet/tip', { video_id: videoId, coins }, { authRequired: true })
}

export async function listMyTips(limit = 20, offset = 0) {
  return postJson<{ tips: TipRecord[] }>('/wallet/tips/mine', { limit, offset }, { authRequired: true })
}

export async function listVideoTips(videoId: number, limit = 20, offset = 0) {
  return postJson<{ tips: TipRecord[] }>('/wallet/tips/byVideo', { video_id: videoId, limit, offset }, { authRequired: true })
}

export function ledgerLabel(bizType: string) {
  switch (bizType) {
    case 'grant_register':
      return '注册赠送'
    case 'grant_checkin':
      return '签到'
    case 'grant_lottery':
      return '抽奖'
    case 'grant_recharge':
      return '充值'
    case 'grant_recharge_bonus':
      return '充值赠送'
    case 'grant_tip':
      return '收到打赏'
    case 'consume_tip':
      return '打赏'
    case 'expire':
      return '过期清零'
    default:
      return bizType
  }
}
