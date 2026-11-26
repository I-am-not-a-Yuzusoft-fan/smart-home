-- 0009_run_step_note.sql —— 为 run_step 增加可读依据文案列（如动态温控调节依据）
ALTER TABLE run_step ADD COLUMN IF NOT EXISTS note text;
COMMENT ON COLUMN run_step.note IS '步骤执行的可读说明/策略调节依据文案（区别于失败错误 error）';
