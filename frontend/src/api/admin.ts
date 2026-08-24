import { postJson, resolveAssetUrl } from './client'
import type { AuditStatus } from './types'

export type AdminAccess = {
  allowed: boolean
}

export type AdminOverview = {
  pending_reports: number
  account_id: number
  username: string
  issued_invoices: number
  paid_yuan: number
  paid_orders: number
  pending_orders: number
  available_coins: number
  video_count: number
  account_count: number
}

export type AdminInterestTag = {
  label: string
  video_id?: number
  source?: string
}

export type AdminVideoBoard = {
  summary: {
    total: number
    pending: number
    reviewing: number
    approved: number
    rejected: number
  }
  videos: AdminVideo[]
  has_more: boolean
}

export type AdminAccountBoard = {
  summary: {
    total: number
  }
  accounts: AdminAccount[]
  has_more: boolean
}

export type AdminInvoice = {
  invoice_no: string
  out_trade_no: string
  account_id: number
  username: string
  yuan: number
  coins: number
  bonus: number
  pay_method: string
  paid_at: string
  title: string
  email: string
  bank_name?: string
  bank_account?: string
  address?: string
  phone?: string
  status: string
  issued_at?: string
  created_at: string
}

export type AdminInvoiceBoard = {
  summary: {
    issued_count: number
    yuan_total: number
    coins_total: number
  }
  invoices: AdminInvoice[]
  has_more: boolean
}

export type AdminPayment = {
  out_trade_no: string
  account_id: number
  username: string
  yuan: number
  coins: number
  bonus: number
  status: string
  pay_method: string
  pay_ref?: string
  paid_at?: string
  closed_at?: string
  expire_at: string
  created_at: string
}

export type AdminPaymentBoard = {
  summary: {
    paid_count: number
    paid_yuan: number
    pending_count: number
    closed_count: number
    platform_cut_coins: number
  }
  orders: AdminPayment[]
  has_more: boolean
}

export type AdminBalance = {
  account_id: number
  username: string
  available_coins: number
  expiring_soon_coins: number
  next_expire_at?: string
}

export type AdminBalanceBoard = {
  summary: {
    accounts_with_balance: number
    available_coins: number
    expiring_soon_coins: number
  }
  balances: AdminBalance[]
  has_more: boolean
}

export type AdminLot = {
  source: string
  remaining: number
  expire_at?: string
  created_at: string
}

export type AdminBalanceDetail = {
  account_id: number
  username: string
  summary: {
    available_coins: number
    expiring_soon_coins: number
    next_expire_at?: string
    next_expire_coins?: number
  }
  lots: AdminLot[]
}

export type AdminVideo = {
  id: number
  author_id: number
  username: string
  title: string
  description?: string
  tags?: string[]
  play_url: string
  cover_url: string
  likes_count: number
  comment_count: number
  audit_status: AuditStatus
  created_at: string
  pending_reports: number
}

export type AdminAccount = {
  id: number
  username: string
  email?: string
  follower_count: number
  created_at: string
  interest_tags?: AdminInterestTag[]
}

export const ADMIN_NOTE_MAX = 500

export function adminAccess() {
  return postJson<AdminAccess>('/admin/access', {}, { authRequired: true })
}

export function adminOverview() {
  return postJson<AdminOverview>('/admin/overview', {}, { authRequired: true })
}

function normalizeVideo(video: AdminVideo): AdminVideo {
  return {
    ...video,
    play_url: resolveAssetUrl(video.play_url),
    cover_url: resolveAssetUrl(video.cover_url),
    comment_count: video.comment_count ?? 0,
    pending_reports: video.pending_reports ?? 0,
  }
}

export async function lookupAdminVideo(videoId: number) {
  const res = await postJson<{ video: AdminVideo }>('/admin/videos/lookup', { video_id: videoId }, { authRequired: true })
  return normalizeVideo(res.video)
}

export function takedownAdminVideo(videoId: number, note: string) {
  return postJson<{ ok: boolean }>('/admin/videos/takedown', { video_id: videoId, note }, { authRequired: true })
}

export async function lookupAdminAccount(input: { id?: number; username?: string; email?: string }) {
  const res = await postJson<{ account: AdminAccount; videos: AdminVideo[] }>(
    '/admin/accounts/lookup',
    input,
    { authRequired: true },
  )
  return {
    account: res.account,
    videos: (res.videos ?? []).map(normalizeVideo),
  }
}

export async function listAdminVideos(input: { query?: string; audit_status?: string; author_id?: number; limit?: number; offset?: number } = {}) {
  const res = await postJson<AdminVideoBoard>('/admin/videos/list', input, { authRequired: true })
  return {
    ...res,
    videos: (res.videos ?? []).map(normalizeVideo),
  }
}

export async function listAdminAccounts(input: { query?: string; limit?: number; offset?: number } = {}) {
  return postJson<AdminAccountBoard>('/admin/accounts/list', input, { authRequired: true })
}

export function listAdminInvoices(input: { invoice_no?: string; out_trade_no?: string; account_id?: number; limit?: number; offset?: number } = {}) {
  return postJson<AdminInvoiceBoard>('/admin/invoices/list', input, { authRequired: true })
}

export function getAdminInvoice(invoiceNo: string) {
  return postJson<{ invoice: AdminInvoice }>('/admin/invoices/get', { invoice_no: invoiceNo }, { authRequired: true })
}

export function listAdminPayments(input: { status?: string; out_trade_no?: string; account_id?: number; limit?: number; offset?: number } = {}) {
  return postJson<AdminPaymentBoard>('/admin/payments/list', input, { authRequired: true })
}

export function listAdminBalances(input: { id?: number; username?: string; limit?: number; offset?: number } = {}) {
  return postJson<AdminBalanceBoard>('/admin/balances/list', input, { authRequired: true })
}

export function getAdminBalance(accountId: number) {
  return postJson<{ balance: AdminBalanceDetail }>('/admin/balances/get', { id: accountId }, { authRequired: true })
}

export const AUDIT_STATUS_LABEL: Record<AuditStatus, string> = {
  pending: '待审',
  reviewing: '复审中',
  approved: '公开',
  rejected: '已下架',
}

export const ORDER_STATUS_LABEL: Record<string, string> = {
  pending: '待支付',
  paid: '已支付',
  closed: '已关闭',
}

export const LOT_SOURCE_LABEL: Record<string, string> = {
  checkin: '签到',
  lottery: '抽奖',
  recharge: '充值',
  register: '注册赠金',
  tip_in: '收到打赏',
}
