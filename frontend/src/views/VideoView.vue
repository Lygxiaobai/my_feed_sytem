<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { onBeforeRouteLeave, RouterLink, useRoute, useRouter } from 'vue-router'

import { track } from '../analytics/track'
import AppIcon from '../components/AppIcon.vue'
import AppShell from '../components/AppShell.vue'
import { AbortedError, ApiError, type FormUploadProgress } from '../api/client'
import * as videoApi from '../api/video'
import type { Video } from '../api/types'
import type { VideoUploadTask } from '../api/video'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const toast = useToastStore()

/**
 * idle：未选文件，或已取消/尚未重新开始。
 * ready：转码完成，可以立刻发布，不必再传一遍。
 * done：本条已发布，展示完成态而不是空表单。
 */
type PublishPhase = 'idle' | 'uploading' | 'processing' | 'ready' | 'publishing' | 'failed' | 'done'

const phase = ref<PublishPhase>('idle')
const uploadPercent = ref(0)
const uploadLoaded = ref(0)
const uploadTotal = ref(0)
const uploadStage = ref<FormUploadProgress['stage']>('sending')
const fileError = ref('')
const lastError = ref('')
const published = ref<Video | null>(null)
const publishRequestKey = ref('')
const publishRequested = ref(false)
const videoInput = ref<HTMLInputElement | null>(null)
const previewVideoUrl = ref('')
const readyTask = ref<VideoUploadTask | null>(null)
const draftId = ref(0)
const dragOver = ref(false)
const savingDraft = ref(false)

let abortController: AbortController | null = null
let prepareRunId = 0

const publishForm = reactive({
  title: '',
  description: '',
  video: null as File | null,
})

const formatFileSize = videoApi.formatFileSize
const maxSizeText = videoApi.formatFileSize(videoApi.MAX_VIDEO_BYTES)
const titleMax = videoApi.MAX_TITLE_CHARS
const descMax = videoApi.MAX_DESCRIPTION_CHARS
const tagMax = videoApi.MAX_VIDEO_TAGS
const tagCharMax = videoApi.MAX_TAG_CHARS
const publishTags = ref<string[]>([])
const tagDraft = ref('')
const inferredTags = computed(() =>
  videoApi.inferTags(publishForm.title, publishForm.description),
)
const unusedInferredTags = computed(() =>
  inferredTags.value.filter(
    (tag) => !publishTags.value.some((item) => item.toLowerCase() === tag.toLowerCase()),
  ),
)
const tagHint = computed(() => {
  if (publishTags.value.length > 0) return '点选会填入标签，也可以自己写'
  if (inferredTags.value.length > 0) return '点选填入标签，也可以自己写。不选则发布时用这些建议'
  return '填好标题或描述后，这里会出现可点的 #标签'
})

const locked = computed(() => phase.value === 'publishing' || phase.value === 'done')
const preparing = computed(() => phase.value === 'uploading' || phase.value === 'processing')
const canChangeFile = computed(() => !locked.value)
const canSubmit = computed(() => {
  if (locked.value || !auth.isLoggedIn) return false
  if (!publishForm.title.trim() || fileError.value) return false
  return !!publishForm.video || !!readyTask.value
})

const canSaveDraft = computed(() => {
  if (locked.value || !auth.isLoggedIn || savingDraft.value) return false
  return !!readyTask.value?.play_url && !!readyTask.value.cover_url
})

const shouldWarnLeave = computed(() =>
  phase.value === 'uploading' ||
  phase.value === 'processing' ||
  phase.value === 'publishing',
)

const isConfirming = computed(() => phase.value === 'uploading' && uploadStage.value === 'confirming')
const showUploadPercent = computed(() => phase.value === 'uploading' && uploadStage.value === 'sending')

const phaseText = computed(() => {
  if (phase.value === 'uploading') {
    if (uploadStage.value === 'confirming') return '正在完成上传'
    return `上传中 ${uploadPercent.value}%`
  }
  if (phase.value === 'processing') return '处理中'
  if (phase.value === 'ready') return '已就绪'
  if (phase.value === 'publishing') return '发布中'
  if (phase.value === 'failed') return lastError.value || '处理失败'
  return ''
})

