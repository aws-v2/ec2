package postgres

import (
	"context"
	"database/sql"
	"ec2-api/internal/domain"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}



func (r *PostgresRepository) CreateScalingPolicy(ctx context.Context, userID string, req *domain.ScalingPolicyRequest) error {
    query := `
        INSERT INTO ec2_scaling_policies (
            id, user_id, name, policy_type,
            min_capacity, max_capacity, target_value,
            scale_in_cooldown, scale_out_cooldown,
            created_at, updated_at
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
    `
    now := time.Now()
    _, err := r.db.ExecContext(ctx, query,
        uuid.New().String(), userID, req.Name, req.PolicyType,
        req.MinCapacity, req.MaxCapacity, req.TargetValue,
        req.ScaleInCooldown, req.ScaleOutCooldown,
        now, now,
    )
    if err != nil {
        return fmt.Errorf("CreateScalingPolicy: %w", err)
    }
    return nil
}

func (r *PostgresRepository) GetScalingPolicies(ctx context.Context, userID string) ([]domain.ScalingPolicy, error) {
    query := `
        SELECT
            id, user_id, name, policy_type,
            min_capacity, max_capacity, target_value,
            scale_in_cooldown, scale_out_cooldown,
            created_at, updated_at
        FROM ec2_scaling_policies
        WHERE user_id = $1
        ORDER BY created_at DESC
    `
    rows, err := r.db.QueryContext(ctx, query, userID)
    if err != nil {
        return nil, fmt.Errorf("GetScalingPolicies: %w", err)
    }
    defer rows.Close()

    var policies []domain.ScalingPolicy
    for rows.Next() {
        var p domain.ScalingPolicy
        if err := rows.Scan(
            &p.ID, &p.UserID, &p.Name, &p.PolicyType,
            &p.MinCapacity, &p.MaxCapacity, &p.TargetValue,
            &p.ScaleInCooldown, &p.ScaleOutCooldown,
            &p.CreatedAt, &p.UpdatedAt,
        ); err != nil {
            return nil, fmt.Errorf("GetScalingPolicies scan: %w", err)
        }
        policies = append(policies, p)
    }
    return policies, rows.Err()
}
func (r *PostgresRepository) UpdateScalingPolicy(
	ctx context.Context,
	userID, policyID string,
	req *domain.UpdateScalingPolicyRequest,
) error {

	query := `
		UPDATE ec2_scaling_policies
		SET
			name               = COALESCE($1, name),
			min_capacity       = COALESCE($2, min_capacity),
			max_capacity       = COALESCE($3, max_capacity),
			target_value       = COALESCE($4, target_value),
			scale_in_cooldown  = COALESCE($5, scale_in_cooldown),
			scale_out_cooldown = COALESCE($6, scale_out_cooldown),
			target_type        = COALESCE($7, target_type),
			target_id          = COALESCE($8, target_id),
			metric_name        = COALESCE($9, metric_name),
			scale_down_value   = COALESCE($10, scale_down_value),
			max_instances      = COALESCE($11, max_instances),
			updated_at         = $12
		WHERE id = $13 AND user_id = $14
	`

	result, err := r.db.ExecContext(
		ctx,
		query,

		req.Name,
		req.MinCapacity,
		req.MaxCapacity,
		req.TargetValue,
		req.ScaleInCooldown,
		req.ScaleOutCooldown,
		req.TargetType,
		req.TargetID,
		req.MetricName,
		req.ScaleDownValue,
		req.MaxInstances,

		time.Now(),
		policyID,
		userID,
	)

	if err != nil {
		return fmt.Errorf("UpdateScalingPolicy: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("UpdateScalingPolicy rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("scaling policy %s not found for user %s", policyID, userID)
	}

	return nil
}
func (r *PostgresRepository) DeleteScalingPolicy(ctx context.Context, userID, policyID string) error {
    query := `
        DELETE FROM ec2_scaling_policies
        WHERE id = $1 AND user_id = $2
    `
    result, err := r.db.ExecContext(ctx, query, policyID, userID)
    if err != nil {
        return fmt.Errorf("DeleteScalingPolicy: %w", err)
    }

    rows, err := result.RowsAffected()
    if err != nil {
        return fmt.Errorf("DeleteScalingPolicy rows affected: %w", err)
    }
    if rows == 0 {
        return fmt.Errorf("scaling policy %s not found for user %s", policyID, userID)
    }
    return nil
}