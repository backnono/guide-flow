import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import {
  confirmQuestion as confirmQuestionApi,
  createLesson,
  getLesson,
  requestTutorial as requestTutorialApi,
  retryTutorial as retryTutorialApi,
  retryLesson as retryLessonApi,
  submitPractice,
} from '../services/lesson'
import type { LessonPlan, LessonResponse, LessonStatus, LessonStep, QuestionAnalysis, QuestionDiagram, ResearchContext } from '../types/lesson'

const DEMO_IMAGE = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII='
const EMPTY_STEP: LessonStep = { title: '', explanation: '', formula: '', visualNote: '', voiceText: '' }

export const useLessonStore = defineStore('lesson', () => {
  const lessonId = ref('')
  const status = ref<LessonStatus>('empty')
  const questionText = ref('')
  const currentStep = ref(0)
  const practiceAnswer = ref('')
  const practiceFeedback = ref('')
  const practiceCorrect = ref(false)
  const selectedFileName = ref('')
  const errorMessage = ref('')
  const shareNotice = ref('')
  const lessonPlan = ref<LessonPlan | null>(null)
  const questionTopic = ref('')
  const learnerGrade = ref(5)
  const questionDiagram = ref<QuestionDiagram | null>(null)
  const questionAnalysis = ref<QuestionAnalysis | null>(null)
  const researchContext = ref<ResearchContext | null>(null)
  const researchStatus = ref('not_started')
  const tutorialStatus = ref('not_started')
  const isSubmitting = ref(false)
  const isPolling = ref(false)
  const currentProgress = ref(0)
  let pollingToken = 0

  const isProcessing = computed(() => ['created', 'recognizing', 'structuring', 'researching', 'teaching', 'solving', 'planning', 'rendering'].includes(status.value))
  const isReady = computed(() => status.value === 'ready')
  const currentLessonStep = computed(() => lessonPlan.value?.steps[currentStep.value] ?? EMPTY_STEP)
  const lessonSteps = computed(() => lessonPlan.value?.steps ?? [])
  const practiceQuestion = computed(() => lessonPlan.value?.practice.question ?? '')
  const imageUrl = computed(() => lessonId.value ? `/api/lessons/${encodeURIComponent(lessonId.value)}/image` : '')
  const isDiagramQuestion = computed(() => questionTopic.value === 'geometry_numeric' || /图|矩形|三角形|四边形|垂直|平行|角|几何|证明/.test(questionText.value))
  // 辅助图没有资格替代原图；只有模型没有报告不确定项时才允许用户主动展开。
  const canShowAuxiliaryDiagram = computed(() => Boolean(questionDiagram.value?.figures?.length) && !(questionAnalysis.value?.uncertainItems?.length))

  function syncLesson(response: LessonResponse) {
    lessonId.value = response.lessonId
    // 后端使用 awaiting_confirmation，前端用 confirmation 统一渲染确认态。
    status.value = response.stage === 'confirmation' ? 'confirmation' : response.status
    currentProgress.value = response.progress
    learnerGrade.value = response.learnerProfile?.grade ?? learnerGrade.value
    if (response.questionDraft?.text) questionText.value = response.questionDraft.text
    if (response.questionAnalysis) questionAnalysis.value = response.questionAnalysis
    else if (response.status === 'structuring' || response.stage === 'structuring') questionAnalysis.value = null
    questionTopic.value = response.question?.topic ?? questionTopic.value
    researchContext.value = response.researchContext ?? null
    researchStatus.value = response.researchStatus
    tutorialStatus.value = response.tutorialStatus
    // 用户修改题干后，旧的辅助图不能继续留在页面上，避免把上一版结构误当成当前题面的事实。
    questionDiagram.value = response.question?.diagram ?? null
    if (response.lessonPlan) {
      lessonPlan.value = response.lessonPlan
      currentStep.value = Math.min(currentStep.value, Math.max(response.lessonPlan.steps.length - 1, 0))
    }
    if (response.error) errorMessage.value = response.error
    if (response.status !== 'failed') errorMessage.value = ''
    updateLessonUrl(response.lessonId)
  }

  function updateLessonUrl(id: string) {
    const url = new URL(window.location.href)
    if (id) url.searchParams.set('lessonId', id)
    else url.searchParams.delete('lessonId')
    window.history.replaceState({}, '', url)
  }

  async function startWithFile(file: File) {
    stopPolling()
    errorMessage.value = ''
    shareNotice.value = ''
    selectedFileName.value = file.name
    isSubmitting.value = true
    try {
      const response = await createLesson(file)
      syncLesson(response)
      startPollingIfNeeded()
    } catch (error) {
      errorMessage.value = getErrorMessage(error)
      status.value = 'failed'
    } finally {
      isSubmitting.value = false
    }
  }

  async function startSample() {
    const response = await fetch(DEMO_IMAGE)
    const blob = await response.blob()
    await startWithFile(new File([blob], 'guide-flow-demo.png', { type: 'image/png' }))
  }

  function selectFile(file: File | undefined) {
    if (file) void startWithFile(file)
  }

  function startPollingIfNeeded() {
    if (!lessonId.value || !isProcessing.value) return
    const token = ++pollingToken
    isPolling.value = true
    void poll(token)
  }

  async function poll(token: number) {
    if (!lessonId.value || token !== pollingToken) return
    try {
      const response = await getLesson(lessonId.value)
      // 旧轮询响应可能晚于重试请求返回；过期响应不得覆盖当前 Lesson 状态和错误信息。
      if (token !== pollingToken) return
      syncLesson(response)
      if (isProcessing.value && token === pollingToken) {
        window.setTimeout(() => void poll(token), 1500)
      } else {
        isPolling.value = false
      }
    } catch (error) {
      // 轮询失败不清空上一次成功状态，短暂网络波动后继续查询。
      errorMessage.value = getErrorMessage(error)
      if (token === pollingToken) window.setTimeout(() => void poll(token), 3000)
    }
  }

  function stopPolling() {
    pollingToken += 1
    isPolling.value = false
  }

  function openConfirmation() {
    status.value = 'confirmation'
    stopPolling()
  }

  async function confirmQuestion() {
    if (!lessonId.value || !questionText.value.trim() || isSubmitting.value) return
    isSubmitting.value = true
    errorMessage.value = ''
    try {
      const response = await confirmQuestionApi(lessonId.value, questionText.value.trim())
      syncLesson(response)
      startPollingIfNeeded()
    } catch (error) {
      errorMessage.value = getErrorMessage(error)
    } finally {
      isSubmitting.value = false
    }
  }

  function reparse() {
    // 含图形题不能通过修改 OCR 纯文本补齐图形关系，重新上传才会重新执行 OCR/图形分析。
    reset()
    window.setTimeout(() => document.getElementById('question-image')?.click(), 0)
  }

  async function retry() {
    if (!lessonId.value || isSubmitting.value) return
    isSubmitting.value = true
    errorMessage.value = ''
    try {
      const response = await retryLessonApi(lessonId.value)
      syncLesson(response)
      startPollingIfNeeded()
    } catch (error) {
      errorMessage.value = getErrorMessage(error)
    } finally {
      isSubmitting.value = false
    }
  }

  async function requestTutorial() {
    if (!lessonId.value || isSubmitting.value) return
    isSubmitting.value = true
    errorMessage.value = ''
    try {
      const response = await requestTutorialApi(lessonId.value)
      syncLesson(response)
      startPollingIfNeeded()
    } catch (error) {
      errorMessage.value = getErrorMessage(error)
    } finally {
      isSubmitting.value = false
    }
  }

  async function retryTutorial() {
    if (!lessonId.value || isSubmitting.value) return
    isSubmitting.value = true
    errorMessage.value = ''
    try {
      const response = await retryTutorialApi(lessonId.value)
      syncLesson(response)
      startPollingIfNeeded()
    } catch (error) {
      errorMessage.value = getErrorMessage(error)
    } finally {
      isSubmitting.value = false
    }
  }

  async function checkPractice() {
    if (!lessonId.value || isSubmitting.value) return
    isSubmitting.value = true
    try {
      const result = await submitPractice(lessonId.value, practiceAnswer.value)
      practiceCorrect.value = result.correct
      practiceFeedback.value = result.feedback
    } catch (error) {
      practiceFeedback.value = getErrorMessage(error)
      practiceCorrect.value = false
    } finally {
      isSubmitting.value = false
    }
  }

  function previousStep() {
    currentStep.value = Math.max(0, currentStep.value - 1)
  }

  function nextStep() {
    if (!lessonPlan.value) return
    currentStep.value = Math.min(lessonPlan.value.steps.length - 1, currentStep.value + 1)
  }

  function shareLesson() {
    shareNotice.value = '分享接口尚未接入；当前已完成真实讲解流程。'
  }

  async function restoreFromUrl() {
    const id = new URLSearchParams(window.location.search).get('lessonId')
    if (!id) return
    lessonId.value = id
    isPolling.value = true
    try {
      const response = await getLesson(id)
      syncLesson(response)
      startPollingIfNeeded()
    } catch (error) {
      errorMessage.value = getErrorMessage(error)
      reset()
    }
  }

  function reset() {
    stopPolling()
    lessonId.value = ''
    status.value = 'empty'
    questionText.value = ''
    currentStep.value = 0
    practiceAnswer.value = ''
    practiceFeedback.value = ''
    practiceCorrect.value = false
    selectedFileName.value = ''
    errorMessage.value = ''
    shareNotice.value = ''
    lessonPlan.value = null
    questionTopic.value = ''
    questionAnalysis.value = null
    researchContext.value = null
    researchStatus.value = 'not_started'
    tutorialStatus.value = 'not_started'
    questionDiagram.value = null
    learnerGrade.value = 5
    currentProgress.value = 0
    updateLessonUrl('')
  }

  function getErrorMessage(error: unknown): string {
    if (typeof error === 'object' && error !== null && 'message' in error) {
      return String((error as { message: string }).message)
    }
    return '服务暂时不可用，请稍后重试。'
  }

  return {
    lessonId,
    status,
    questionText,
    currentStep,
    practiceAnswer,
    practiceFeedback,
    practiceCorrect,
    selectedFileName,
    errorMessage,
    shareNotice,
    lessonPlan,
    isSubmitting,
    isPolling,
    isProcessing,
    isReady,
    currentLessonStep,
    lessonSteps,
    practiceQuestion,
    currentProgress,
    imageUrl,
    isDiagramQuestion,
    learnerGrade,
    questionDiagram,
    questionAnalysis,
    researchContext,
    researchStatus,
    tutorialStatus,
    canShowAuxiliaryDiagram,
    startSample,
    selectFile,
    openConfirmation,
    confirmQuestion,
    reparse,
    retry,
    requestTutorial,
    retryTutorial,
    reset,
    previousStep,
    nextStep,
    checkPractice,
    shareLesson,
    restoreFromUrl,
  }
})
