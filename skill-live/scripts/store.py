"""本地 SQLite 业务数据（对应客户端 MediaRepository）。

保留与客户端一致的核心表：
- media_sources：源视频；
- recording_sessions：录制 / 下载会话；
- asr_results：ASR 转写缓存（source_id + 指纹）；
- clip_drafts：一键成片生成的剪映草稿记录。

技能以命令行方式运行，不提供界面，但这些表让「转写缓存复用」「草稿列表」
「按需下载」与客户端行为一致。
"""

from __future__ import annotations

import json
import os
import sqlite3
from typing import Any, Dict, List, Optional

from util import current_ms, directory_size, hls_playlist_duration_ms


# 与 LiveMix/src/data/MediaRepository.cpp 的 schema v4 保持一致，
# 便于把 --db 指向客户端数据库（或让客户端读取技能生成的数据库）。
_SCHEMA = """
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS media_sources (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('online','local')),
    source_uri TEXT NOT NULL,
    local_path TEXT NOT NULL DEFAULT '',
    note TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('recording','ready','failed')),
    duration_ms INTEGER NOT NULL DEFAULT 0,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    probe_json TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    created_at_ms INTEGER NOT NULL,
    updated_at_ms INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS recording_sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id INTEGER NOT NULL REFERENCES media_sources(id) ON DELETE CASCADE,
    output_path TEXT NOT NULL,
    container TEXT NOT NULL DEFAULT 'hls',
    status TEXT NOT NULL CHECK (status IN ('recording','completed','failed','cancelled')),
    started_at_ms INTEGER NOT NULL,
    finished_at_ms INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_media_sources_status_created
    ON media_sources(status, created_at_ms DESC);
CREATE INDEX IF NOT EXISTS idx_recording_sessions_source
    ON recording_sessions(source_id, status);

CREATE TABLE IF NOT EXISTS asr_results (
    source_id INTEGER PRIMARY KEY REFERENCES media_sources(id) ON DELETE CASCADE,
    fingerprint TEXT NOT NULL,
    data_json TEXT NOT NULL,
    created_at_ms INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS clip_drafts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id INTEGER NOT NULL REFERENCES media_sources(id) ON DELETE CASCADE,
    draft_id TEXT NOT NULL,
    draft_url TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    tags_json TEXT NOT NULL DEFAULT '[]',
    source_start_ms INTEGER NOT NULL DEFAULT 0,
    source_end_ms INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    created_at_ms INTEGER NOT NULL,
    downloaded_path TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_clip_drafts_source
    ON clip_drafts(source_id, created_at_ms);

CREATE TABLE IF NOT EXISTS clip_tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id INTEGER NOT NULL REFERENCES media_sources(id) ON DELETE CASCADE,
    title TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('running','completed','failed')),
    step INTEGER NOT NULL DEFAULT 0,
    progress INTEGER NOT NULL DEFAULT 0,
    message TEXT NOT NULL DEFAULT '',
    draft_count INTEGER NOT NULL DEFAULT 0,
    created_at_ms INTEGER NOT NULL,
    updated_at_ms INTEGER NOT NULL,
    finished_at_ms INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_clip_tasks_created
    ON clip_tasks(created_at_ms DESC);

CREATE TABLE IF NOT EXISTS prompts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL,
    uses INTEGER NOT NULL DEFAULT 0,
    created_at_ms INTEGER NOT NULL,
    updated_at_ms INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_prompts_updated
    ON prompts(updated_at_ms DESC);
"""


