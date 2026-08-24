<script setup lang="ts">
import { computed, ref } from 'vue'

import { ApiError } from '../api/client'
import { buildShareText, buildShareUrl, getShareInfo, rememberCopiedShare, type ShareInfo } from '../api/video'
import { useToastStore } from '../stores/toast'

const toast = useToastStore()

const open = ref(false)
const loading = ref(false)
const share = ref<ShareInfo | null>(null)

const shareText = computed(() => (share.value ? buildShareText(share.value) : ''))
const shareUrl = computed(() => (share.value ? buildShareUrl(share.value) : ''))

async function openFor(videoId: number) {
  open.value = true
  share.value = null
  loading.value = true
  try {
    share.value = await getShareInfo(videoId)
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : String(e))
    open.value = false
  } finally {
    loading.value = false
  }
}

function close() {
  open.value = false
}

/**
 * 复制到剪贴板，失败时降级到 prompt。
 *
 * 降级分支不是防御性冗余：navigator.clipboard 只在安全上下文存在，
 * 而本站的明文 IP 入口不是安全上下文，那条路径下它就是 undefined。
 */
async function copy(text: string, okTip: string) {
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    rememberCopiedShare(text)
    toast.success(okTip)
  } catch {
    rememberCopiedShare(text)
    window.prompt('复制下面的内容', text)
  }
}

defineExpose({ openFor, close })
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="mask" @click.self="close">
      <div class="sheet" role="dialog" aria-modal="true" aria-label="分享">
        <div class="grab" />
        <div class="head">
          <div>
            <p class="title">分享</p>
            <p class="subtle">复制后发给好友，对方打开本站即可识别</p>
          </div>
          <button class="x" type="button" aria-label="关闭" @click="close">×</button>
        </div>

        <div v-if="loading" class="empty">生成中…</div>

        <template v-else-if="share">
          <div class="code-row">
            <span class="code">{{ share.code }}</span>
            <span class="code-tip">专属口令</span>
          </div>

          <pre class="preview">{{ shareText }}</pre>

          <button class="primary wide" type="button" @click="copy(shareText, '口令已复制，发给好友打开本站即可')">
            复制口令
          </button>
          <button class="ghost wide" type="button" @click="copy(shareUrl, '链接已复制')">仅复制链接</button>
        </template>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.mask {
  position: fixed;
  inset: 0;
  z-index: 140;
  display: flex;
  align-items: flex-end;
  justify-content: center;
  padding: 0;
  background: rgba(0, 0, 0, 0.58);
  backdrop-filter: blur(10px);
}

.sheet {
  width: min(480px, 100%);
  max-height: min(80vh, 720px);
  overflow: auto;
  padding: 10px 18px 20px;
  border-radius: 20px 20px 0 0;
  border: 1px solid rgba(var(--fg), 0.12);
  border-bottom: 0;
  background: var(--surface);
  display: grid;
  gap: 12px;
}

.grab {
  width: 36px;
  height: 4px;
  margin: 4px auto 2px;
  border-radius: 999px;
  background: rgba(var(--fg), 0.2);
}

.head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}

.title {
  margin: 0;
  font-size: 18px;
  font-weight: 800;
}

.subtle {
  margin: 4px 0 0;
  color: rgba(var(--fg), 0.62);
  font-size: 13px;
}

.x {
  width: 34px;
  height: 34px;
  border-radius: 12px;
  border: 1px solid rgba(var(--fg), 0.14);
  background: rgba(var(--fg), 0.06);
  color: rgba(var(--fg), 0.9);
  cursor: pointer;
  font-size: 20px;
  line-height: 1;
}

.code-row {
  display: flex;
  align-items: baseline;
  gap: 10px;
}

.code {
  font-size: 26px;
  font-weight: 800;
  letter-spacing: 3px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  color: #fe2c55;
}

.code-tip {
  font-size: 12px;
  color: rgba(var(--fg), 0.5);
}

.preview {
  margin: 0;
  padding: 12px;
  border-radius: 14px;
  border: 1px solid rgba(var(--fg), 0.1);
  background: rgba(var(--fg), 0.05);
  color: rgba(var(--fg), 0.82);
  font-size: 13px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  font-family: inherit;
}

.wide {
  width: 100%;
}

.ghost {
  padding: 10px 14px;
  border-radius: 12px;
  border: 1px solid rgba(var(--fg), 0.16);
  background: rgba(var(--fg), 0.05);
  color: inherit;
  cursor: pointer;
}

.empty {
  padding: 28px 8px;
  text-align: center;
  color: rgba(var(--fg), 0.62);
}
</style>
