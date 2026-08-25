import { describe, expect, it } from 'vitest';
import {
  closeLogin,
  isSafeInternalPath,
  openLogin,
  sanitizeReturnPath,
} from './loginFlow';
import { getLoginModalStore } from '~/components/LoginModal/store';
import { DEFAULT_APP_PATH } from '~/routes/const';

describe('isSafeInternalPath', () => {
  it('allows same-origin relative paths', () => {
    expect(isSafeInternalPath('/')).toBe(true);
    expect(isSafeInternalPath('/source-videos')).toBe(true);
    expect(isSafeInternalPath('/videos-slice/1?tab=a#sec')).toBe(true);
  });

  it('rejects open-redirect payloads', () => {
    expect(isSafeInternalPath('')).toBe(false);
    expect(isSafeInternalPath('https://evil.com')).toBe(false);
    expect(isSafeInternalPath('//evil.com')).toBe(false);
    expect(isSafeInternalPath('/\\evil.com')).toBe(false);
    expect(isSafeInternalPath('/%2F%2Fevil.com')).toBe(false);
  });
});

describe('sanitizeReturnPath', () => {
  it('returns fallback for unsafe paths', () => {
    expect(sanitizeReturnPath('//evil.com')).toBe(DEFAULT_APP_PATH);
    expect(sanitizeReturnPath('/source-videos')).toBe('/source-videos');
  });
});

describe('loginFlow', () => {
  it('closeLogin resets modal store', () => {
    const store = getLoginModalStore();
    store.open = true;
    store.returnTo = '/home';

    closeLogin();

    expect(store.open).toBe(false);
    expect(store.returnTo).toBeNull();
  });

  it('openLogin is callable', () => {
    expect(() => openLogin({ pathname: '/home', search: '', hash: '' })).not.toThrow();
  });
});
