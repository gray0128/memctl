import { describe, expect, it } from 'vitest'
import {
  PAGE_SIZE,
  SOURCE_MISSING,
  dayKey,
  displayTimestamp,
  groupByDay,
  hasSource,
  pageNumbers,
  pageRequest,
  pagerState,
  sourceParts,
} from './audit.js'

describe('sourceParts', () => {
  it('splits the recorded trailers into labelled chips', () => {
    expect(sourceParts({ agent: 'dsh', device: 'MacBookPro.local', sessionId: 'sid-1' })).toEqual([
      { key: 'agent', label: 'Agent', value: 'dsh' },
      { key: 'device', label: '设备', value: 'MacBookPro.local' },
      { key: 'session', label: '会话', value: 'sid-1' },
    ])
  })

  it('omits missing trailers instead of inventing a value', () => {
    expect(sourceParts({ agent: 'memctl' })).toEqual([{ key: 'agent', label: 'Agent', value: 'memctl' }])
    expect(sourceParts({})).toEqual([])
    expect(sourceParts(null)).toEqual([])
    expect(hasSource({ agent: 'memctl' })).toBe(true)
    expect(hasSource({})).toBe(false)
    expect(SOURCE_MISSING).toBe('未记录来源')
  })
})

describe('displayTimestamp', () => {
  it('renders a commit timestamp in local time', () => {
    const local = new Date(2026, 9, 19, 17, 26)
    expect(displayTimestamp(local.toISOString())).toBe('2026-10-19 17:26')
  })
})

describe('dayKey and groupByDay', () => {
  it('keys a commit by its local calendar day', () => {
    const local = new Date(2026, 9, 19, 0, 5)
    expect(dayKey(local.toISOString())).toBe('2026-10-19')
    expect(dayKey('not-a-date')).toBe('not-a-date')
    expect(dayKey('')).toBe('')
  })

  it('groups only contiguous commits of the same day', () => {
    const first = new Date(2026, 9, 19, 17, 0).toISOString()
    const second = new Date(2026, 9, 19, 9, 0).toISOString()
    const previous = new Date(2026, 9, 18, 23, 0).toISOString()
    const groups = groupByDay([
      { hash: 'a', timestamp: first },
      { hash: 'b', timestamp: second },
      { hash: 'c', timestamp: previous },
    ])
    expect(groups.map((group) => group.key)).toEqual(['2026-10-19', '2026-10-18'])
    expect(groups[0].commits.map((commit) => commit.hash)).toEqual(['a', 'b'])
    expect(groupByDay([])).toEqual([])
  })
})

describe('cursor pager', () => {
  it('asks for page one without a cursor and later pages with the previous cursor', () => {
    expect(pageRequest(1, [])).toEqual({ limit: PAGE_SIZE })
    expect(pageRequest(2, [{ nextCursor: 'abc' }])).toEqual({ limit: PAGE_SIZE, before: 'abc' })
    expect(pageRequest(3, [{ nextCursor: 'abc' }, { nextCursor: 'def' }])).toEqual({
      limit: PAGE_SIZE,
      before: 'def',
    })
  })

  it('enables next only when the current page returned a cursor', () => {
    expect(pagerState([], 1)).toEqual({ hasPrev: false, hasNext: false, loaded: 0 })
    expect(pagerState([{ nextCursor: 'abc' }], 1)).toEqual({ hasPrev: false, hasNext: true, loaded: 1 })
    expect(pagerState([{ nextCursor: 'abc' }, { nextCursor: '' }], 2)).toEqual({
      hasPrev: true,
      hasNext: false,
      loaded: 2,
    })
  })

  it('numbers every visited page and the next one only', () => {
    expect(pageNumbers([], 1)).toEqual([1])
    expect(pageNumbers([{ nextCursor: 'a' }], 1)).toEqual([1])
    expect(pageNumbers([{ nextCursor: 'a' }, { nextCursor: 'b' }], 2)).toEqual([1, 2])
    expect(pageNumbers([{ nextCursor: 'a' }, { nextCursor: 'b' }], 3)).toEqual([1, 2, 3])
  })
})
