import { describe, expect, it } from 'vitest';
import { appConfig, isLeftNavLayout, isTopNavLayout } from './config';

describe('appConfig', () => {
  it('provides default title', () => {
    expect(appConfig.title).toBeTruthy();
  });

  it('uses a valid nav layout', () => {
    expect(isLeftNavLayout || isTopNavLayout).toBe(true);
  });
});
