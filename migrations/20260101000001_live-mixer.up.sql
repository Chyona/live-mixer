-- live-mixer 数据库初始化（创新部署，无需兼容历史版本）

-- 账号表
CREATE TABLE IF NOT EXISTS account (
    id          BIGSERIAL PRIMARY KEY,
    username    VARCHAR(64)  NOT NULL UNIQUE,
    email       VARCHAR(128) NOT NULL UNIQUE,
    password    VARCHAR(255) NOT NULL,
    nickname    VARCHAR(64),
    avatar      VARCHAR(1024),
    roles       VARCHAR(64),
    open_id     VARCHAR(128),
    remark      VARCHAR(256),
    phone       VARCHAR(32),
    is_active   SMALLINT     NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    ext         VARCHAR(1024),
    CONSTRAINT chk_account_is_active CHECK (is_active IN (0, 1))
);

COMMENT ON TABLE account IS '账号表';
COMMENT ON COLUMN account.id IS '主键';
COMMENT ON COLUMN account.username IS '用户名（唯一）';
COMMENT ON COLUMN account.email IS '邮箱（唯一）';
COMMENT ON COLUMN account.password IS '登录密码（加密存储）';
COMMENT ON COLUMN account.nickname IS '昵称';
COMMENT ON COLUMN account.avatar IS '用户头像 URL';
COMMENT ON COLUMN account.roles IS '用户角色，多个角色用逗号分隔';
COMMENT ON COLUMN account.open_id IS '第三方授权 OpenId';
COMMENT ON COLUMN account.remark IS '备注';
COMMENT ON COLUMN account.phone IS '手机号码';
COMMENT ON COLUMN account.is_active IS '是否启用：0否 1是';
COMMENT ON COLUMN account.created_at IS '创建时间';
COMMENT ON COLUMN account.updated_at IS '更新时间';
COMMENT ON COLUMN account.ext IS '扩展字段';

CREATE INDEX IF NOT EXISTS idx_account_open_id ON account (open_id);

-- 直播素材表：一条记录对应一场直播及其 ASR 结果
CREATE TABLE IF NOT EXISTS live_material (
    id           BIGSERIAL PRIMARY KEY,
    name         VARCHAR(64) NOT NULL,
    remark       VARCHAR(256),
    live_url     VARCHAR(1024) NOT NULL,
    live_asr        JSONB         NOT NULL DEFAULT '{}',
    duration        BIGINT        NOT NULL DEFAULT 0,
    asr_status      VARCHAR(20)   NOT NULL DEFAULT 'pending',
    asr_progress    SMALLINT      NOT NULL DEFAULT 0,
    asr_error_msg   TEXT,
    asr_started_at  TIMESTAMPTZ,
    asr_updated_at  TIMESTAMPTZ,
    created_by      BIGINT        NOT NULL REFERENCES account (id),
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    ext             VARCHAR(1024),
    CONSTRAINT chk_live_material_asr_progress CHECK (asr_progress BETWEEN 0 AND 100),
    CONSTRAINT chk_live_material_asr_status CHECK (asr_status IN ('pending', 'processing', 'completed', 'failed'))
);

COMMENT ON TABLE live_material IS '直播素材表';
COMMENT ON COLUMN live_material.id IS '主键';
COMMENT ON COLUMN live_material.name IS '素材名称';
COMMENT ON COLUMN live_material.remark IS '备注';
COMMENT ON COLUMN live_material.live_url IS '直播链接';
COMMENT ON COLUMN live_material.live_asr IS '直播视频 ASR 识别结果（JSON），默认为空对象';
COMMENT ON COLUMN live_material.duration IS '直播时长（毫秒）';
COMMENT ON COLUMN live_material.asr_status IS 'ASR 识别状态：pending待处理 processing识别中 completed已完成 failed失败';
COMMENT ON COLUMN live_material.asr_progress IS 'ASR 识别进度（0-100）';
COMMENT ON COLUMN live_material.asr_error_msg IS 'ASR 识别失败原因';
COMMENT ON COLUMN live_material.asr_started_at IS 'ASR 识别开始时间';
COMMENT ON COLUMN live_material.asr_updated_at IS 'ASR 识别状态最后更新时间（用于检测长时间卡死任务）';
COMMENT ON COLUMN live_material.created_by IS '添加人（账号 ID）';
COMMENT ON COLUMN live_material.created_at IS '添加时间';
COMMENT ON COLUMN live_material.updated_at IS '最后更新时间';
COMMENT ON COLUMN live_material.ext IS '扩展字段';

CREATE INDEX IF NOT EXISTS idx_live_material_created_by ON live_material (created_by);
CREATE INDEX IF NOT EXISTS idx_live_material_asr_status ON live_material (asr_status);

