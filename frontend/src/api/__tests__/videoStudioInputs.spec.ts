import { describe, expect, it, vi } from 'vitest'
vi.mock('../client', () => ({ apiClient: {}, buildApiUrl: (s: string) => s }))
import { emptyShot, type VideoInput } from '../videoStudio'
import { parseVideoProject, shotInput, videoInputError } from '../videoStudioInputs'
const input: VideoInput = { prompt: 'single', duration: 5, resolution: '768p', aspect_ratio: '16:9', images: ['single-image'], videos: ['single-video'], audios: ['single-audio'], generate_audio: false }
describe('storyboard request isolation', () => {
  it('isolates shot media and deduplicates character and first-frame images', () => {
    const shot = { ...emptyShot(), prompt: 'shot', first_frame: 'character', images: ['character', 'shot-image'], videos: ['shot-video'], audios: ['shot-audio'] }
    const result = shotInput(input, shot, ['character'])
    expect(result.images).toEqual(['character', 'shot-image'])
    expect(result.videos).toEqual(['shot-video'])
    expect(result.audios).toEqual(['shot-audio'])
    result.videos.push('another')
    expect(shot.videos).toEqual(['shot-video'])
    expect(shotInput(input, emptyShot(), []).videos).toEqual([])
  })
  it('checks combined references and per-model duration limits', () => {
    const model = { id: 'H3', label: 'H3', mode: 'second' as const, rates: { '768p': .22 }, min_seconds: 4, max_seconds: 30, max_images: 9, max_videos: 1, max_audios: 1, audio: true }
    expect(videoInputError(model, { ...input, duration: 1 })).toContain('4～30')
    expect(videoInputError(model, { ...input, videos: ['a', 'b'] })).toContain('最多 1')
    expect(videoInputError(model, { ...input, images: Array(10).fill('x') })).toContain('包含角色图和首帧')
    expect(videoInputError(model, { ...input, duration: 4.5 })).toContain('整数')
    expect(videoInputError(model, input)).toBe('')
  })
  it('rejects combined prompt overflow before quoting', () => {
    const shot = { ...emptyShot(), prompt: 'a'.repeat(6999), dialogue: 'hello' }
    expect(videoInputError({ id: 'H3', label: 'H3', mode: 'second' as const, rates: { '768p': .22 }, min_seconds: 4, max_seconds: 30, max_images: 9, max_videos: 1, max_audios: 1, audio: true }, shotInput(input, shot, []))).toContain('合并后的提示词')
  })
})
describe('project import', () => {
  const doc = () => ({ id: 'old', revision: 10, document: { title: 'story', outline: '', character_images: [], shots: [{ ...emptyShot(), prompt: 'scene', task_id: 'old-task' }] } })
  it('creates a fresh project and clears task identities while retaining media', () => {
    const original = doc(); original.document.shots[0]!.videos = ['vid']
    const result = parseVideoProject(JSON.stringify(original))
    expect(result.id).toBe('')
    expect(result.revision).toBe(0)
    expect(result.document.shots[0]?.id).not.toBe(original.document.shots[0]?.id)
    expect(result.document.shots[0]?.task_id).toBe('')
    expect(result.document.shots[0]?.videos).toEqual(['vid'])
  })
  it('accepts older exports with missing optional media arrays', () => {
    const original = doc(); delete original.document.shots[0]!.videos; delete original.document.shots[0]!.audios
    expect(parseVideoProject(JSON.stringify(original)).document.shots[0]?.audios).toEqual([])
  })
  it('rejects malformed projects before replacing the open project', () => {
    const original = doc()
    expect(() => parseVideoProject('{"document":{"shots":[]}}')).toThrow('格式')
    original.document.shots[0]!.duration = 1.5
    expect(() => parseVideoProject(JSON.stringify(original))).toThrow('整数')
    original.document.shots = Array(41).fill(emptyShot())
    expect(() => parseVideoProject(JSON.stringify(original))).toThrow('40')
  })
})

it('preserves output settings when importing a saved project', () => {
 const output = { key_id: 1, model: 'MiniMax-H3 2K', resolution: '2k', aspect_ratio: '9:16', generate_audio: true }
 const project = { document: { title: 'story', outline: '', character_images: [], shots: [emptyShot()], output } }
 expect(parseVideoProject(JSON.stringify(project)).document.output).toEqual(output)
})

it('retains explicit FACS off and adaptive ratio in imported scripts', () => {
 const output = { key_id: 1, model: 'seedance2.5-yx-stable', resolution: '720p', aspect_ratio: 'adaptive', generate_audio: true, facs: false }
 const project = { document: { title: 'story', outline: '', character_images: [], shots: [emptyShot()], output } }
 expect(parseVideoProject(JSON.stringify(project)).document.output).toEqual(output)
 expect(() => parseVideoProject(JSON.stringify({ ...project, document: { ...project.document, output: { ...output, facs: 'false' } } }))).toThrow('格式')
})

it('enforces each Movo model reference boundary and mixed H3 totals', async () => {
 const { videoReferenceError } = await import('../videoStudioInputs')
 const base = { id: 'test', label: 'test', mode: 'job' as const, rates: { '720p': 1 }, min_seconds: 1, max_seconds: 15, audio: true }
 for (const caps of [{ id: 'MiniMax-H3', max_images: 9, max_videos: 3, max_audios: 3, max_references: 12 }, { id: 'seedance2.5-yx-stable', max_images: 30, max_videos: 10, max_audios: 10 }, { id: 'seedance-2.0', max_images: 9, max_videos: 0, max_audios: 0 }]) {
  const model = { ...base, ...caps }
  for (const [field, limit] of [['images', caps.max_images], ['videos', caps.max_videos], ['audios', caps.max_audios]] as const) {
   const refs = { images: [] as string[], videos: [] as string[], audios: [] as string[], [field]: Array(limit).fill('a') }
   expect(videoReferenceError(model, refs)).toBe('')
   expect(videoReferenceError(model, { ...refs, [field]: [...refs[field], 'extra'] })).not.toBe('')
  }
 }
 const h3 = { ...base, max_images: 9, max_videos: 3, max_audios: 3, max_references: 12 }
 expect(videoReferenceError(h3, { images: Array(9).fill('i'), videos: Array(3).fill('v'), audios: [] })).toBe('')
 expect(videoReferenceError(h3, { images: Array(9).fill('i'), videos: Array(3).fill('v'), audios: ['a'] })).toContain('总数最多 12')
 const shot = { ...emptyShot(), first_frame: 'frame', images: Array.from({ length: 8 }, (_,i) => `image-${i}`) }
 expect(videoReferenceError(h3, shotInput(input, shot, ['character']))).toContain('最多 9 张')
 expect(videoReferenceError(h3, shotInput(input, { ...shot, first_frame: 'image-0' }, []))).toBe('')
})