const progressHint = computed(() => {
  if (phase.value === 'processing') return '正在准备可播放的视频'
  if (phase.value === 'publishing') return '马上就好'
  if (isConfirming.value && uploadTotal.value > 0) {
    return `已发送 ${formatFileSize(uploadLoaded.value)} / ${formatFileSize(uploadTotal.value)}，正在确认`
  }
  if (isConfirming.value) return '已发出，正在确认接收'
  if (uploadTotal.value > 0) {
    return `${formatFileSize(uploadLoaded.value)} / ${formatFileSize(uploadTotal.value)}`
  }
  return ''
})

const progressBarWidth = computed(() => uploadPercent.value)

function compactTag(raw: string) {
  const label = raw.trim().replace(/^#/, '')
  if (!label) return ''
  return [...label].slice(0, tagCharMax).join('')
}

function addPublishTag(raw = tagDraft.value) {
  const label = compactTag(raw)
  tagDraft.value = ''
  if (!label || locked.value) return
  const exists = publishTags.value.some((item) => item.toLowerCase() === label.toLowerCase())
  if (exists) return
  if (publishTags.value.length >= tagMax) {
    toast.error(`最多 ${tagMax} 个标签`)
    return
  }
  publishTags.value = [...publishTags.value, label]
}

function removePublishTag(label: string) {
  if (locked.value) return
  publishTags.value = publishTags.value.filter((item) => item !== label)
}

function onTagKeydown(event: KeyboardEvent) {
  if (event.key === 'Enter' || event.key === ',') {
    event.preventDefault()
    addPublishTag()
  }
  if (event.key === 'Backspace' && !tagDraft.value && publishTags.value.length) {
    removePublishTag(publishTags.value[publishTags.value.length - 1] ?? '')
  }
}

const submitLabel = computed(() => {
  if (phase.value === 'publishing') return '发布中'
  if (publishRequested.value && preparing.value) return '处理完成后发布'
  if (phase.value === 'failed') return '重试并发布'
  return '发布'
})

const indeterminate = computed(() => phase.value === 'processing' || phase.value === 'publishing')
const showProgress = computed(
  () => phase.value === 'uploading' || phase.value === 'processing' || phase.value === 'publishing',
)

function setPreviewVideo(file: File | null) {
  if (previewVideoUrl.value) URL.revokeObjectURL(previewVideoUrl.value)
  previewVideoUrl.value = file ? URL.createObjectURL(file) : ''
}

watch(
  () => publishForm.video,
  (file) => setPreviewVideo(file),
)

watch(
  () => [publishForm.title, publishForm.description, publishForm.video, publishTags.value.join('\n')],
  () => {
    if (phase.value !== 'publishing') publishRequestKey.value = ''
  },
)

function abortInFlight() {
  abortController?.abort()
  abortController = null
}

function resetUploadProgress() {
  uploadPercent.value = 0
  uploadLoaded.value = 0
  uploadTotal.value = 0
  uploadStage.value = 'sending'
}

function resetMediaState() {
  resetUploadProgress()
  readyTask.value = null
  lastError.value = ''
  publishRequested.value = false
}

function onBeforeUnload(event: BeforeUnloadEvent) {
  if (!shouldWarnLeave.value) return
  event.preventDefault()
  event.returnValue = ''
}

onMounted(() => {
  window.addEventListener('beforeunload', onBeforeUnload)
  void loadDraftFromQuery()
})

onUnmounted(() => {
  window.removeEventListener('beforeunload', onBeforeUnload)
  abortInFlight()
  prepareRunId += 1
  setPreviewVideo(null)
})

onBeforeRouteLeave(async (_to, _from, next) => {
  if (phase.value === 'ready' && readyTask.value?.play_url && readyTask.value.cover_url) {
    await persistDraft({ silent: true })
    next()
    return
  }
  if (!shouldWarnLeave.value) {
    next()
    return
  }
  const ok = window.confirm('视频还在处理中，离开将取消未完成的上传。确定离开？')
  if (ok) {
    abortInFlight()
    next()
    return
  }
  next(false)
})

async function requireLogin() {
  toast.error('请先登录')
  await router.push('/account')
}

function openPicker() {
  if (!canChangeFile.value) return
  if (!auth.isLoggedIn) {
    void requireLogin()
    return
  }
  if (videoInput.value) videoInput.value.value = ''
  videoInput.value?.click()
}

function acceptFile(file: File) {
  if (!canChangeFile.value) return
  if (!auth.isLoggedIn) {
    void requireLogin()
    return
  }

  const message = videoApi.validateVideoFile(file)
  if (message) {
    fileError.value = message
    toast.error(message)
    return
  }

  fileError.value = ''
  publishRequested.value = false
  publishForm.video = file
  void prepareMedia(file)
}

function pickVideo(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0] ?? null
  if (!file) return
  acceptFile(file)
}

