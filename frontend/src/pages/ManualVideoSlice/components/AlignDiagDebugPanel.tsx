import type { AlignDiagSnapshot } from '~/services/sourceVideo';

function shortUrl(url: string): string {
  const u = url.trim();
  if (!u) return '(empty)';
  if (u.length <= 72) return u;
  return `${u.slice(0, 36)}…${u.slice(-28)}`;
}

function fmtSec(sec: number): string {
  if (!Number.isFinite(sec)) return '-';
  return sec.toFixed(3);
}

export interface AlignDiagActiveAsr {
  start: number;
  end: number;
  text: string;
}

export interface AlignDiagDebugPanelProps {
  playUrl: string;
  liveUrl: string;
  mediaDurationSec: number;
  currentTime: number;
  activeAsr: AlignDiagActiveAsr | null;
  alignDiag?: AlignDiagSnapshot | null;
}

/** 仅 ?isdebug=true 显示：对照预览播放轴与当前 ASR 句时间，便于截屏/对照后端 align_diag.jsonl */
const AlignDiagDebugPanel = ({
  playUrl,
  liveUrl,
  mediaDurationSec,
  currentTime,
  activeAsr,
  alignDiag,
}: AlignDiagDebugPanelProps) => {
  const samePlayLive =
    alignDiag?.same_play_and_live ??
    (playUrl.trim() !== '' &&
      liveUrl.trim() !== '' &&
      playUrl.split('?')[0] === liveUrl.split('?')[0]);

  const drift =
    activeAsr != null && Number.isFinite(activeAsr.start)
      ? currentTime - activeAsr.start
      : null;

  return (
    <div
      className="align-diag-debug-panel"
      style={{
        marginTop: 8,
        padding: '8px 10px',
        fontSize: 11,
        lineHeight: 1.45,
        fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
        background: 'rgba(0,0,0,0.72)',
        color: '#e8e8e8',
        borderRadius: 4,
        maxHeight: 160,
        overflow: 'auto',
      }}
    >
      <div style={{ fontWeight: 600, marginBottom: 4 }}>align-diag (isdebug)</div>
      <div>play_url: {shortUrl(playUrl)}</div>
      <div>live_url: {shortUrl(liveUrl)}</div>
      <div>
        same_play_live: {String(samePlayLive)}
        {alignDiag?.hint ? ` · hint: ${alignDiag.hint}` : ''}
      </div>
      <div>
        media.duration={fmtSec(mediaDurationSec)}s · currentTime={fmtSec(currentTime)}s
        {alignDiag
          ? ` · windows=${alignDiag.window_count} · master_ready_ms=${alignDiag.master_ready_ms} · asr_cursor_ms=${alignDiag.asr_cursor_ms}`
          : ''}
      </div>
      <div>
        active_asr:{' '}
        {activeAsr
          ? `[${fmtSec(activeAsr.start)}–${fmtSec(activeAsr.end)}] ${activeAsr.text.slice(0, 48)}${
              activeAsr.text.length > 48 ? '…' : ''
            }`
          : '(none)'}
      </div>
      <div>
        drift(current−asrStart): {drift == null ? '-' : `${drift >= 0 ? '+' : ''}${fmtSec(drift)}s`}
      </div>
    </div>
  );
};

export default AlignDiagDebugPanel;
