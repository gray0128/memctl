import { describe, expect, it } from 'vitest'
import { formatTimestamp } from './time.js'

describe('formatTimestamp', () => {
  it('renders an ISO instant in local time', () => {
    const local = new Date(2026, 9, 17, 17, 39)
    expect(formatTimestamp(local.toISOString())).toBe('2026-10-17 17:39')
  })

  it('pads month, day, hour and minute', () => {
    const local = new Date(2026, 0, 2, 3, 4)
    expect(formatTimestamp(local.toISOString())).toBe('2026-01-02 03:04')
  })

  it('returns 未知 without a value and keeps an unparsable value', () => {
    expect(formatTimestamp('')).toBe('未知')
    expect(formatTimestamp(undefined)).toBe('未知')
    expect(formatTimestamp('not-a-date')).toBe('not-a-date')
  })
})
