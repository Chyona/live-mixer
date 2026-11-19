import { describe, expect, it } from 'vitest';
import {
  getVideoErrorMessage,
  isSameMediaResource,
  LIVE_PREVIEW_DECODE_MESSAGE,
  mediaResourceKey,
  resolveVideoCrossOrigin,
} from './videoUrl';

function mediaError(code: number): MediaError {
  return { code } as MediaError;
}

function ensureMediaError() {
  if (typeof MediaError !== 'undefined') return;
  Object.defineProperty(globalThis, 'MediaError', {
    value: {
      MEDIA_ERR_ABORTED: 1,
      MEDIA_ERR_NETWORK: 2,
      MEDIA_ERR_DECODE: 3,
      MEDIA_ERR_SRC_NOT_SUPPORTED: 4,
    },
  });
}

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

describe('mediaResourceKey', () => {
  it('忽略对象存储签名 query', () => {
    const a =
      'https://bucket.cos.ap-guangzhou.myqcloud.com/video_editing/live-record/uuid/live.m3u8?q-sign-algorithm=sha1&q-sign-time=1';
    const b =
      'https://bucket.cos.ap-guangzhou.myqcloud.com/video_editing/live-record/uuid/live.m3u8?q-sign-algorithm=sha1&q-sign-time=2';
    expect(mediaResourceKey(a)).toBe(mediaResourceKey(b));
    expect(isSameMediaResource(a, b)).toBe(true);
  });

  it('不同对象不算同一资源', () => {
    expect(
      isSameMediaResource(
        'https://cdn.example/live.m3u8?sig=1',
        'https://cdn.example/other.m3u8?sig=1'
      )
    ).toBe(false);
  });
});

describe('getVideoErrorMessage', () => {
  ensureMediaError();

  it('解码失败默认仍提示文件可能损坏', () => {
    expect(getVideoErrorMessage(mediaError(3))).toBe('视频解码失败，文件可能已损坏');
  });

  it('传入跟播文案时只替换解码失败', () => {
    expect(getVideoErrorMessage(mediaError(3), LIVE_PREVIEW_DECODE_MESSAGE)).toBe(
      LIVE_PREVIEW_DECODE_MESSAGE
    );
    expect(getVideoErrorMessage(mediaError(2), LIVE_PREVIEW_DECODE_MESSAGE)).toBe(
      '视频网络加载失败，请检查网络或播放地址是否过期'
    );
  });

  it('空白跟播文案回退到默认解码提示', () => {
    expect(getVideoErrorMessage(mediaError(3), '  ')).toBe('视频解码失败，文件可能已损坏');
  });
});
