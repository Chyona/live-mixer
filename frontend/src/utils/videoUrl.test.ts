import { describe, expect, it } from 'vitest';
import { resolveVideoCrossOrigin } from './videoUrl';

describe('resolveVideoCrossOrigin', () => {
  const origin = 'https://app.example.com';

  it('同源相对路径不设置 crossOrigin', () => {
    expect(resolveVideoCrossOrigin('/tos-media/video.mp4', origin)).toBe('');
  });

  it('跨域 https 视频使用 anonymous', () => {
    expect(
      resolveVideoCrossOrigin('https://bucket.tos-cn-shanghai.volces.com/a.mp4', origin)
    ).toBe('anonymous');
  });

  it('blob 与 data 不设置 crossOrigin', () => {
    expect(resolveVideoCrossOrigin('blob:https://app.example.com/uuid', origin)).toBe('');
    expect(resolveVideoCrossOrigin('data:video/mp4;base64,abc', origin)).toBe('');
  });
});
