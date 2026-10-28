export type LessonStatus =
  | 'empty'
  | 'recognizing'
  | 'awaiting_confirmation'
  | 'confirmation'
  | 'structuring'
  | 'solving'
  | 'planning'
  | 'rendering'
  | 'ready'
  | 'research_ready'
  | 'research_unavailable'
  | 'rejected'
  | 'failed'

export interface LessonStep {
  title: string
  explanation: string
  formula: string
  visualNote: string
  voiceText: string
  visual?: { type: string; segments?: number[] }
}

export interface LessonPlan {
  title: string
  steps: LessonStep[]
  finalAnswer: string
  commonMistakes?: string[]
  practice: {
    question: string
    answer: string
    checkExpression?: string
  }
}

export interface QuestionDraft {
  text: string
  confidence: number
  fieldConfidence?: Record<string, number>
}

export interface QuestionKnown {
  name: string
  value: string
  unit?: string
  confidence?: number
}

export interface QuestionAsk {
  type: string
  target?: string[]
  confidence?: number
}

export interface QuestionRelation {
  type: string
  points?: string[]
  segments?: string[]
  lines?: string[]
  point?: string
  target?: string[]
}

export interface QuestionAnalysis {
  normalizedText?: string
  knowns?: QuestionKnown[]
  asks?: QuestionAsk[]
  relations?: QuestionRelation[]
  uncertainItems?: string[]
  confidence?: number
}

export interface ResearchSource {
  title: string
  url: string
  summary: string
  trustScore: number
}

export interface ResearchContext {
  query: string
  status: string
  sources: ResearchSource[]
  matchedQuestion?: { title: string; matchScore: number; matchedSubquestionCount: number; checks: string[] }
  referenceSolution?: { status: string; steps: string[]; finalAnswer: string; uncertainties: string[] }
  durationMs?: number
  errorCode?: string
}

export interface DiagramFigure {
  id: string
  title?: string
  points?: string[]
  segments?: string[]
  curves?: DiagramCurve[]
  intersections?: DiagramIntersection[]
  collinear?: string[][]
  coordinates?: Record<string, { x: string; y: string }>
  shape?: string
  constraints?: DiagramConstraint[]
  layoutHints?: DiagramLayoutHints
  relations?: DiagramRelation[]
}

export interface DiagramCurve {
  type: string
  endpoints?: string[]
  center?: string
  sweep?: 'upper' | 'lower'
}

export interface DiagramConstraint {
  type: string
  points?: string[]
  point?: string
  lines?: string[]
  segments?: string[]
  segment?: string
  operator?: string
}

export interface DiagramLayoutHints {
  aspectRatio?: number
  orientation?: string
  anchors?: Record<string, { x: number; y: number }>
}

export interface DiagramIntersection {
  point: string
  of?: string[]
}

export interface DiagramRelation {
  type: string
  target?: string
  points?: string[]
  lines?: string[]
  segments?: string[]
}

export interface QuestionDiagram {
  figures?: DiagramFigure[]
  entities?: { id: string; type: string }[]
  relations?: { type: string; target?: string; points?: string[] }[]
}

export interface LessonResponse {
  lessonId: string
  status: Exclude<LessonStatus, 'empty'>
  stage: string
  progress: number
  nextAction: 'wait' | 'confirm_question' | 'retry' | 'practice' | 'create_lesson'
  questionDraft?: QuestionDraft
  questionAnalysis?: QuestionAnalysis
  researchContext?: ResearchContext
  researchStatus: string
  tutorialStatus: string
  lessonPlan?: LessonPlan
  question?: { topic?: string; tags?: string[]; diagram?: QuestionDiagram }
  learnerProfile: { grade: number }
  imageName?: string
  error?: string
  errorCode?: string
  failureKind?: string
  retryCount: number
}

export interface PracticeResult {
  attempt: number
  answer: string
  correct: boolean
  feedback: string
  expectedAnswer: string
}