function onDragOver(event: DragEvent) {
  event.preventDefault()
  if (!canChangeFile.value || !auth.isLoggedIn) return
  dragOver.value = true
}

function onDragLeave(event: DragEvent) {
  const next = event.relatedTarget as Node | null
  const current = event.currentTarget as Node
  if (next && current.contains(next)) return
  dragOver.value = false
}

function onDrop(event: DragEvent) {
  event.preventDefault()
  dragOver.value = false
  if (!canChangeFile.value) return
  if (!auth.isLoggedIn) {
    void requireLogin()
    return
  }
  const files = event.dataTransfer?.files
  const file = files?.[0]
  if (!file) return
  if (files && files.length > 1) {
    toast.info('一次只能发布一个视频，已使用第一个文件')
  }
  acceptFile(file)
}

function clearVideo() {
  if (!canChangeFile.value) return
  abortInFlight()
  prepareRunId += 1
  publishForm.video = null
  fileError.value = ''
  resetMediaState()
  phase.value = 'idle'
  if (videoInput.value) videoInput.value.value = ''
}

function cancel() {
  if (!preparing.value) return
  publishRequested.value = false
  abortInFlight()
  toast.info('已取消')
}

function awaitingReview(video: Video | null) {
  return video?.audit_status === 'pending' || video?.audit_status === 'reviewing'
}

async function prepareMedia(file: File) {
  const myRun = ++prepareRunId
  abortInFlight()
  const controller = new AbortController()
  abortController = controller
  readyTask.value = null
  lastError.value = ''
  resetUploadProgress()
  phase.value = 'uploading'

  try {
    const task = await videoApi.uploadVideo(file, {
      onProgress: (progress) => {
        if (myRun !== prepareRunId) return
        // 100% 只表示请求已结束，下一拍就会切到处理中；这里不再写入，避免闪一下 100%。
        if (progress.stage === 'done') return
        uploadPercent.value = progress.percent
        uploadLoaded.value = progress.loaded
        uploadTotal.value = progress.total
        uploadStage.value = progress.stage
      },
      signal: controller.signal,
    })
    if (myRun !== prepareRunId) return

    const alreadyReady = task.status === 'ready' && !!task.play_url && !!task.cover_url
    if (!alreadyReady) phase.value = 'processing'
    const ready = alreadyReady
      ? task
      : await videoApi.waitForVideoUpload(task.id, { signal: controller.signal })
    if (myRun !== prepareRunId) return

    readyTask.value = ready
    phase.value = 'ready'
    if (!publishRequested.value) {
      await persistDraft({ silent: true })
    }
    if (publishRequested.value) {
      await commitPublish(ready)
    }
  } catch (error) {
    if (myRun !== prepareRunId) return
    if (error instanceof AbortedError) {
      phase.value = 'idle'
      resetUploadProgress()
      readyTask.value = null
      return
    }
    lastError.value = error instanceof ApiError ? error.message : String(error)
    phase.value = 'failed'
    publishRequested.value = false
    toast.error(lastError.value)
  } finally {
    if (myRun === prepareRunId) abortController = null
  }
}

