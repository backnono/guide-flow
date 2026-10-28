<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useLessonStore } from '../../stores/lesson'
import QuestionDiagram from '../../components/lesson/QuestionDiagram.vue'

const lesson = useLessonStore()
const showAuxiliaryDiagram = ref(false)
const showResearchDetails = ref(false)

const stages = [
  { key: 'recognizing', label: '正在读题目' },
  { key: 'confirmation', label: '确认题目' },
  { key: 'structuring', label: '整理关系' },
  { key: 'researching', label: '查找参考' },
  { key: 'research_ready', label: '参考已找到' },
  { key: 'solving', label: '检查算式' },
  { key: 'planning', label: '准备讲法' },
  { key: 'ready', label: '可以开始讲' },
]

const processingCopy = computed(() => {
  const copies: Record<string, { title: string; description: string }> = {
    recognizing: { title: '正在读题目', description: '正在检查题目文字、数字和单位。' },
    structuring: { title: '正在整理题目', description: '正在找出已知条件和问题目标。' },
    researching: { title: '正在查找参考解答', description: '正在核对原题、图形关系和小问数量。' },
    teaching: { title: '正在生成个性化讲解', description: '正在把核对过的参考步骤改写成孩子听得懂的话。' },
    solving: { title: '正在检查算式', description: '正在验证计算过程和答案。' },
    planning: { title: '正在准备讲法', description: '正在把解法改成孩子听得懂的话。' },
  }
  return copies[lesson.status] ?? copies.recognizing
})

const currentStageIndex = computed(() => stages.findIndex((stage) => stage.key === lesson.status))
const topicLabel = computed(() => lesson.isDiagramQuestion ? '图形题' : '综合题')
const researchPreviewSources = computed(() => lesson.researchContext?.sources?.slice(0, 2) ?? [])
const remainingResearchSources = computed(() => Math.max((lesson.researchContext?.sources?.length ?? 0) - researchPreviewSources.value.length, 0))

onMounted(() => {
  void lesson.restoreFromUrl()
})

function speak() {
  if (!('speechSynthesis' in window)) {
    globalThis.alert('当前浏览器不支持朗读，但可以直接照着文字讲。')
    return
  }
  window.speechSynthesis.cancel()
  window.speechSynthesis.speak(new SpeechSynthesisUtterance(lesson.currentLessonStep.voiceText))
}

function toggleAuxiliaryDiagram() {
  showAuxiliaryDiagram.value = !showAuxiliaryDiagram.value
}
</script>

