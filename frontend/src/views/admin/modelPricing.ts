import type { ChannelModelPricing } from '@/api/admin/channels'
import type { PricingFormEntry } from '@/components/admin/channel/types'
import {
  apiIntervalsToForm,
  apiTimePricingToForm,
  createDefaultTimePricingForm,
  formIntervalsToAPI,
  formTimePricingToAPI,
  mTokToPerToken,
  perTokenToMTok,
  toNullableNumber,
} from '@/components/admin/channel/types'

export const pricingPlatforms = [
  'anthropic',
  'openai',
  'gemini',
  'antigravity',
  'grok',
  'kimi',
  'zhipu',
  'deepseek',
] as const

export function createEmptyPricingEntry(): PricingFormEntry {
  return {
    models: [],
    billing_mode: 'token',
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_write_1h_price: null,
    cache_read_price: null,
    fast_multiplier: null,
    flex_multiplier: null,
    image_input_price: null,
    image_output_price: null,
    per_request_price: null,
    intervals: [],
    time_pricing: createDefaultTimePricingForm(),
  }
}

export function pricingEntryFromAPI(entry: ChannelModelPricing): PricingFormEntry {
  return {
    models: [...(entry.models || [])],
    billing_mode: entry.billing_mode || 'token',
    input_price: perTokenToMTok(entry.input_price),
    output_price: perTokenToMTok(entry.output_price),
    cache_write_price: perTokenToMTok(entry.cache_write_price),
    cache_write_1h_price: perTokenToMTok(entry.cache_write_1h_price),
    cache_read_price: perTokenToMTok(entry.cache_read_price),
    fast_multiplier: entry.fast_multiplier ?? null,
    flex_multiplier: entry.flex_multiplier ?? null,
    image_input_price: perTokenToMTok(entry.image_input_price),
    image_output_price: perTokenToMTok(entry.image_output_price),
    per_request_price: entry.per_request_price,
    intervals: apiIntervalsToForm(entry.intervals || []),
    time_pricing: apiTimePricingToForm(entry.time_pricing),
  }
}

export function pricingEntryToAPI(
  entry: PricingFormEntry,
  platform: string,
  options: { groupOverride?: boolean } = {},
): ChannelModelPricing {
  const groupOverride = options.groupOverride === true
  return {
    platform,
    models: [...entry.models],
    billing_mode: entry.billing_mode,
    input_price: mTokToPerToken(entry.input_price),
    output_price: mTokToPerToken(entry.output_price),
    cache_write_price: mTokToPerToken(entry.cache_write_price),
    cache_write_1h_price: mTokToPerToken(entry.cache_write_1h_price),
    cache_read_price: mTokToPerToken(entry.cache_read_price),
    fast_multiplier: toNullableNumber(entry.fast_multiplier),
    flex_multiplier: toNullableNumber(entry.flex_multiplier),
    image_input_price: mTokToPerToken(entry.image_input_price),
    image_output_price: mTokToPerToken(entry.image_output_price),
    per_request_price: toNullableNumber(entry.per_request_price),
    intervals:
      groupOverride && entry.billing_mode === 'token'
        ? []
        : formIntervalsToAPI(entry.intervals || []),
    time_pricing: groupOverride ? null : formTimePricingToAPI(entry.time_pricing),
  }
}

export function replacePlatformPricing(
  existing: ChannelModelPricing[],
  platform: string,
  replacement: ChannelModelPricing[],
  defaultPlatform = 'anthropic',
): ChannelModelPricing[] {
  return [
    ...(existing || []).filter(entry => (entry.platform || defaultPlatform) !== platform),
    ...replacement,
  ]
}

export function formatTokenPrice(value: number | null | undefined): string {
  if (value == null) return '—'
  const perMTok = perTokenToMTok(value)
  if (perMTok == null) return '—'
  return `$${perMTok.toLocaleString(undefined, { maximumFractionDigits: 8 })}`
}