async function commitPublish(task: VideoUploadTask) {
  if (phase.value === 'publishing' || phase.value === 'done') return

  const title = publishForm.title.trim().slice(0, titleMax)
  const description = publishForm.description.trim().slice(0, descMax)
  if (!title) {
    publishRequested.value = false
    toast.error('请输入标题')
    return
  }
  if (!task.play_url || !task.cover_url) {
    publishRequested.value = false
    phase.value = 'failed'
    lastError.value = '视频还没准备好，请重试'
    toast.error(lastError.value)
    return
  }

  phase.value = 'publishing'
  if (!publishRequestKey.value) publishRequestKey.value = videoApi.createIdempotencyKey()

  try {
    // 封面由后端转码时自动取视频首帧生成，用户侧不再有封面这个概念。
    const res = await videoApi.publishVideo(
      {
        title,
        description,
        tags: publishTags.value,
        play_url: task.play_url,
        cover_url: task.cover_url,
        draft_id: draftId.value || undefined,
      },
      { idempotencyKey: publishRequestKey.value },
    )
    draftId.value = 0

    published.value = res
    track('video_publish', { video_id: res.id })
    publishRequestKey.value = ''
    publishRequested.value = false
    phase.value = 'done'
    toast.success(awaitingReview(res) ? '已提交，审核通过后将出现在信息流中' : '发布成功')
  } catch (error) {
    publishRequested.value = false
    // 媒体已就绪，失败只影响这一次发布，不必重新上传。
    phase.value = 'ready'
    toast.error(error instanceof ApiError ? error.message : String(error))
  }
}

async function onPublish() {
  if (!canSubmit.value) return
  if (!auth.isLoggedIn) {
    await requireLogin()
    return
  }

  const file = publishForm.video
  if (!file) {
    toast.error('请选择视频文件')
    return
  }

  publishRequested.value = true
  published.value = null

  if (phase.value === 'ready' && readyTask.value) {
    await commitPublish(readyTask.value)
    return
  }
  if (preparing.value) return
  await prepareMedia(file)
}

function startAnother() {
  abortInFlight()
  prepareRunId += 1
  published.value = null
  draftId.value = 0
  publishForm.title = ''
  publishForm.description = ''
  publishForm.video = null
  publishTags.value = []
  tagDraft.value = ''
  fileError.value = ''
  resetMediaState()
  phase.value = 'idle'
  if (videoInput.value) videoInput.value.value = ''
  setPreviewVideo(null)
}

function applyDraft(video: Video) {
  draftId.value = video.id
  publishForm.title = video.title ?? ''
  publishForm.description = video.description ?? ''
  publishTags.value = video.tags ? [...video.tags] : []
  readyTask.value = {
    id: video.id,
    status: 'ready',
    play_url: video.play_url,
    cover_url: video.cover_url,
    content_type: 'video/mp4',
    created_at: video.created_at,
    updated_at: video.created_at,
  }
  phase.value = 'ready'
}

async function persistDraft(options?: { silent?: boolean }) {
  const task = readyTask.value
  if (!task?.play_url || !task.cover_url || savingDraft.value) return null
  savingDraft.value = true
  try {
    const saved = await videoApi.saveDraft({
      id: draftId.value || undefined,
      title: publishForm.title.trim().slice(0, titleMax),
      description: publishForm.description.trim().slice(0, descMax),
      tags: publishTags.value,
      play_url: task.play_url,
      cover_url: task.cover_url,
    })
    draftId.value = saved.id
    if (!options?.silent) toast.success('已保存到草稿箱')
    return saved
  } catch (error) {
    if (!options?.silent) {
      toast.error(error instanceof ApiError ? error.message : String(error))
    }
    return null
  } finally {
    savingDraft.value = false
  }
}

async function loadDraftFromQuery() {
  const raw = route.query.draft
  const id = Number(Array.isArray(raw) ? raw[0] : raw)
  if (!Number.isFinite(id) || id <= 0) return
  try {
    const video = await videoApi.getDetail(id)
    if (!videoApi.isDraft(video)) {
      toast.info('该内容已不是草稿')
      await router.replace(`/video/${video.id}`)
      return
    }
    applyDraft(video)
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : '草稿不存在')
  }
}

