-- 回滚 account 表
DROP INDEX IF EXISTS idx_account_open_id;
DROP INDEX IF EXISTS idx_account_deleted_at;
DROP TABLE IF EXISTS account;
