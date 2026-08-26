import { describe, expect, it, afterEach } from 'vitest';
import {
  appendDebugAsrKeyToPath,
  getAsrParagraphsApiKey,
  mergeDebugAsrKeySearchParams,
  resetDebugSearchPersistForTests,
  syncDebugSearchPersistFromUrl,
} from './asrParagraphsKey';

function setSearch(search: string) {
  window.history.replaceState({}, '', search ? `/${search}` : '/');
}

describe('getAsrParagraphsApiKey', () => {
  afterEach(() => {
    setSearch('');
    resetDebugSearchPersistForTests();
  });

  it('defaults to asr_paragraphs when URL has no override', () => {
    setSearch('');
    expect(getAsrParagraphsApiKey()).toBe('asr_paragraphs');
  });

  it('reads live_asr when isdebug=true', () => {
    setSearch('?isdebug=true');
    expect(getAsrParagraphsApiKey()).toBe('live_asr');
  });

  it('falls back to default after isdebug is removed from URL', () => {
    setSearch('?isdebug=true');
    expect(getAsrParagraphsApiKey()).toBe('live_asr');
    setSearch('');
    expect(getAsrParagraphsApiKey()).toBe('asr_paragraphs');
  });
});

describe('mergeDebugAsrKeySearchParams', () => {
  afterEach(() => {
    setSearch('');
    resetDebugSearchPersistForTests();
  });

  it('copies isdebug from current URL into navigation search params', () => {
    setSearch('?isdebug=true');

    const merged = mergeDebugAsrKeySearchParams(new URLSearchParams({ projectId: '9' }));
    expect(merged.get('isdebug')).toBe('true');
    expect(merged.get('projectId')).toBe('9');
  });

  it('keeps isdebug after URL cleared when sticky persist was enabled', () => {
    syncDebugSearchPersistFromUrl('?isdebug=true');
    setSearch('');

    const merged = mergeDebugAsrKeySearchParams(new URLSearchParams({ projectId: '1' }));
    expect(merged.get('isdebug')).toBe('true');
  });

  it('stops merging after isdebug=false', () => {
    syncDebugSearchPersistFromUrl('?isdebug=true');
    syncDebugSearchPersistFromUrl('?isdebug=false');

    const merged = mergeDebugAsrKeySearchParams(new URLSearchParams({ projectId: '1' }));
    expect(merged.get('isdebug')).toBeNull();
  });

  it('appends isdebug to path built without query', () => {
    setSearch('?isdebug=true');

    expect(appendDebugAsrKeyToPath('/tasks')).toBe('/tasks?isdebug=true');
    expect(appendDebugAsrKeyToPath('/videos-manual-slice/1?projectId=2')).toBe(
      '/videos-manual-slice/1?projectId=2&isdebug=true'
    );
  });

  it('does not overwrite isdebug already present in target search', () => {
    setSearch('?isdebug=true');

    const merged = mergeDebugAsrKeySearchParams(
      new URLSearchParams({ isdebug: 'false', projectId: '1' })
    );
    expect(merged.get('isdebug')).toBe('false');
  });
});