async function onSaveDraft() {
  if (!canSaveDraft.value) return
  await persistDraft()
}
</script>

<template>
  <AppShell>
    <div v-if="!auth.isLoggedIn" class="card">
      <p class="title" style="margin: 0">发布视频</p>
      <p class="subtle" style="margin: 10px 0 16px">登录后即可投稿。选择视频后会立刻开始上传，你可以同时填写标题。</p>
      <RouterLink class="pill" to="/account">去登录</RouterLink>
    </div>

    <div v-else-if="phase === 'done' && published" class="card done-card">
      <p class="title" style="margin: 0">{{ awaitingReview(published) ? '已提交审核' : '发布成功' }}</p>
      <p class="done-title">{{ published.title }}</p>
      <p class="audit-tip">
        {{
          awaitingReview(published)
            ? '审核通过后才会出现在信息流中，你可以先在「我的」里查看'
            : '已出现在信息流中'
        }}
      </p>
      <div class="row done-actions">
        <RouterLink class="pill" :to="`/video/${published.id}`">去播放</RouterLink>
        <button type="button" @click="startAnother">再发一条</button>
      </div>
    </div>

    <div v-else class="card">
      <div class="composer-head">
        <p class="title" style="margin: 0">{{ draftId ? '继续编辑草稿' : '发布视频' }}</p>
        <RouterLink
          v-if="auth.claims?.account_id"
          class="draft-link"
          :to="'/account?works=drafts'"
        >
          草稿箱
        </RouterLink>
      </div>
      <p class="subtle" style="margin: 8px 0 0">选好视频后会立刻上传。没发完的会进草稿箱，可以稍后继续。</p>

        <input
          ref="videoInput"
          class="file-native"
          type="file"
          accept="video/*"
          :disabled="locked"
          @change="pickVideo"
        />

        <div v-if="!publishForm.video && !readyTask" class="empty-block">
          <button
            class="dropzone"
            :class="{ over: dragOver }"
            type="button"
            @click="openPicker"
            @dragover="onDragOver"
            @dragleave="onDragLeave"
            @drop="onDrop"
          >
            <span class="drop-cover">
              <AppIcon name="plus-box" :size="28" />
            </span>
            <span class="drop-copy">
              <span class="dropzone-title">将视频拖到这里，或点击选择</span>
              <span class="dropzone-hint">支持常见视频格式，最大 {{ maxSizeText }}</span>
            </span>
          </button>
          <p v-if="fileError" class="file-tip bad">{{ fileError }}</p>
        </div>

        <div
          v-else
          class="composer"
          :class="{ over: dragOver }"
          @dragover="onDragOver"
          @dragleave="onDragLeave"
          @drop="onDrop"
        >
          <div class="preview-col">
            <div class="preview-card">
              <video
                v-if="previewVideoUrl || readyTask?.play_url"
                class="video"
                :src="previewVideoUrl || readyTask?.play_url"
                controls
                playsinline
                preload="metadata"
              />
            </div>
            <div class="file-meta">
              <div class="file-name">{{ publishForm.video?.name || '已保存的视频' }}</div>
              <div class="file-tip">
                {{
                  publishForm.video
                    ? `${formatFileSize(publishForm.video.size)}，上限 ${maxSizeText}`
                    : '来自草稿，可直接发布或更换文件'
                }}
              </div>
              <div v-if="phaseText" class="status-line" :class="{ bad: phase === 'failed', ok: phase === 'ready' }">
                {{ phaseText }}
              </div>
            </div>
            <div class="row file-actions">
              <button type="button" :disabled="!canChangeFile" @click="openPicker">更换</button>
              <button type="button" :disabled="!canChangeFile" @click="clearVideo">清除</button>
            </div>
          </div>

          <div class="form-col">
            <div>
              <div class="field-head">
                <label for="publish-title">标题</label>
                <span class="count">{{ publishForm.title.length }} / {{ titleMax }}</span>
              </div>
              <input
                id="publish-title"
                v-model="publishForm.title"
                class="big-input"
                :disabled="locked"
                :maxlength="titleMax"
                placeholder="给视频起个标题"
              />
            </div>

            <div>
              <div class="field-head">
                <label for="publish-desc">描述</label>
                <span class="count">{{ publishForm.description.length }} / {{ descMax }}</span>
              </div>
              <textarea
                id="publish-desc"
                v-model="publishForm.description"
                class="big-input desc-input"
                :disabled="locked"
                :maxlength="descMax"
                placeholder="选填"
              />
            </div>

            <div>
              <div class="field-head">
                <label for="publish-tag">标签</label>
                <span class="count">{{ publishTags.length }} / {{ tagMax }}</span>
              </div>
              <div class="tag-editor" :class="{ locked }">
                <button
                  v-for="tag in publishTags"
                  :key="tag"
                  class="tag-chip"
                  type="button"
                  :disabled="locked"
                  @click="removePublishTag(tag)"
                >
                  {{ tag }}
                  <span aria-hidden="true">×</span>
                </button>
                <input
                  id="publish-tag"
                  v-model="tagDraft"
                  class="tag-input"
                  :disabled="locked || publishTags.length >= tagMax"
                  :maxlength="tagCharMax"
                  placeholder="回车添加，最多 7 个"
                  @keydown="onTagKeydown"
                  @blur="addPublishTag()"
                />
              </div>
              <div v-if="unusedInferredTags.length" class="tag-suggest">
                <p class="tag-suggest-label">从标题和描述发现</p>
                <div class="tag-suggest-list">
                  <button
                    v-for="tag in unusedInferredTags"
                    :key="tag"
                    class="tag-suggest-chip"
                    type="button"
                    :disabled="locked || publishTags.length >= tagMax"
                    @click="addPublishTag(tag)"
                  >
                    #{{ tag }}
                  </button>
                </div>
              </div>
              <p class="file-tip">{{ tagHint }}</p>
            </div>

            <div
              v-if="showProgress"
              class="progress"
              :class="{ confirming: isConfirming, busy: indeterminate }"
            >
              <div class="progress-head">
                <div class="progress-copy">
                  <span class="progress-title">{{ phaseText }}</span>
                  <span v-if="progressHint" class="progress-hint">{{ progressHint }}</span>
                </div>
                <div class="progress-aside">
                  <span v-if="showUploadPercent" class="progress-percent">{{ uploadPercent }}%</span>
                  <button v-if="preparing" class="cancel-btn" type="button" @click="cancel">取消</button>
                </div>
              </div>
              <div
                class="progress-track"
                role="progressbar"
                :aria-valuemin="0"
                :aria-valuemax="100"
                :aria-valuenow="indeterminate ? undefined : progressBarWidth"
                :aria-valuetext="phaseText"
                :aria-busy="indeterminate || isConfirming"
              >
                <div
                  class="progress-bar"
                  :class="{ indeterminate, confirming: isConfirming }"
                  :style="indeterminate ? undefined : { width: `${progressBarWidth}%` }"
                />
              </div>
            </div>

            <p v-else-if="phase === 'failed'" class="file-tip bad">{{ lastError || '处理失败，可更换视频或重试' }}</p>
            <p v-else-if="phase === 'ready'" class="file-tip ok">视频已准备好，填写标题后即可发布</p>

            <div class="row submit-row">
              <button class="ghost-btn" type="button" :disabled="!canSaveDraft" @click="onSaveDraft">
                {{ savingDraft ? '保存中' : draftId ? '更新草稿' : '存草稿' }}
              </button>
              <button class="primary big-btn" type="button" :disabled="!canSubmit" @click="onPublish">
                {{ submitLabel }}
              </button>
            </div>
          </div>
        </div>
    </div>
  </AppShell>
