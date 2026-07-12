-- 回滚 live-mixer 全部表（按外键依赖逆序）
DROP TABLE IF EXISTS task;
DROP TABLE IF EXISTS llm_system_prompt;
DROP TABLE IF EXISTS video_project;
DROP TABLE IF EXISTS live_material;

DROP INDEX IF EXISTS idx_account_open_id;
DROP TABLE IF EXISTS account;
