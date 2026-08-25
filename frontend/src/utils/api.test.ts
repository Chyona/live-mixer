import { describe, expect, it } from 'vitest';
import { apiPath } from './api';

describe('apiPath', () => {
  it('joins prefix and path', () => {
    expect(apiPath('/v1/user')).toMatch(/\/v1\/user$/);
  });
});
