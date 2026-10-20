import { describe, expect, it, vi } from 'vitest'
vi.mock('../client', () => ({ apiClient: {}, buildApiUrl: (s: string) => s }))
import { estimateVideo, type VideoModel } from '../videoStudio'
const model: VideoModel = { id: 'H3', label: 'H3', mode: 'second', rates: { '768p': .22, '2k': .35 }, min_seconds: 4, max_seconds: 30, max_images: 9, max_videos: 1, max_audios: 1, audio: true }
describe('video quote preview', () => {
  it('charges by seconds and resolution without a hidden group multiplier', () => {
    expect(estimateVideo(model, '768p', 4)).toBe(.88)
    expect(estimateVideo(model, '2k', 30)).toBe(10.5)
  })
  it('keeps flat-job price constant and rejects unsupported settings', () => {
    const job = { ...model, mode: 'job' as const, rates: { '2k': 2.2 }, max_seconds: 15 }
    expect(estimateVideo(job, '2k', 4)).toBe(2.2)
    expect(estimateVideo(job, '2k', 15)).toBe(2.2)
    expect(estimateVideo(job, '2k', 16)).toBe(0)
    expect(estimateVideo(job, '768p', 10)).toBe(0)
  })
})
