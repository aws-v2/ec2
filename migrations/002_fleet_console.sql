-- internal/repository/postgres/migrations/002_fleet_console.sql

CREATE TABLE IF NOT EXISTS activity_logs (
    id VARCHAR(50) PRIMARY KEY,
    timestamp TIMESTAMP NOT NULL DEFAULT NOW(),
    type VARCHAR(20) NOT NULL,
    message TEXT NOT NULL,
    resource VARCHAR(100) NOT NULL,
    user_id VARCHAR(50) NOT NULL
);

CREATE TABLE IF NOT EXISTS lambda_functions (
    id VARCHAR(50) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    status VARCHAR(20) NOT NULL,
    user_id VARCHAR(50) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_activity_logs_user_id ON activity_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_activity_logs_timestamp ON activity_logs(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_lambda_functions_user_id ON lambda_functions(user_id);