<template>
  <div class="app-shell">
    <header class="topbar">
      <a class="brand" href="#top" aria-label="讲给我听首页">
        <span class="brand-mark">算</span>
        <span>
          <strong>讲给我听</strong>
          <small>小学数学家庭辅导</small>
        </span>
      </a>
      <div class="topbar-actions">
        <span class="topbar-note"><i class="status-dot" />{{ lesson.isReady ? '讲解已准备好' : '陪你把“不会教”变成“会讲了”' }}</span>
        <button class="button button-quiet" type="button" @click="lesson.reset">新建题目</button>
      </div>
    </header>

    <main id="top" class="page-shell">
      <section class="hero">
        <div>
          <p class="eyebrow">家庭辅导 · Lesson 工作台</p>
          <h1>一道题，<em>慢慢讲</em><br />孩子就会了。</h1>
          <p class="hero-lede">先把题目读懂，再用孩子听得懂的方式，一步一步讲清楚。</p>
        </div>
        <div class="hero-stamp" aria-hidden="true">每一步<br />都值得<br />讲明白</div>
      </section>

      <section class="workspace" aria-label="题目辅导工作区">
        <aside class="card capture-card">
          <p class="card-kicker">STEP 01</p>
          <h2>把题目交给我</h2>
          <label class="upload-zone" for="question-image">
            <input id="question-image" type="file" accept="image/png,image/jpeg" @change="lesson.selectFile(($event.target as HTMLInputElement).files?.[0])" />
            <span class="upload-icon">＋</span>
            <strong>{{ lesson.selectedFileName || '上传题目照片' }}</strong>
            <small>支持 JPG、PNG，尽量拍清楚题干和图形</small>
          </label>
          <button class="sample-button" type="button" @click="lesson.startSample">
            <span class="sample-thumb">题</span>
            <span><strong>先试试示例题</strong><small>五年级 · 分数应用题</small></span>
            <span class="sample-arrow">→</span>
          </button>
          <p class="privacy-note">▣ 图片只用于本次讲解，不会公开展示</p>
        </aside>

        <section class="card lesson-card" aria-live="polite">
          <template v-if="lesson.status === 'empty'">
            <div class="empty-state">
              <p class="card-kicker">开始一道题</p>
              <h2>先把题目放到桌上</h2>
              <p>拍下作业，先看懂题目，再用孩子听得懂的方式，一步一步讲清楚。</p>
              <button class="button button-dark" type="button" @click="lesson.startSample">用示例题开始</button>
            </div>
          </template>

          <template v-else-if="lesson.status === 'confirmation'">
            <div class="card-heading">
              <div><p class="card-kicker">STEP 01 · 需要你看一眼</p><h2>请帮我确认一下题目</h2></div>
              <span class="status-pill warning">有些内容不太确定</span>
            </div>
            <div class="confirm-layout">
              <div class="photo-placeholder" aria-label="题目图片预览"><img v-if="lesson.imageUrl" :src="lesson.imageUrl" alt="用户上传的原题图片" /><span v-else>题<br />目<br />图</span></div>
              <div class="confirm-copy">
                <p>原图是后续讲解的主要依据。请重点检查题干、数字、单位和问题目标；确认后的内容会直接用于后面的讲解。</p>
                <textarea v-model="lesson.questionText" aria-label="确认题目文字" />
                <div v-if="lesson.questionAnalysis?.knowns?.length" class="question-facts">
                  <strong>已知条件</strong>
                  <span v-for="known in lesson.questionAnalysis.knowns" :key="`${known.name}-${known.value}`">{{ known.name }}：{{ known.value }}{{ known.unit || '' }}</span>
                </div>
                <div v-if="lesson.questionAnalysis?.asks?.length" class="question-facts">
                  <strong>问题目标</strong>
                  <span v-for="ask in lesson.questionAnalysis.asks" :key="`${ask.type}-${(ask.target || []).join(',')}`">{{ ask.type }}{{ ask.target?.length ? `：${ask.target.join('、')}` : '' }}</span>
                </div>
                <div v-if="lesson.questionAnalysis?.relations?.length" class="question-facts">
                  <strong>关键关系</strong>
                  <span v-for="(relation, index) in lesson.questionAnalysis.relations" :key="`${relation.type}-${index}`">
                    {{ relation.type }}{{ relation.points?.length ? `：${relation.points.join('、')}` : '' }}{{ relation.segments?.length ? `：${relation.segments.join('、')}` : '' }}{{ relation.lines?.length ? `：${relation.lines.join('、')}` : '' }}{{ relation.point ? `：${relation.point}` : '' }}{{ relation.target?.length ? `：${relation.target.join('、')}` : '' }}
                  </span>
                </div>
                <div v-if="lesson.questionAnalysis?.uncertainItems?.length" class="uncertainty-box">
                  <strong>还无法确认</strong>
                  <span v-for="item in lesson.questionAnalysis.uncertainItems" :key="item">{{ item }}</span>
                </div>
                <button v-if="lesson.canShowAuxiliaryDiagram" class="text-button" type="button" @click="toggleAuxiliaryDiagram">
                  {{ showAuxiliaryDiagram ? '隐藏辅助示意图' : '查看辅助示意图' }}
                </button>
                <QuestionDiagram v-if="showAuxiliaryDiagram && lesson.canShowAuxiliaryDiagram" :diagram="lesson.questionDiagram" />
                <div class="action-row">
                  <button class="button button-quiet" type="button" @click="lesson.reparse">{{ lesson.isDiagramQuestion ? '重新解析这道题' : '重新上传' }}</button>
                  <button class="button button-dark" type="button" :disabled="!lesson.questionText.trim()" @click="lesson.confirmQuestion">确认题目，开始解答</button>
                </div>
              </div>
            </div>
          </template>

          <template v-else-if="lesson.isProcessing">
            <div class="processing-state">
              <p class="card-kicker">任务处理中</p>
              <h2>{{ processingCopy.title }}</h2>
              <p>{{ processingCopy.description }}</p>
              <ol class="stage-list">
                <li v-for="(stage, index) in stages.slice(0, 5)" :key="stage.key" :class="{ done: index < currentStageIndex, current: index === currentStageIndex }">
                  <span>{{ index < currentStageIndex ? '✓' : index === currentStageIndex ? '·' : '○' }}</span>{{ stage.label }}
                </li>
              </ol>
              <p class="processing-hint">页面会自动查询处理结果，完成后会继续显示下一步。</p>
            </div>
          </template>

          <template v-else-if="lesson.status === 'research_ready'">
            <div class="card-heading lesson-heading">
              <div><p class="card-kicker">参考解答 · 已找到</p><h2>先看看标准步骤，再决定要不要讲解</h2></div>
              <span class="status-pill">外部参考，尚未个性化讲解</span>
            </div>
            <div class="question-paper"><small>已确认题目</small><p>{{ lesson.questionText }}</p></div>
            <div class="research-found-note">
              <span class="research-found-mark">✓</span>
              <div><strong>已找到可参考解答</strong><p>下一步可以生成结合这道题的智能讲解。</p></div>
            </div>
            <div v-if="lesson.researchContext?.referenceSolution?.steps?.length || lesson.researchContext?.referenceSolution?.finalAnswer" class="question-paper">
              <small>参考步骤</small>
              <ol><li v-for="step in lesson.researchContext.referenceSolution.steps" :key="step">{{ step }}</li></ol>
              <p v-if="lesson.researchContext.referenceSolution.finalAnswer"><strong>参考结论：</strong>{{ lesson.researchContext.referenceSolution.finalAnswer }}</p>
            </div>
            <div v-if="lesson.researchContext?.sources?.length" class="question-paper">
              <button class="research-details-toggle" type="button" :aria-expanded="showResearchDetails" @click="showResearchDetails = !showResearchDetails">
                <span>查看检索信息</span><small>已参考 {{ lesson.researchContext.sources.length }} 条网络资料</small><span class="research-chevron">{{ showResearchDetails ? '⌃' : '⌄' }}</span>
              </button>
              <div v-if="showResearchDetails" class="research-details">
                <p class="research-details-intro">搜索范围较广，下面展示少量参考片段；完整内容已用于生成讲解。</p>
                <p v-for="source in researchPreviewSources" :key="source.url" class="research-source-preview"><a :href="source.url" target="_blank" rel="noreferrer">{{ source.title }}</a><span>{{ source.summary }}</span></p>
                <p v-if="remainingResearchSources" class="research-details-more">还有 {{ remainingResearchSources }} 条候选资料未展开</p>
              </div>
            </div>
            <div class="action-row"><button class="button button-dark" type="button" :disabled="lesson.isSubmitting" @click="lesson.requestTutorial">{{ lesson.isSubmitting ? '正在准备' : '生成我的讲解' }}</button></div>
          </template>

          <template v-else-if="lesson.status === 'research_unavailable'">
            <div class="processing-state">
              <p class="card-kicker">参考解答</p><h2>未找到可靠参考解答</h2>
              <p>这道题暂时没有通过核对的外部资料，因此没有直接调用模型猜答案。</p>
              <div class="action-row"><button class="button button-dark" type="button" :disabled="lesson.isSubmitting" @click="lesson.retry">重新检索</button><button class="button button-quiet" type="button" @click="lesson.reset">重新上传</button></div>
            </div>
          </template>

          <template v-else-if="lesson.status === 'failed'">
            <div class="processing-state">
              <p class="card-kicker">{{ lesson.tutorialStatus === 'failed' ? '参考解答 · 已找到' : '这次没有完成' }}</p>
              <h2>{{ lesson.tutorialStatus === 'failed' ? '参考解答已找到，讲解需要重试' : '我没能把这道题整理好' }}</h2>
              <p>{{ lesson.errorMessage || '可能是照片中的数字或题目文字不够清楚。你可以再试一次，也可以换一张更清楚的照片。' }}</p>
              <div v-if="lesson.tutorialStatus === 'failed'" class="research-found-note compact"><span class="research-found-mark">✓</span><div><strong>搜索结果已保留</strong><p>不会重新搜索，重试只重新生成个性化讲解。</p></div></div>
              <div v-else class="error-notice">错误类型：题目识别未完成<br />建议：让题干、数字和单位完整出现在照片中。</div>
              <div class="action-row"><button class="button button-dark" type="button" :disabled="lesson.isSubmitting" @click="lesson.tutorialStatus === 'failed' ? lesson.retryTutorial() : lesson.retry()">{{ lesson.isSubmitting ? '正在重试' : (lesson.tutorialStatus === 'failed' ? '重试讲解' : '再试一次') }}</button><button class="button button-quiet" type="button" @click="lesson.reset">重新上传</button></div>
            </div>
          </template>

          <template v-else-if="lesson.status === 'rejected'">
            <div class="processing-state">
              <p class="card-kicker">题目已结束</p>
              <h2>这道题没有继续处理</h2>
              <p>你可以重新上传一道题，开始新的讲解。</p>
              <button class="button button-dark" type="button" @click="lesson.reset">重新上传</button>
            </div>
          </template>

          <template v-else>
            <div class="card-heading lesson-heading">
              <div><p class="card-kicker">STEP 02 · 讲解中</p><h2>{{ lesson.currentLessonStep.title }}</h2></div>
              <span class="status-pill">识别结果已保留原图</span>
            </div>
            <div class="question-paper"><small>题目 · 已确认</small><p>{{ lesson.questionText }}</p><button v-if="!lesson.isDiagramQuestion" class="edit-link" type="button" @click="lesson.openConfirmation">修改文字</button></div>
            <div v-if="lesson.imageUrl" class="source-preview"><img :src="lesson.imageUrl" alt="原题图片" /><span>原题图片</span></div>
            <div v-if="lesson.canShowAuxiliaryDiagram" class="auxiliary-diagram-control">
              <button class="text-button" type="button" @click="toggleAuxiliaryDiagram">
                {{ showAuxiliaryDiagram ? '隐藏辅助示意图' : '查看辅助示意图' }}
              </button>
              <QuestionDiagram v-if="showAuxiliaryDiagram" :diagram="lesson.questionDiagram" />
            </div>
            <div class="chips"><span class="chip">{{ topicLabel }}</span><span class="chip muted">{{ lesson.learnerGrade }} 年级</span></div>
            <div class="divider"><span>今天我们只解决一个小问题</span></div>
            <div class="step-view">
              <div class="step-number">0{{ lesson.currentStep + 1 }}<small> / {{ String(lesson.lessonSteps.length).padStart(2, '0') }}</small></div>
              <div class="step-copy"><h3>{{ lesson.currentLessonStep.title }}</h3><p>{{ lesson.currentLessonStep.explanation }}</p><div class="formula">{{ lesson.currentLessonStep.formula }}</div></div>
              <div class="visual-stage">
                <template v-if="lesson.currentLessonStep.visual?.type === 'ratio_bar'"><div class="book-bar"><span class="bar-read" /><span class="bar-left" /></div><div class="bar-caption"><span>已完成部分</span><span>剩余部分</span></div></template>
                <img v-else-if="lesson.isDiagramQuestion && lesson.imageUrl" class="step-source-image" :src="lesson.imageUrl" alt="讲解所依据的原题图形" />
                <div v-else class="generic-visual">{{ lesson.currentLessonStep.formula }}</div>
                <div class="stage-note">{{ lesson.currentLessonStep.visualNote || '这一步只使用当前题目已经验证的关系。' }}</div>
              </div>
            </div>
            <div class="lesson-controls">
              <div class="action-row"><button class="button button-sound" type="button" @click="speak">◖ 听我讲</button><button v-if="lesson.isReady" class="button button-quiet" type="button" @click="lesson.shareLesson">分享讲解</button></div>
              <div class="pager"><button type="button" aria-label="上一步" :disabled="lesson.currentStep === 0" @click="lesson.previousStep">←</button><span v-for="(_, index) in lesson.lessonSteps" :key="index" :class="['dot', { active: index === lesson.currentStep }]" /><button type="button" aria-label="下一步" :disabled="lesson.currentStep === lesson.lessonSteps.length - 1" @click="lesson.nextStep">→</button></div>
            </div>
            <p v-if="lesson.shareNotice" class="share-notice">{{ lesson.shareNotice }}</p>
          </template>
        </section>
        <p v-if="lesson.errorMessage && lesson.status !== 'failed'" class="global-error">{{ lesson.errorMessage }}</p>
      </section>

      <section v-if="lesson.isReady && lesson.practiceQuestion" class="card practice-card">
        <div><p class="card-kicker">STEP 03 · 巩固一下</p><h2>换一道相似的，你来试试</h2><p>{{ lesson.practiceQuestion }}</p></div>
        <div class="practice-form"><label for="practice-answer">我的答案</label><div class="answer-line"><input id="practice-answer" v-model="lesson.practiceAnswer" inputmode="numeric" placeholder="写数字" /><span>支</span><button class="button button-dark" type="button" @click="lesson.checkPractice">检查一下</button></div><p v-if="lesson.practiceFeedback" :class="['feedback', lesson.practiceFeedback.startsWith('太好了') ? 'success' : 'danger']">{{ lesson.practiceFeedback }}</p></div>
      </section>
    </main>
  </div>
</template>