-- 剪辑项目表
CREATE TABLE IF NOT EXISTS video_project (
    id              BIGSERIAL PRIMARY KEY,
    name            VARCHAR(64),
    remark          VARCHAR(256),
    live_id         BIGINT      NOT NULL REFERENCES live_material (id),
    -- 提示词 ID（对应 llm_system_prompt.id），不设外键，便于灵活配置；默认 1
    prompt_id       BIGINT      NOT NULL DEFAULT 1,
    clips0          JSONB       NOT NULL DEFAULT '[]',
    clips1          JSONB       NOT NULL DEFAULT '[]',
    draft_url       VARCHAR(1024),
    video_url       VARCHAR(1024),
    created_by      BIGINT      NOT NULL REFERENCES account (id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ext             VARCHAR(1024)
);

COMMENT ON TABLE video_project IS '剪辑项目表';
COMMENT ON COLUMN video_project.id IS '主键';
COMMENT ON COLUMN video_project.name IS '项目名称';
COMMENT ON COLUMN video_project.remark IS '备注';
COMMENT ON COLUMN video_project.live_id IS '关联直播素材 ID（live_material.id）';
COMMENT ON COLUMN video_project.prompt_id IS '提示词 ID（llm_system_prompt.id），无外键约束，默认 1';
COMMENT ON COLUMN video_project.clips0 IS '视频切片列表（毫秒），格式：[{"start_time":0,"end_time":10}]';
COMMENT ON COLUMN video_project.clips1 IS '带文本与词级时间戳的切片列表（毫秒），格式：[{"text":"...","start_time":0,"end_time":10,"words":[{"text":"...","start_time":0,"end_time":160}]}]';
COMMENT ON COLUMN video_project.draft_url IS '剪映草稿 URL';
COMMENT ON COLUMN video_project.video_url IS '视频地址 URL';
COMMENT ON COLUMN video_project.created_by IS '创建人（账号 ID）';
COMMENT ON COLUMN video_project.created_at IS '创建时间';
COMMENT ON COLUMN video_project.updated_at IS '最后编辑时间';
COMMENT ON COLUMN video_project.ext IS '扩展字段';

CREATE INDEX IF NOT EXISTS idx_video_project_created_by ON video_project (created_by);
CREATE INDEX IF NOT EXISTS idx_video_project_live_id ON video_project (live_id);
CREATE INDEX IF NOT EXISTS idx_video_project_prompt_id ON video_project (prompt_id);

-- 大模型系统提示词管理表
CREATE TABLE IF NOT EXISTS llm_system_prompt (
    id           BIGSERIAL PRIMARY KEY,
    name         VARCHAR(128) NOT NULL,
    content      TEXT         NOT NULL,
    remark       VARCHAR(256),
    is_editable  SMALLINT     NOT NULL DEFAULT 1,
    created_by   BIGINT       NOT NULL REFERENCES account (id),
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    ext          VARCHAR(1024),
    CONSTRAINT chk_llm_system_prompt_editable CHECK (is_editable IN (0, 1))
);

COMMENT ON TABLE llm_system_prompt IS '大模型系统提示词管理表';
COMMENT ON COLUMN llm_system_prompt.id IS '主键';
COMMENT ON COLUMN llm_system_prompt.name IS '提示词名称';
COMMENT ON COLUMN llm_system_prompt.content IS '提示词内容';
COMMENT ON COLUMN llm_system_prompt.remark IS '备注';
COMMENT ON COLUMN llm_system_prompt.is_editable IS '是否允许修改：0否 1是';
COMMENT ON COLUMN llm_system_prompt.created_by IS '创建人（账号 ID）';
COMMENT ON COLUMN llm_system_prompt.created_at IS '创建时间';
COMMENT ON COLUMN llm_system_prompt.updated_at IS '更新时间';
COMMENT ON COLUMN llm_system_prompt.ext IS '扩展字段';

CREATE INDEX IF NOT EXISTS idx_llm_system_prompt_created_by ON llm_system_prompt (created_by);

-- 任务表：异步任务统一入口，通过 type 区分三类业务
CREATE TABLE IF NOT EXISTS task (
    id                     BIGSERIAL PRIMARY KEY,
    type                   VARCHAR(32) NOT NULL,
    status                 VARCHAR(32) NOT NULL DEFAULT 'pending',
    progress               SMALLINT    NOT NULL DEFAULT 0,
    sys_prompt             TEXT,
    usr_prompt             TEXT,
    error_message          TEXT,
    created_by             BIGINT      NOT NULL REFERENCES account (id),
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at             TIMESTAMPTZ,
    completed_at           TIMESTAMPTZ,
    ext                    VARCHAR(1024),
    CONSTRAINT chk_task_type CHECK (type IN ('ai_slice', 'draft', 'ai_slice_draft')),
    CONSTRAINT chk_task_status CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    CONSTRAINT chk_task_progress CHECK (progress BETWEEN 0 AND 100)
);

COMMENT ON TABLE task IS '任务表';
COMMENT ON COLUMN task.id IS '主键';
COMMENT ON COLUMN task.type IS '任务类型：ai_slice AI切片 draft 剪映草稿 ai_slice_draft AI切片+剪映草稿';
COMMENT ON COLUMN task.status IS '任务状态：pending待处理 processing执行中 completed已完成 failed失败';
COMMENT ON COLUMN task.progress IS '任务进度（0-100），供客户端轮询展示';
COMMENT ON COLUMN task.sys_prompt IS '系统提示词';
COMMENT ON COLUMN task.usr_prompt IS '用户提示词';
COMMENT ON COLUMN task.error_message IS '失败原因';
COMMENT ON COLUMN task.created_by IS '任务创建人（账号 ID）';
COMMENT ON COLUMN task.created_at IS '创建时间';
COMMENT ON COLUMN task.updated_at IS '更新时间';
COMMENT ON COLUMN task.started_at IS '开始执行时间';
COMMENT ON COLUMN task.completed_at IS '完成时间';
COMMENT ON COLUMN task.ext IS '扩展字段';

CREATE INDEX IF NOT EXISTS idx_task_type ON task (type);
CREATE INDEX IF NOT EXISTS idx_task_status ON task (status);
CREATE INDEX IF NOT EXISTS idx_task_created_by ON task (created_by);
-- 多实例 Worker 按类型抢占 pending 任务时使用
CREATE INDEX IF NOT EXISTS idx_task_type_status_id ON task (type, status, id);