</template>

<style scoped>
.file-native {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}

.empty-block {
  margin-top: 14px;
}

.composer-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.draft-link {
  font-size: 13px;
  color: rgba(var(--fg), 0.62);
  text-decoration: none;
}

.draft-link:hover {
  color: rgba(var(--fg), 0.9);
}

.ghost-btn {
  border: 1px solid rgba(var(--fg), 0.14);
  background: var(--fill);
  color: rgba(var(--fg), 0.86);
  border-radius: 14px;
  padding: 12px 16px;
  cursor: pointer;
}

.ghost-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.dropzone {
  display: grid;
  grid-template-columns: 220px minmax(0, 1fr);
  gap: 14px;
  width: 100%;
  min-height: 0;
  padding: 0;
  align-items: center;
  text-align: left;
  border: 1px dashed rgba(var(--fg), 0.22);
  background: var(--panel);
  border-radius: 12px;
  overflow: hidden;
  color: rgba(var(--fg), 0.86);
}

.dropzone:hover,
.dropzone.over {
  border-color: rgba(254, 44, 85, 0.55);
  background: rgba(254, 44, 85, 0.08);
}

.drop-cover {
  aspect-ratio: 16/9;
  display: grid;
  place-items: center;
  background: rgba(var(--fg), 0.04);
  color: rgba(var(--fg), 0.45);
}

