import { describe, expect, it } from 'vitest'
import { buildQuery } from './queries.js'

describe('buildQuery', () => {
  it('omits empty and whitespace-only filters', () => {
    expect(
      buildQuery({ q: 'runtime', agent: '', device: '   ', file: 'alpha.md', from: undefined, to: null }),
    ).toBe('?q=runtime&file=alpha.md')
  })

  it('encodes every value and returns an empty string without filters', () => {
    expect(buildQuery({ q: 'a b&c' })).toBe('?q=a+b%26c')
    expect(buildQuery({})).toBe('')
  })
})