class Store:
    def __init__(self, path: str) -> None:
        self.path = os.path.abspath(os.path.expanduser(path))
        os.makedirs(os.path.dirname(self.path), exist_ok=True)
        self._db = sqlite3.connect(self.path, timeout=10)
        self._db.row_factory = sqlite3.Row
        self._db.executescript(_SCHEMA)
        self._db.execute("PRAGMA user_version = 4")
        self._db.commit()

    def close(self) -> None:
        try:
            self._db.close()
        except sqlite3.Error:
            pass

    def __enter__(self) -> "Store":
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()

    # ------------------------------------------------------------------
    # media_sources
    # ------------------------------------------------------------------
    def add_source(
        self,
        *,
        title: str,
        source_kind: str,
        source_uri: str,
        local_path: str,
        note: str = "",
        status: str = "ready",
        duration_ms: int = 0,
        size_bytes: int = 0,
        probe_json: str = "",
        error_message: str = "",
    ) -> int:
        now = current_ms()
        cursor = self._db.execute(
            """
            INSERT INTO media_sources
                (title, source_kind, source_uri, local_path, note, status,
                 duration_ms, size_bytes, probe_json, error_message,
                 created_at_ms, updated_at_ms)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (title, source_kind, source_uri, local_path, note, status,
             int(duration_ms), int(size_bytes), probe_json, error_message, now, now),
        )
        self._db.commit()
        return int(cursor.lastrowid)

    def update_source(self, source_id: int, **fields: Any) -> None:
        allowed = {
            "title", "source_kind", "source_uri", "local_path", "note", "status",
            "duration_ms", "size_bytes", "probe_json", "error_message",
        }
        updates = {key: value for key, value in fields.items() if key in allowed}
        if not updates:
            return
        updates["updated_at_ms"] = current_ms()
        assignments = ", ".join(f"{key} = ?" for key in updates)
        values = list(updates.values()) + [source_id]
        self._db.execute(
            f"UPDATE media_sources SET {assignments} WHERE id = ?", values
        )
        self._db.commit()

    def source(self, source_id: int) -> Optional[Dict[str, Any]]:
        row = self._db.execute(
            "SELECT * FROM media_sources WHERE id = ?", (source_id,)
        ).fetchone()
        return dict(row) if row else None

    def list_sources(self, status: str = "") -> List[Dict[str, Any]]:
        if status:
            rows = self._db.execute(
                "SELECT * FROM media_sources WHERE status = ? "
                "ORDER BY created_at_ms DESC, id DESC",
                (status,),
            ).fetchall()
        else:
            rows = self._db.execute(
                "SELECT * FROM media_sources "
                "ORDER BY created_at_ms DESC, id DESC"
            ).fetchall()
        return [dict(row) for row in rows]

    def source_by_path(self, local_path: str) -> Optional[Dict[str, Any]]:
        path = os.path.abspath(os.path.expanduser(local_path))
        row = self._db.execute(
            "SELECT * FROM media_sources WHERE local_path = ? "
            "ORDER BY id DESC LIMIT 1",
            (path,),
        ).fetchone()
        return dict(row) if row else None

    def ensure_source_for_input(
        self,
        local_path: str,
        title: str,
        *,
        note: str = "",
        duration_ms: int = 0,
        size_bytes: int = 0,
        probe_json: str = "",
    ) -> int:
        path = os.path.abspath(os.path.expanduser(local_path))
        existing = self.source_by_path(path)
        if existing and existing.get("status") == "ready":
            if note and not str(existing.get("note", "")).strip():
                self.update_source(int(existing["id"]), note=note)
            return int(existing["id"])
        if existing:
            self.update_source(
                int(existing["id"]), status="ready", note=note,
                duration_ms=duration_ms, size_bytes=size_bytes,
                probe_json=probe_json, error_message="",
            )
            return int(existing["id"])
        return self.add_source(
            title=title, source_kind="local", source_uri=path,
            local_path=path, note=note, status="ready",
            duration_ms=duration_ms, size_bytes=size_bytes,
            probe_json=probe_json,
        )

    # ------------------------------------------------------------------
    # recording_sessions
    # ------------------------------------------------------------------
    def create_recording_source(
        self,
        *,
        title: str,
        source_uri: str,
        output_path: str,
        started_at_ms: int,
        container: str,
        source_kind: str = "online",
        local_path: str = "",
    ) -> tuple:
        now = current_ms()
        source_id = self.add_source(
            title=title,
            source_kind=source_kind,
            source_uri=source_uri,
            local_path=local_path or output_path,
            status="recording",
        )
        cursor = self._db.execute(
            """
            INSERT INTO recording_sessions
                (source_id, output_path, container, status, started_at_ms)
            VALUES (?, ?, ?, 'recording', ?)
            """,
            (source_id, output_path, container, started_at_ms),
        )
        self._db.commit()
        return source_id, int(cursor.lastrowid)

    def finish_recording(
        self,
        source_id: int,
        session_id: int,
        *,
        success: bool,
        local_path: str,
        duration_ms: int,
        size_bytes: int,
        error_message: str = "",
    ) -> None:
        now = current_ms()
        session_status = "completed" if success else "failed"
        source_status = "ready" if success else "failed"
        self._db.execute(
            """
            UPDATE recording_sessions
               SET status = ?, finished_at_ms = ?, duration_ms = ?,
                   size_bytes = ?, error_message = ?
             WHERE id = ? AND source_id = ?
            """,
            (session_status, now, duration_ms, size_bytes, error_message,
             session_id, source_id),
        )
        self._db.execute(
            """
            UPDATE media_sources
               SET status = ?, local_path = ?, duration_ms = ?, size_bytes = ?,
                   error_message = ?, updated_at_ms = ?
             WHERE id = ?
            """,
            (source_status, local_path, duration_ms, size_bytes,
             error_message, now, source_id),
        )
        self._db.commit()

    def recover_interrupted_recordings(self) -> List[Dict[str, Any]]:
        """把上次进程异常退出留下的 recording 状态修正为可用/失败。

        与客户端启动时的恢复逻辑一致：已有播放列表的部分录制保留为 ready，
        并附带警告；没有可用文件则标记 failed。技能以单进程顺序使用为前提，
        不要在同一数据库上并发运行 record 与 clip。
        """
        rows = self._db.execute(
            "SELECT * FROM media_sources WHERE status = 'recording'"
        ).fetchall()
        recovered: List[Dict[str, Any]] = []
        for row in rows:
            source = dict(row)
            source_id = int(source["id"])
            path = str(source.get("local_path", ""))
            exists = bool(path) and os.path.isfile(path) and os.path.getsize(path) > 0
            warning = ""
            if exists and path.lower().endswith(".m3u8"):
                duration_ms = hls_playlist_duration_ms(path)
                size_bytes = directory_size(os.path.dirname(path))
                warning = "上次录制中断，已保留可恢复的录像。"
                status = "ready"
            elif exists:
                duration_ms = 0
                size_bytes = os.path.getsize(path)
                warning = "上次操作中断，已保留可用文件。"
                status = "ready"
            else:
                duration_ms = 0
                size_bytes = 0
                warning = "上次操作中断，且没有可用文件。"
                status = "failed"

            now = current_ms()
            self._db.execute(
                """
                UPDATE recording_sessions
                   SET status = 'failed', finished_at_ms = ?,
                       error_message = CASE
                           WHEN error_message = '' THEN ? ELSE error_message END
                 WHERE source_id = ? AND status = 'recording'
                """,
                (now, warning, source_id),
            )
            self._db.execute(
                """
                UPDATE media_sources
                   SET status = ?, duration_ms = ?, size_bytes = ?,
                       error_message = ?, updated_at_ms = ?
                 WHERE id = ?
                """,
                (status, duration_ms, size_bytes, warning, now, source_id),
            )
            recovered.append({
                "source_id": source_id,
                "title": source.get("title", ""),
                "status": status,
                "local_path": path,
                "warning": warning,
            })
        if recovered:
            self._db.commit()
        return recovered

    # ------------------------------------------------------------------
    # asr_results
    # ------------------------------------------------------------------
    def load_asr(self, source_id: int, fingerprint: str) -> Optional[Dict[str, Any]]:
        row = self._db.execute(
            "SELECT fingerprint, data_json FROM asr_results WHERE source_id = ?",
            (source_id,),
        ).fetchone()
        if not row or row["fingerprint"] != fingerprint:
            return None
        try:
            payload = json.loads(row["data_json"])
        except ValueError:
            return None
        if not isinstance(payload, dict):
            return None
        return payload

    def save_asr(self, source_id: int, fingerprint: str,
                 transcript: Dict[str, Any]) -> None:
        payload = json.dumps(transcript, ensure_ascii=False)
        self._db.execute(
            """
            INSERT INTO asr_results (source_id, fingerprint, data_json, created_at_ms)
            VALUES (?, ?, ?, ?)
            ON CONFLICT(source_id) DO UPDATE SET
                fingerprint = excluded.fingerprint,
                data_json = excluded.data_json,
                created_at_ms = excluded.created_at_ms
            """,
            (source_id, fingerprint, payload, current_ms()),
        )
        self._db.commit()

    # ------------------------------------------------------------------
    # clip_drafts
    # ------------------------------------------------------------------
    def add_draft(self, record: Dict[str, Any]) -> int:
        tags = record.get("tags") or []
        cursor = self._db.execute(
            """
            INSERT INTO clip_drafts
                (source_id, draft_id, draft_url, title, description, tags_json,
                 source_start_ms, source_end_ms, duration_ms, created_at_ms)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                int(record.get("source_id", -1)),
                str(record.get("draft_id", "")),
                str(record.get("draft_url", "")),
                str(record.get("title", "")),
                str(record.get("description", "")),
                json.dumps(tags, ensure_ascii=False),
                int(record.get("source_start_ms", 0)),
                int(record.get("source_end_ms", 0)),
                int(record.get("duration_ms", 0)),
                int(record.get("created_at_ms", current_ms())),
            ),
        )
        self._db.commit()
        return int(cursor.lastrowid)

    def list_drafts(self, source_id: Optional[int] = None) -> List[Dict[str, Any]]:
        if source_id is None:
            rows = self._db.execute(
                "SELECT * FROM clip_drafts ORDER BY created_at_ms DESC, id DESC"
            ).fetchall()
        else:
            rows = self._db.execute(
                "SELECT * FROM clip_drafts WHERE source_id = ? "
                "ORDER BY created_at_ms DESC, id DESC",
                (source_id,),
            ).fetchall()
        drafts = []
        for row in rows:
            item = dict(row)
            try:
                item["tags"] = json.loads(item.pop("tags_json", "[]") or "[]")
            except ValueError:
                item["tags"] = []
            drafts.append(item)
        return drafts

    def draft(self, draft_row_id: int) -> Optional[Dict[str, Any]]:
        rows = [
            item for item in self.list_drafts()
            if int(item.get("id", -1)) == int(draft_row_id)
        ]
        return rows[0] if rows else None

    def mark_draft_downloaded(self, draft_row_id: int, local_path: str) -> None:
        self._db.execute(
            "UPDATE clip_drafts SET downloaded_path = ? WHERE id = ?",
            (local_path, draft_row_id),
        )
        self._db.commit()
