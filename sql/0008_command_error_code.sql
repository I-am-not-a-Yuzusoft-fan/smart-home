-- 记录被拒/超时命令的错误码，重放同一 requestId 时返回与首次一致的结果。
ALTER TABLE command_log ADD COLUMN IF NOT EXISTS error_code text;
