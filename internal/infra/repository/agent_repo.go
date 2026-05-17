// repository/rollout_repo.go
package postgres

import (
	domain "ec2-api/internal/domain/instance"
	"ec2-api/internal/interfaces"

	"context"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type RolloutRepo struct {
	db *sqlx.DB
}

 
func NewRolloutRepo(db *sqlx.DB) interfaces.RolloutRepository {
	return &RolloutRepo{db: db}
}
func (r *RolloutRepo) CreateRollout(ctx context.Context, rollout domain.AgentRollout) error {
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO agent_rollouts (id, version, file_name, sha256, url, initiated_by, total, created_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())`,
		rollout.ID, rollout.Version, rollout.FileName,
		rollout.SHA256, rollout.URL, rollout.InitiatedBy, rollout.Total,
	)
	return err
}

func (r *RolloutRepo) UpdateRolloutSummary(ctx context.Context, rolloutID uuid.UUID, ok, failed int, s3Err string) error {
	_, err := r.db.ExecContext(ctx, `
        UPDATE agent_rollouts SET ok=$1, failed=$2, s3_error=NULLIF($3,'') WHERE id=$4`,
		ok, failed, s3Err, rolloutID,
	)
	return err
}

func (r *RolloutRepo) InsertUpdateStatus(ctx context.Context, s domain.AgentUpdateStatus) error {
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO agent_update_statuses (id, rollout_id, host_id, host_ip, status, error_msg, sent_at, updated_at)
        VALUES ($1,$2,$3,$4,'pending',NULL,NOW(),NOW())`,
		s.ID, s.RolloutID, s.HostID, s.HostIP,
	)
	return err
}

// Called when a host pings back with its new version
func (r *RolloutRepo) UpdateStatusByHostAndVersion(ctx context.Context, hostID uuid.UUID, version, status string) error {
	_, err := r.db.ExecContext(ctx, `
        UPDATE agent_update_statuses aus
        SET    status=$1, updated_at=NOW()
        FROM   agent_rollouts ar
        WHERE  aus.rollout_id = ar.id
          AND  aus.host_id    = $2
          AND  ar.version     = $3
          AND  aus.status     = 'pending'`,
		status, hostID, version,
	)
	return err
}

func (r *RolloutRepo) GetStatusByRollout(ctx context.Context, rolloutID uuid.UUID) ([]domain.AgentUpdateStatus, error) {
	var statuses []domain.AgentUpdateStatus
	err := r.db.SelectContext(ctx, &statuses, `
        SELECT * FROM agent_update_statuses WHERE rollout_id = $1`,
		rolloutID,
	)
	return statuses, err
}
