import { expect, it } from 'vitest'
import { read, utils, write } from 'xlsx'

it('preserves Chinese usage export text, numeric costs and literal formula-like strings', () => {
  const rows = [['模型', '费用', '提示词'], ['MiniMax-H3', 0.88, '=HYPERLINK("https://example.test")'], ['中文镜头', 2.2, '001']]
  const workbook = utils.book_new()
  utils.book_append_sheet(workbook, utils.aoa_to_sheet(rows), '使用记录')
  const bytes = write(workbook, { bookType: 'xlsx', type: 'array' })
  const restored = read(bytes, { type: 'array' })
  expect(utils.sheet_to_json(restored.Sheets['使用记录']!, { header: 1 })).toEqual(rows)
  expect(restored.Sheets['使用记录']!['C2']?.f).toBeUndefined()
})