.drop-copy {
  display: grid;
  gap: 6px;
  padding: 12px 14px 12px 0;
  justify-items: start;
}

.dropzone-title {
  font-size: 15px;
  font-weight: 600;
}

.dropzone-hint {
  font-size: 12px;
  color: rgba(var(--fg), 0.55);
}

.composer {
  display: grid;
  grid-template-columns: minmax(0, 0.9fr) minmax(0, 1.1fr);
  gap: 20px;
  margin-top: 18px;
  border-radius: 16px;
}

.composer.over {
  outline: 1px dashed rgba(254, 44, 85, 0.55);
  outline-offset: 6px;
}

.preview-col,
.form-col {
  display: grid;
  gap: 12px;
  align-content: start;
}

.preview-card {
  border: 1px solid rgba(var(--fg), 0.12);
  background: rgba(0, 0, 0, 0.35);
  border-radius: 16px;
  overflow: hidden;
}

.video {
  display: block;
  width: 100%;
  max-height: 420px;
  aspect-ratio: 16/9;
  object-fit: contain;
  background: rgba(0, 0, 0, 0.35);
}

.file-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
  color: rgba(var(--fg), 0.88);
}

.file-tip {
  margin-top: 4px;
  font-size: 12px;
  color: rgba(var(--fg), 0.6);
}

.file-tip.bad,
.status-line.bad {
  color: rgba(254, 44, 85, 0.92);
}

.file-tip.ok,
.status-line.ok {
  color: rgba(34, 197, 94, 0.95);
}

.status-line {
  margin-top: 6px;
  font-size: 12px;
  color: rgba(var(--fg), 0.7);
}

.file-actions {
  margin-top: 2px;
}

.field-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}

.count {
  font-size: 12px;
  color: rgba(var(--fg), 0.45);
}

.big-input {
  box-sizing: border-box;
  width: 100%;
  max-width: 100%;
  padding: 12px 14px;
  font-size: 14px;
  border-radius: 14px;
}

.desc-input {
  min-height: 120px;
}

.tag-editor {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  min-height: 48px;
  padding: 8px;
  border: 1px solid rgba(var(--fg), 0.12);
  border-radius: 14px;
  background: rgba(var(--fg), 0.03);
}

.tag-editor.locked {
  opacity: 0.7;
}

.tag-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-height: 28px;
  padding: 2px 10px;
  border: 0;
  border-radius: 999px;
  background: rgba(254, 44, 85, 0.12);
  color: inherit;
  font-size: 13px;
}

.tag-input {
  flex: 1 1 140px;
  min-width: 120px;
  min-height: 32px;
  padding: 0 6px;
  border: 0;
  background: transparent;
  color: inherit;
  font-size: 14px;
}

.tag-input:focus {
  outline: none;
}

.tag-suggest {
  display: grid;
  gap: 8px;
  margin-top: 8px;
}

.tag-suggest-label {
  margin: 0;
  color: rgba(var(--fg), 0.45);
  font-size: 12px;
}

.tag-suggest-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.tag-suggest-chip {
  min-height: 28px;
  padding: 2px 10px;
  border: 1px dashed rgba(254, 44, 85, 0.35);
  border-radius: 999px;
  background: transparent;
  color: inherit;
  font-size: 13px;
}

