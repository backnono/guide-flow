import { createRequestId, request } from './api'
import type { LessonResponse, PracticeResult } from '../types/lesson'

export async function createLesson(file: File, grade = 5): Promise<LessonResponse> {
  const body = new FormData()
  body.append('image', file)
  body.append('learnerProfile', JSON.stringify({ grade }))
  return request<LessonResponse>('/lessons', {
    method: 'POST',
    body,
    headers: { 'Idempotency-Key': createRequestId() },
  })
}

export function getLesson(id: string): Promise<LessonResponse> {
  return request<LessonResponse>(`/lessons/${encodeURIComponent(id)}`)
}

export function confirmQuestion(id: string, text: string): Promise<LessonResponse> {
  return request<LessonResponse>(`/lessons/${encodeURIComponent(id)}/question/confirm`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ text }),
  })
}

export function rejectQuestion(id: string): Promise<LessonResponse> {
  return request<LessonResponse>(`/lessons/${encodeURIComponent(id)}/question/reject`, { method: 'POST' })
}

export function retryLesson(id: string): Promise<LessonResponse> {
  return request<LessonResponse>(`/lessons/${encodeURIComponent(id)}/retry`, { method: 'POST' })
}

export function requestTutorial(id: string): Promise<LessonResponse> {
  return request<LessonResponse>(`/lessons/${encodeURIComponent(id)}/tutorial`, { method: 'POST' })
}

export function retryTutorial(id: string): Promise<LessonResponse> {
  return request<LessonResponse>(`/lessons/${encodeURIComponent(id)}/tutorial/retry`, { method: 'POST' })
}

export function submitPractice(id: string, answer: string): Promise<PracticeResult> {
  return request<PracticeResult>(`/lessons/${encodeURIComponent(id)}/practice`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ answer }),
  })
}
