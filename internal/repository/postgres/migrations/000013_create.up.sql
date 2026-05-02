CREATE TABLE IF NOT EXISTS ec2_scaling_policies (
    id                  TEXT PRIMARY KEY,
    user_id             TEXT NOT NULL,
    name                TEXT NOT NULL,
    policy_type         TEXT NOT NULL,
    min_capacity        INT,
    max_capacity        INT,
    target_value        FLOAT,
    scale_in_cooldown   INT,
    scale_out_cooldown  INT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ec2_scaling_policies_user_id ON ec2_scaling_policies(user_id);