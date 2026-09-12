import { describe, expect, it } from 'vitest'
import {
  createEmptyPricingEntry,
  pricingEntryFromAPI,
  pricingEntryToAPI,
  replacePlatformPricing,
} from '../modelPricing'

describe('model pricing form conversion', () => {
  it('round-trips token prices using $/MTok in the form', () => {
    const form = pricingEntryFromAPI({
      platform: 'openai',
      models: ['gpt-5'],
      billing_mode: 'token',
      input_price: 1.25e-6,
      output_price: 1e-5,
      cache_write_price: null,
      cache_read_price: 1.25e-7,
      image_input_price: null,
      image_output_price: null,
      per_request_price: null,
      intervals: [],
      time_pricing: null,
    })

    expect(form.input_price).toBe(1.25)
    expect(form.output_price).toBe(10)
    expect(form.cache_read_price).toBe(0.125)

    const api = pricingEntryToAPI(form, 'openai')
    expect(api.input_price).toBe(1.25e-6)
    expect(api.output_price).toBe(1e-5)
    expect(api.cache_read_price).toBe(1.25e-7)
  })

  it('drops token intervals and time pricing for group overrides', () => {
    const form = createEmptyPricingEntry()
    form.models = ['claude-*']
    form.intervals = [{
      min_tokens: 0,
      max_tokens: null,
      tier_label: '',
      input_price: 2,
      output_price: null,
      cache_write_price: null,
      cache_write_1h_price: null,
      cache_read_price: null,
      input_multiplier: null,
      output_multiplier: null,
      cache_write_multiplier: null,
      cache_read_multiplier: null,
      per_request_price: null,
      sort_order: 0,
    }]
    form.time_pricing.periods = [{ start_time: '00:00:00', end_time: '06:00:00', multiplier: 0.5 }]

    const api = pricingEntryToAPI(form, 'anthropic', { groupOverride: true })
    expect(api.intervals).toEqual([])
    expect(api.time_pricing).toBeNull()
  })

  it('replaces one platform without overwriting other platform pricing', () => {
    const anthropicForm = createEmptyPricingEntry()
    anthropicForm.models = ['claude-sonnet-4']
    const openAIForm = createEmptyPricingEntry()
    openAIForm.models = ['gpt-5']
    const replacementForm = createEmptyPricingEntry()
    replacementForm.models = ['gpt-5.1']

    const result = replacePlatformPricing(
      [
        pricingEntryToAPI(anthropicForm, 'anthropic'),
        pricingEntryToAPI(openAIForm, 'openai'),
      ],
      'openai',
      [pricingEntryToAPI(replacementForm, 'openai')],
    )

    expect(result.map(entry => entry.models[0])).toEqual(['claude-sonnet-4', 'gpt-5.1'])
  })
})
