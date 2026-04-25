import { postJson } from './client'
import type { Account, BackendAccountEnvelope, MessageResponse, TokenResponse } from './types'

export function register(username: string, password: string) {
  return postJson<MessageResponse>('/account/register', { username, password })
}

export function login(username: string, password: string) {
  return postJson<TokenResponse>('/account/login', { username, password })
}

export function logout() {
  return postJson<MessageResponse>('/account/logout', {}, { authRequired: true })
}

export function rename(newUsername: string) {
  return postJson<TokenResponse>('/account/rename', { new_username: newUsername }, { authRequired: true })
}

export function changePassword(oldPassword: string, newPassword: string) {
  return postJson<MessageResponse>(
    '/account/changePassword',
    {
      old_password: oldPassword,
      new_password: newPassword,
    },
    { authRequired: true },
  )
}

export async function findById(id: number) {
  const res = await postJson<BackendAccountEnvelope>('/account/findByID', { id })
  return res.account as Account
}

export async function findByUsername(username: string) {
  const res = await postJson<BackendAccountEnvelope>('/account/findByUsername', { username })
  return res.account as Account
}
