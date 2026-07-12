-- live-mixer 数据库初始化（创新部署，无需兼容历史版本）

-- 账号表
CREATE TABLE IF NOT EXISTS account (
    id          BIGSERIAL PRIMARY KEY,
    username    VARCHAR(64)  NOT NULL UNIQUE,
    email       VARCHAR(128) NOT NULL UNIQUE,
    password    VARCHAR(255) NOT NULL,
    nickname    VARCHAR(64),
    avatar      VARCHAR(512),
    roles       VARCHAR(64),
    open_id     VARCHAR(128),
    remark      VARCHAR(256),
    phone       VARCHAR(32),
    ext         VARCHAR(1024),
    status      SMALLINT     NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
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
COMMENT ON COLUMN account.ext IS '扩展字段';
COMMENT ON COLUMN account.status IS '账号状态：1正常 0禁用';
COMMENT ON COLUMN account.created_at IS '创建时间';
COMMENT ON COLUMN account.updated_at IS '更新时间';
COMMENT ON COLUMN account.deleted_at IS '软删除时间';

CREATE INDEX IF NOT EXISTS idx_account_deleted_at ON account (deleted_at);
CREATE INDEX IF NOT EXISTS idx_account_open_id ON account (open_id);

-- 直播素材表：一条记录对应一场直播及其 ASR 结果
CREATE TABLE IF NOT EXISTS live_material (
    id           BIGSERIAL PRIMARY KEY,
    live_url     VARCHAR(1024) NOT NULL,
    asr_result   JSONB         NOT NULL DEFAULT '{}',
    created_by   BIGINT        NOT NULL REFERENCES account (id),
    created_at   TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    deleted_at   TIMESTAMPTZ
);

COMMENT ON TABLE live_material IS '直播素材表';
COMMENT ON COLUMN live_material.id IS '主键';
COMMENT ON COLUMN live_material.live_url IS '直播链接';
COMMENT ON COLUMN live_material.asr_result IS '直播视频 ASR 识别结果（JSON），默认为空对象';
COMMENT ON COLUMN live_material.created_by IS '添加人（账号 ID）';
COMMENT ON COLUMN live_material.created_at IS '添加时间';
COMMENT ON COLUMN live_material.updated_at IS '最后更新时间';
COMMENT ON COLUMN live_material.deleted_at IS '软删除时间';

CREATE INDEX IF NOT EXISTS idx_live_material_created_by ON live_material (created_by);
CREATE INDEX IF NOT EXISTS idx_live_material_deleted_at ON live_material (deleted_at);

-- 剪辑项目表：一个项目包含多个视频切片（切片见 edit_project_clip 表）
CREATE TABLE IF NOT EXISTS edit_project (
    id              BIGSERIAL PRIMARY KEY,
    created_by      BIGINT      NOT NULL REFERENCES account (id),
    last_edited_by  BIGINT      NOT NULL REFERENCES account (id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

COMMENT ON TABLE edit_project IS '剪辑项目表';
COMMENT ON COLUMN edit_project.id IS '主键';
COMMENT ON COLUMN edit_project.created_by IS '创建人（账号 ID）';
COMMENT ON COLUMN edit_project.last_edited_by IS '最后编辑人（账号 ID）';
COMMENT ON COLUMN edit_project.created_at IS '创建时间';
COMMENT ON COLUMN edit_project.updated_at IS '最后编辑时间';
COMMENT ON COLUMN edit_project.deleted_at IS '软删除时间';

CREATE INDEX IF NOT EXISTS idx_edit_project_created_by ON edit_project (created_by);
CREATE INDEX IF NOT EXISTS idx_edit_project_last_edited_by ON edit_project (last_edited_by);
CREATE INDEX IF NOT EXISTS idx_edit_project_deleted_at ON edit_project (deleted_at);

-- 剪辑项目切片表：满足第一范式，每个切片为独立原子记录
CREATE TABLE IF NOT EXISTS edit_project_clip (
    id               BIGSERIAL PRIMARY KEY,
    project_id       BIGINT      NOT NULL REFERENCES edit_project (id),
    live_material_id BIGINT      NOT NULL REFERENCES live_material (id),
    start_ms         BIGINT      NOT NULL,
    end_ms           BIGINT      NOT NULL,
    sort_order       INTEGER     NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_edit_project_clip_time CHECK (end_ms > start_ms)
);

COMMENT ON TABLE edit_project_clip IS '剪辑项目切片表';
COMMENT ON COLUMN edit_project_clip.id IS '主键';
COMMENT ON COLUMN edit_project_clip.project_id IS '所属剪辑项目 ID';
COMMENT ON COLUMN edit_project_clip.live_material_id IS '关联直播素材 ID';
COMMENT ON COLUMN edit_project_clip.start_ms IS '切片开始时间（毫秒）';
COMMENT ON COLUMN edit_project_clip.end_ms IS '切片结束时间（毫秒）';
COMMENT ON COLUMN edit_project_clip.sort_order IS '切片排序序号（升序）';
COMMENT ON COLUMN edit_project_clip.created_at IS '创建时间';
COMMENT ON COLUMN edit_project_clip.updated_at IS '更新时间';

CREATE INDEX IF NOT EXISTS idx_edit_project_clip_project_id ON edit_project_clip (project_id);
CREATE INDEX IF NOT EXISTS idx_edit_project_clip_live_material_id ON edit_project_clip (live_material_id);

-- 大模型系统提示词管理表
CREATE TABLE IF NOT EXISTS llm_system_prompt (
    id           BIGSERIAL PRIMARY KEY,
    name         VARCHAR(128) NOT NULL,
    content      TEXT         NOT NULL,
    is_system    BOOLEAN      NOT NULL DEFAULT FALSE,
    is_editable  BOOLEAN      NOT NULL DEFAULT TRUE,
    created_by   BIGINT       NOT NULL REFERENCES account (id),
    updated_by   BIGINT       NOT NULL REFERENCES account (id),
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    deleted_at   TIMESTAMPTZ,
    CONSTRAINT chk_llm_system_prompt_editable CHECK (NOT is_system OR is_editable = FALSE)
);

COMMENT ON TABLE llm_system_prompt IS '大模型系统提示词管理表';
COMMENT ON COLUMN llm_system_prompt.id IS '主键';
COMMENT ON COLUMN llm_system_prompt.name IS '提示词名称';
COMMENT ON COLUMN llm_system_prompt.content IS '提示词内容';
COMMENT ON COLUMN llm_system_prompt.is_system IS '是否系统默认提示词';
COMMENT ON COLUMN llm_system_prompt.is_editable IS '是否允许修改（系统默认提示词不可修改）';
COMMENT ON COLUMN llm_system_prompt.created_by IS '创建人（账号 ID）';
COMMENT ON COLUMN llm_system_prompt.updated_by IS '最后修改人（账号 ID）';
COMMENT ON COLUMN llm_system_prompt.created_at IS '创建时间';
COMMENT ON COLUMN llm_system_prompt.updated_at IS '更新时间';
COMMENT ON COLUMN llm_system_prompt.deleted_at IS '软删除时间';

CREATE INDEX IF NOT EXISTS idx_llm_system_prompt_created_by ON llm_system_prompt (created_by);
CREATE INDEX IF NOT EXISTS idx_llm_system_prompt_is_system ON llm_system_prompt (is_system);
CREATE INDEX IF NOT EXISTS idx_llm_system_prompt_deleted_at ON llm_system_prompt (deleted_at);

-- 任务表：异步任务统一入口，通过 type 区分三类业务
CREATE TABLE IF NOT EXISTS task (
    id                     BIGSERIAL PRIMARY KEY,
    type                   VARCHAR(32) NOT NULL,
    status                 VARCHAR(32) NOT NULL DEFAULT 'pending',
    live_material_id       BIGINT      REFERENCES live_material (id),
    edit_project_id        BIGINT      REFERENCES edit_project (id),
    result_edit_project_id BIGINT      REFERENCES edit_project (id),
    result_draft_url       VARCHAR(1024),
    prompt_id              BIGINT      REFERENCES llm_system_prompt (id),
    error_message          TEXT,
    created_by             BIGINT      NOT NULL REFERENCES account (id),
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at             TIMESTAMPTZ,
    finished_at            TIMESTAMPTZ,
    CONSTRAINT chk_task_type CHECK (type IN ('jianying_draft', 'ai_slice', 'ai_slice_jianying')),
    CONSTRAINT chk_task_status CHECK (status IN ('pending', 'processing', 'completed', 'failed'))
);

COMMENT ON TABLE task IS '任务表';
COMMENT ON COLUMN task.id IS '主键';
COMMENT ON COLUMN task.type IS '任务类型：jianying_draft剪映草稿生成 ai_slice AI切片 ai_slice_jianying AI切片+剪映草稿生成';
COMMENT ON COLUMN task.status IS '任务状态：pending待处理 processing执行中 completed已完成 failed失败';
COMMENT ON COLUMN task.live_material_id IS '源直播素材 ID（AI 切片类任务）';
COMMENT ON COLUMN task.edit_project_id IS '源剪辑项目 ID（剪映草稿生成类任务）';
COMMENT ON COLUMN task.result_edit_project_id IS '产出的剪辑项目 ID（AI 切片类任务）';
COMMENT ON COLUMN task.result_draft_url IS '产出的剪映草稿地址（剪映草稿生成类任务）';
COMMENT ON COLUMN task.prompt_id IS '使用的大模型系统提示词 ID（AI 切片类任务，可选）';
COMMENT ON COLUMN task.error_message IS '失败原因';
COMMENT ON COLUMN task.created_by IS '任务创建人（账号 ID）';
COMMENT ON COLUMN task.created_at IS '创建时间';
COMMENT ON COLUMN task.updated_at IS '更新时间';
COMMENT ON COLUMN task.started_at IS '开始执行时间';
COMMENT ON COLUMN task.finished_at IS '结束时间';

CREATE INDEX IF NOT EXISTS idx_task_type ON task (type);
CREATE INDEX IF NOT EXISTS idx_task_status ON task (status);
CREATE INDEX IF NOT EXISTS idx_task_created_by ON task (created_by);
CREATE INDEX IF NOT EXISTS idx_task_live_material_id ON task (live_material_id);
CREATE INDEX IF NOT EXISTS idx_task_edit_project_id ON task (edit_project_id);
