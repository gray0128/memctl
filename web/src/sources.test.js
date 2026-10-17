import { describe, expect, it } from 'vitest'
import { sourceProtocol } from './sources.js'

// docs/04-ui-and-cli-specification.md section 4: only an explicit safe
// protocol may navigate, agent:// offers copy, everything else stays text.

describe('sourceProtocol', () => {
  it('navigates only https references', () => {
    expect(sourceProtocol('https://example.com/memory')).toBe('https')
    expect(sourceProtocol('HTTPS://example.com')).toBe('https')
  })

  it('offers copy for agent references', () => {
    expect(sourceProtocol('agent://dev/claude/s1')).toBe('agent')
  })

  it('keeps every other value as plain text', () => {
    expect(sourceProtocol('javascript:alert(1)')).toBe('text')
    expect(sourceProtocol('data:text/html,x')).toBe('text')
    expect(sourceProtocol('http://example.com')).toBe('text')
    expect(sourceProtocol('')).toBe('text')
    expect(sourceProtocol(null)).toBe('text')
  })
})