.tag-suggest-chip:disabled {
  opacity: 0.5;
}

.big-btn {
  padding: 12px 18px;
  font-size: 14px;
  border-radius: 14px;
  min-width: 140px;
}

.submit-row {
  justify-content: flex-end;
  margin-top: 4px;
}

.progress {
  display: grid;
  gap: 12px;
  padding: 14px 14px 12px;
  border-radius: 16px;
  background: linear-gradient(180deg, rgba(254, 44, 85, 0.08), rgba(37, 244, 238, 0.04));
  border: 1px solid rgba(254, 44, 85, 0.16);
}

.progress-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.progress-copy {
  min-width: 0;
}

.progress-title {
  display: block;
  font-size: 13px;
  font-weight: 600;
  color: rgba(var(--fg), 0.92);
}

.progress-hint {
  display: block;
  margin-top: 2px;
  font-size: 12px;
  color: rgba(var(--fg), 0.52);
}

.progress-aside {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-shrink: 0;
}

.progress-percent {
  font-variant-numeric: tabular-nums;
  font-size: 22px;
  font-weight: 700;
  letter-spacing: -0.04em;
  line-height: 1;
  background: linear-gradient(90deg, #25f4ee, #fe2c55);
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}

.cancel-btn {
  padding: 5px 12px;
  border-radius: 999px;
  font-size: 12px;
  min-height: 0;
  color: rgba(var(--fg), 0.72);
}

.progress-track {
  position: relative;
  height: 10px;
  border-radius: 999px;
  background: rgba(var(--fg), 0.08);
  overflow: hidden;
  box-shadow: inset 0 1px 2px rgba(0, 0, 0, 0.16);
}

.progress-bar {
  position: relative;
  height: 100%;
  border-radius: inherit;
  background: linear-gradient(90deg, #25f4ee 0%, #7c5cff 48%, #fe2c55 100%);
  box-shadow: 0 0 16px rgba(254, 44, 85, 0.28);
  transition: width 0.28s ease-out;
}

.progress-bar::after {
  content: '';
  position: absolute;
  inset: 0;
  background: linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.42), transparent);
  transform: translateX(-120%);
  animation: progress-shine 1.6s ease-in-out infinite;
}

.progress-bar.confirming {
  animation: progress-breathe 1.5s ease-in-out infinite;
}

/* 处理和发布阶段拿不到进度，用流动渐变表示仍在进行。 */
.progress-bar.indeterminate {
  width: 100%;
  background-size: 220% 100%;
  box-shadow: none;
  animation: progress-flow 1.5s linear infinite;
}

.progress-bar.indeterminate::after {
  display: none;
}

@keyframes progress-shine {
  0% {
    transform: translateX(-120%);
  }
  100% {
    transform: translateX(160%);
  }
}

@keyframes progress-breathe {
  0%,
  100% {
    filter: brightness(1);
  }
  50% {
    filter: brightness(1.18);
  }
}

@keyframes progress-flow {
  0% {
    background-position: 100% 0;
  }
  100% {
    background-position: -100% 0;
  }
}

.done-card {
  display: grid;
  gap: 8px;
}

.done-title {
  margin: 8px 0 0;
  font-size: 16px;
  font-weight: 600;
}

.audit-tip {
  margin: 0;
  font-size: 12px;
  color: rgba(var(--fg), 0.6);
}

.done-actions {
  margin-top: 8px;
}

@media (max-width: 900px) {
  .dropzone {
    grid-template-columns: 1fr;
  }

  .drop-copy {
    padding: 0 12px 12px;
  }

  .composer {
    grid-template-columns: 1fr;
  }

  .video {
    max-height: 280px;
  }

  .submit-row {
    justify-content: stretch;
  }

  .big-btn {
    width: 100%;
  }
}

@media (prefers-reduced-motion: reduce) {
  .progress-bar,
  .progress-bar::after,
  .progress-bar.confirming,
  .progress-bar.indeterminate {
    animation: none;
  }

  .progress-bar.indeterminate {
    width: 100%;
  }
}
</style>
