import { describe, expect, it } from 'vitest'
import { encodeFilePath, ifMatchHeader, isAbsent, putFileHeaders } from './writes.js'

// This file verifies the request shapes of the managed write routes: path
// wildcard encoding and the If-Match / If-None-Match preconditions of
// docs/02-domain-models-and-contracts.md section 4.1.

describe('encodeFilePath', () => {
  it('escapes each segment but keeps the / separators', () => {
    expect(encodeFilePath('notes/alpha beta.md')).toBe('notes/alpha%20beta.md')
    expect(encodeFilePath('a/b/c.md')).toBe('a/b/c.md')
    expect(encodeFilePath('dir with space/file#1.md')).toBe('dir%20with%20space/file%231.md')
  })
})

describe('isAbsent', () => {
  it('treats the absent literal and an empty value as a creation baseline', () => {
    expect(isAbsent('absent')).toBe(true)
    expect(isAbsent('')).toBe(true)
    expect(isAbsent(null)).toBe(true)
    expect(isAbsent('sha256:abc')).toBe(false)
  })
})

describe('ifMatchHeader', () => {
  it('quotes the file revision', () => {
    expect(ifMatchHeader('sha256:abc')).toEqual({ 'If-Match': '"sha256:abc"' })
  })
})

describe('putFileHeaders', () => {
  it('creates with If-None-Match and updates with If-Match', () => {
    expect(putFileHeaders('absent')).toEqual({ 'If-None-Match': '*' })
    expect(putFileHeaders('sha256:abc')).toEqual({ 'If-Match': '"sha256:abc"' })
  })
})
