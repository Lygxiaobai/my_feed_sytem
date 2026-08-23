import { postJson } from './client'

export type InvoiceStatus = 'issued'

export type InvoiceProfile = {
  title: string
  email: string
  bank_name?: string
  bank_account?: string
  address?: string
  phone?: string
}

export type EligibleOrder = {
  out_trade_no: string
  yuan: number
  coins: number
  bonus: number
  pay_method: string
  paid_at: string
}

export type Invoice = {
  invoice_no: string
  out_trade_no: string
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
  status: InvoiceStatus
  issued_at?: string
  created_at: string
}

export function payMethodLabel(method: string) {
  switch (method) {
    case 'qr':
      return '支付宝扫码'
    case 'page':
      return '支付宝网页'
    case 'stripe':
      return 'Stripe'
    default:
      return method || '充值'
  }
}

export function invoiceProfile() {
  return postJson<{ profile: InvoiceProfile }>('/invoice/profile', {}, { authRequired: true })
}

export function saveInvoiceProfile(input: InvoiceProfile) {
  return postJson<{ profile: InvoiceProfile }>('/invoice/profile/save', { ...input, kind: 'personal' }, { authRequired: true })
}

export function listEligibleOrders(limit = 20, offset = 0) {
  return postJson<{ orders: EligibleOrder[] }>('/invoice/eligible', { limit, offset }, { authRequired: true })
}

export function applyInvoice(input: InvoiceProfile & { out_trade_no: string }) {
  return postJson<{ invoice: Invoice }>('/invoice/apply', { ...input, kind: 'personal' }, { authRequired: true })
}

export function listMyInvoices(limit = 20, offset = 0) {
  return postJson<{ invoices: Invoice[] }>('/invoice/list', { limit, offset }, { authRequired: true })
}

export function getInvoice(invoiceNo: string) {
  return postJson<{ invoice: Invoice }>('/invoice/get', { invoice_no: invoiceNo }, { authRequired: true })
}
