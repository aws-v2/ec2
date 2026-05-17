-- migration 017: Add ssh_private_key column to hosts table
ALTER TABLE hosts ADD COLUMN IF NOT EXISTS ssh_private_key TEXT;



CREATE TABLE agent_rollouts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    version     VARCHAR(50)  NOT NULL,
    file_name   VARCHAR(255) NOT NULL,
    sha256      VARCHAR(64)  NOT NULL,
    url         TEXT         NOT NULL,
    initiated_by UUID        NOT NULL,          -- user_id
    total       INT          NOT NULL DEFAULT 0,
    ok          INT          NOT NULL DEFAULT 0,
    failed      INT          NOT NULL DEFAULT 0,
    s3_error    TEXT,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE agent_update_statuses (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rollout_id  UUID         NOT NULL REFERENCES agent_rollouts(id) ON DELETE CASCADE,
    host_id     TEXT         NOT NULL REFERENCES hosts(id)          ON DELETE CASCADE,  -- TEXT to match hosts.id
    host_ip     VARCHAR(45)  NOT NULL,
    status      VARCHAR(20)  NOT NULL DEFAULT 'pending',
    error_msg   TEXT,
    sent_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_rollout_host UNIQUE (rollout_id, host_id),
    CONSTRAINT chk_status CHECK (status IN ('pending', 'complete', 'failed'))
);

CREATE INDEX idx_update_statuses_host_id    ON agent_update_statuses(host_id);
CREATE INDEX idx_update_statuses_rollout_id ON agent_update_statuses(rollout_id);
CREATE INDEX idx_update_statuses_status     ON agent_update_statuses(status);