import { describe, expect, it, vi } from 'vitest'
vi.mock('../client', () => ({ apiClient: {}, buildApiUrl: (s: string) => s }))
import { emptyShot, initialChannel, type VideoInput } from '../videoStudio'
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
    const model = initialChannel().models[0]!
    expect(videoInputError(model, { ...input, duration: 1 })).toContain('4～30')
    expect(videoInputError(model, { ...input, videos: ['a', 'b'] })).toContain('最多 1')
    expect(videoInputError(model, { ...input, images: Array(10).fill('x') })).toContain('包含角色图和首帧')
    expect(videoInputError(model, { ...input, duration: 4.5 })).toContain('整数')
    expect(videoInputError(model, input)).toBe('')
  })
  it('rejects combined prompt overflow before quoting', () => {
    const shot = { ...emptyShot(), prompt: 'a'.repeat(6999), dialogue: 'hello' }
    expect(videoInputError(initialChannel().models[0], shotInput(input, shot, []))).toContain('合并后的提示词')
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
