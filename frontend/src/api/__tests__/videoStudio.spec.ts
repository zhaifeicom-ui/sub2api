import { describe, expect, it, vi } from 'vitest'
vi.mock('../client', () => ({ apiClient: {}, buildApiUrl: (s: string) => s }))
import { estimateVideo, initialChannel, type VideoModel } from '../videoStudio'
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

it('prices conditional sound/reference tariffs with minimum and stepped seconds', () => {
  const m = initialChannel().models[0]!
  m.rules = [{ resolution: '768p', min_seconds: 4, max_seconds: 30, audio: 'off', reference: 'any', mode: 'second', rate: 2, minimum_seconds: 6, step_seconds: 4, base_fee: 1, image_fee: .5 }]
  const input = { prompt: 'test', duration: 5, resolution: '768p', aspect_ratio: '16:9', images: ['a', 'b'], videos: [], audios: [], generate_audio: false }
  expect(estimateVideo(m, '768p', 5, input)).toBe(18)
  expect(estimateVideo(m, '768p', 5, { ...input, generate_audio: true })).toBe(0)
})
