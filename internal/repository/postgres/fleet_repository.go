package postgres

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/Qarani-m/ec2-api/internal/domain"
	"github.com/jmoiron/sqlx"
)

type fleetRepository struct {
	db *sqlx.DB
}

func NewFleetRepository(db *sqlx.DB) domain.FleetRepository {
	return &fleetRepository{db: db}
}

func (r *fleetRepository) GetOverview(userID string) (*domain.FleetOverview, error) {
	overview := &domain.FleetOverview{
		ClusterHealth: "Operational",
	}

	// Count instances
	query := `
		SELECT 
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE status = 'running') as active,
			COALESCE(AVG(cpu), 0) as avg_cpu,
			COALESCE(AVG(ram), 0) as avg_ram
		FROM instances 
		WHERE user_id = $1 AND status != 'terminated'
	`
	var stats struct {
		Total  int     `db:"total"`
		Active int     `db:"active"`
		AvgCPU float64 `db:"avg_cpu"`
		AvgRAM float64 `db:"avg_ram"`
	}

	err := r.db.Get(&stats, query, userID)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get instance stats: %w", err)
	}

	overview.TotalInstances = stats.Total
	overview.ActiveInstances = stats.Active
	// For telemetry load, we might need a separate metrics table. 
	// For now, we use configured averages or placeholders.
	overview.AvgCpuLoad = stats.AvgCPU // Placeholder for actual load
	overview.AvgRamUsage = stats.AvgRAM // Placeholder for actual usage

	// Count lambda functions
	var lambdaCount int
	err = r.db.Get(&lambdaCount, "SELECT COUNT(*) FROM lambda_functions WHERE user_id = $1 AND status = 'active'", userID)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get lambda count: %w", err)
	}
	overview.ActiveFunctions = lambdaCount

	return overview, nil
}

func (r *fleetRepository) GetEvents(userID string, limit int) ([]*domain.FleetEvent, error) {
	var events []*domain.FleetEvent
	query := `
		SELECT id, timestamp, type, message, resource, user_id 
		FROM activity_logs 
		WHERE user_id = $1 
		ORDER BY timestamp DESC 
		LIMIT $2
	`
	err := r.db.Select(&events, query, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch activity logs: %w", err)
	}
	return events, nil
}

func (r *fleetRepository) LogEvent(event *domain.FleetEvent) error {
	query := `
		INSERT INTO activity_logs (id, timestamp, type, message, resource, user_id)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	_, err := r.db.Exec(query, event.ID, event.Timestamp, event.Type, event.Message, event.Resource, event.UserID)
	return err
}

type lambdaRepository struct {
	db *sqlx.DB
}

func NewLambdaRepository(db *sqlx.DB) domain.LambdaRepository {
	return &lambdaRepository{db: db}
}

func (r *lambdaRepository) CountActive(userID string) (int, error) {
	var count int
	err := r.db.Get(&count, "SELECT COUNT(*) FROM lambda_functions WHERE user_id = $1 AND status = 'active'", userID)
	return count, err
}
