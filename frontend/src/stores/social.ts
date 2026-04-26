import { defineStore } from 'pinia'
import { computed, reactive, ref } from 'vue'

import { ApiError } from '../api/client'
import type { SocialRelation } from '../api/types'
import * as socialApi from '../api/social'
import { useAuthStore } from './auth'

export const useSocialStore = defineStore('social', () => {
  const auth = useAuthStore()
  const pendingByVloggerId = reactive<Record<string, boolean>>({})

  const followers = ref<SocialRelation[]>([])
  const vloggers = ref<SocialRelation[]>([])

  const followersLoading = ref(false)
  const vloggersLoading = ref(false)

  const followersError = ref('')
  const vloggersError = ref('')

  const followerCount = computed(() => followers.value.length)
  const followingCount = computed(() => vloggers.value.length)

  const syncAttempts = 6
  const syncDelayMs = 400

  function clear() {
    followers.value = []
    vloggers.value = []
    followersError.value = ''
    vloggersError.value = ''
    followersLoading.value = false
    vloggersLoading.value = false
    for (const key of Object.keys(pendingByVloggerId)) {
      delete pendingByVloggerId[key]
    }
  }

  function isFollowing(accountId: number) {
    return vloggers.value.some((item) => item.vlogger_id === accountId)
  }

  function isPending(accountId: number) {
    return !!pendingByVloggerId[String(accountId)]
  }

  function setPending(accountId: number, pending: boolean) {
    const key = String(accountId)
    if (pending) {
      pendingByVloggerId[key] = true
      return
    }
    delete pendingByVloggerId[key]
  }

  function wait(ms: number) {
    return new Promise<void>((resolve) => {
      window.setTimeout(resolve, ms)
    })
  }

  function insertOptimisticVlogger(vloggerId: number, vloggerUsername?: string) {
    if (isFollowing(vloggerId)) return

    vloggers.value = vloggers.value.concat({
      id: 0,
      follower_id: Number(auth.claims?.account_id ?? 0),
      vlogger_id: vloggerId,
      created_at: new Date().toISOString(),
      follower_username: auth.claims?.username,
      vlogger_username: vloggerUsername,
    })
  }

  function removeOptimisticVlogger(vloggerId: number) {
    vloggers.value = vloggers.value.filter((item) => item.vlogger_id !== vloggerId)
  }

  async function syncVloggersUntil(vloggerId: number, shouldFollow: boolean) {
    for (let attempt = 0; attempt < syncAttempts; attempt += 1) {
      if (attempt > 0) {
        await wait(syncDelayMs)
      }
      if (!auth.isLoggedIn) return

      try {
        const res = await socialApi.getAllVloggers()
        if (res.vloggers.some((item) => item.vlogger_id === vloggerId) === shouldFollow) {
          vloggers.value = res.vloggers
          vloggersError.value = ''
          return
        }
      } catch {
        // Keep optimistic state and retry quietly.
      }
    }
  }

  async function refreshFollowers(vloggerId?: number) {
    if (!auth.isLoggedIn) {
      clear()
      return
    }

    followersLoading.value = true
    followersError.value = ''
    try {
      const res = await socialApi.getAllFollowers(vloggerId)
      followers.value = res.followers
    } catch (e) {
      followersError.value = e instanceof ApiError ? e.message : String(e)
      followers.value = []
    } finally {
      followersLoading.value = false
    }
  }

  async function refreshVloggers(followerId?: number) {
    if (!auth.isLoggedIn) {
      clear()
      return
    }

    vloggersLoading.value = true
    vloggersError.value = ''
    try {
      const res = await socialApi.getAllVloggers(followerId)
      vloggers.value = res.vloggers
    } catch (e) {
      vloggersError.value = e instanceof ApiError ? e.message : String(e)
      vloggers.value = []
    } finally {
      vloggersLoading.value = false
    }
  }

  async function refreshMine() {
    await Promise.all([refreshFollowers(), refreshVloggers()])
  }

  async function follow(vloggerId: number, vloggerUsername?: string) {
    if (!auth.isLoggedIn) throw new ApiError('需要先登录', 401)
    if (isPending(vloggerId)) return

    const previous = vloggers.value.slice()
    insertOptimisticVlogger(vloggerId, vloggerUsername)
    setPending(vloggerId, true)
    try {
      await socialApi.follow(vloggerId)
      void syncVloggersUntil(vloggerId, true).finally(() => setPending(vloggerId, false))
    } catch (e) {
      vloggers.value = previous
      setPending(vloggerId, false)
      throw e
    }
  }

  async function unfollow(vloggerId: number) {
    if (!auth.isLoggedIn) throw new ApiError('需要先登录', 401)
    if (isPending(vloggerId)) return

    const previous = vloggers.value.slice()
    removeOptimisticVlogger(vloggerId)
    setPending(vloggerId, true)
    try {
      await socialApi.unfollow(vloggerId)
      void syncVloggersUntil(vloggerId, false).finally(() => setPending(vloggerId, false))
    } catch (e) {
      vloggers.value = previous
      setPending(vloggerId, false)
      throw e
    }
  }

  return {
    followers,
    vloggers,
    followerCount,
    followingCount,
    followersLoading,
    vloggersLoading,
    followersError,
    vloggersError,
    clear,
    isFollowing,
    isPending,
    refreshMine,
    refreshFollowers,
    refreshVloggers,
    follow,
    unfollow,
  }
})
