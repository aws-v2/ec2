package postgres

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"ec2-api/internal/domain/host"

	"github.com/jmoiron/sqlx"
)

type MetricsRepository struct {
	db     *sqlx.DB
	logger *slog.Logger
}

func NewMetricsRepo(db *sqlx.DB, logger *slog.Logger) host.MetricsRepository {
	if logger == nil {
		logger = slog.Default()
	}
	return &MetricsRepository{
		db:     db,
		logger: logger.With("component", "metrics_repository"),
	}
}

func (r *MetricsRepository) GetByGatewayHostID(
	ctx context.Context,
	hostID string,

) (*host.Host, error) {

	query := `SELECT * FROM gateway_ports WHERE gateway_id = $1`
	var host host.Host

	err := r.db.Get(&host, query, hostID)
	if err == sql.ErrNoRows {
		return nil, errors.New("host not found")
	}
	return &host, err

}

func (r *MetricsRepository) InsertGateway(
	ctx context.Context,
	hostID string,
	gateway host.GatewayHost,

) error {

_, err := r.db.ExecContext(ctx, `
    INSERT INTO gateway_ports(gateway_id, port, status)
    SELECT $1, p, 'AVAILABLE'
    FROM generate_series($2::int, $3::int) AS p
    ON CONFLICT (gateway_id, port) DO NOTHING;
`, hostID, gateway.StartPort, gateway.EndPort)
	if err != nil {
		return err
	}
	return nil

}
func (r *MetricsRepository) Insert(
	ctx context.Context,
	hostID string,
	metric host.DomainStats,
) error {
	log := r.logger.With(
		"method", "Insert",
		"vm_id", metric.VMID,
		"host_id", hostID,
		"vm_name", metric.Name,
		"state", metric.State,
	)

	log.DebugContext(ctx, "inserting vm metric",
		"cpu_used", metric.CPUUsed,
		"memory_used", metric.Memory,
		"disk_read", metric.DiskRead,
		"disk_write", metric.DiskWrite,
		"net_rx", metric.NetRx,
		"net_tx", metric.NetTx,
	)

	start := time.Now()

	_, err := r.db.ExecContext(ctx, `
        INSERT INTO vm_metrics (
            vm_id,
            host_id,
            name,
            state,
            cpu_used,
            memory_used,
            disk_read,
            disk_write,
            net_rx,
            net_tx,
            created_at
        )
        VALUES (
            $1, $2, $3, $4, $5,
            $6, $7, $8, $9, $10,
            NOW()
        )
    `,
		metric.VMID,
		hostID,
		metric.Name,
		metric.State,
		metric.CPUUsed,
		metric.Memory,
		metric.DiskRead,
		metric.DiskWrite,
		metric.NetRx,
		metric.NetTx,
	)

	elapsed := time.Since(start)

	if err != nil {
		log.ErrorContext(ctx, "failed to insert vm metric",
			"error", err,
			"duration_ms", elapsed.Milliseconds(),
		)
		return err
	}

	log.InfoContext(ctx, "vm metric inserted",
		"duration_ms", elapsed.Milliseconds(),
	)

	return nil
}

func (r *MetricsRepository) GetVMsRequiringAction(
	ctx context.Context,
	sleepDuration,
	terminateDuration time.Duration,
) ([]host.VMActionTarget, error) {
	log := r.logger.With(
		"method", "GetVMsRequiringAction",
		"sleep_duration", sleepDuration.String(),
		"terminate_duration", terminateDuration.String(),
	)

	log.DebugContext(ctx, "querying vms requiring action")

	query := `
		WITH vm_activity AS (
			SELECT 
				vm_id,
				host_id,
				MAX(created_at) as last_seen,
				MAX(CASE WHEN created_at > NOW() - ($1 || ' seconds')::interval THEN 1 ELSE 0 END) as active_30m,
				MAX(CASE WHEN created_at > NOW() - ($2 || ' seconds')::interval THEN 1 ELSE 0 END) as active_1h
			FROM vm_metrics
			WHERE created_at > NOW() - ($2 || ' seconds')::interval
			AND (cpu_used > 0.1 OR disk_read > 0 OR disk_write > 0 OR net_rx > 0 OR net_tx > 0)
			GROUP BY vm_id, host_id
		)
		SELECT 
			a.vm_id,
			h.ip as host_ip,
			CASE 
				WHEN a.active_1h = 1 AND a.last_seen > NOW() - INTERVAL '5 minutes' THEN 'terminate'
				WHEN a.active_30m = 1 AND a.last_seen > NOW() - INTERVAL '5 minutes' THEN 'sleep'
			END as action
		FROM vm_activity a
		JOIN instances i ON i.id = a.vm_id
		JOIN hosts h ON h.id = a.host_id
		WHERE i.status = 'active'
		AND (
			(a.active_1h = 1) OR (a.active_30m = 1)
		)
	`

	start := time.Now()

	rows, err := r.db.QueryContext(
		ctx, query,
		int(sleepDuration.Seconds()),
		int(terminateDuration.Seconds()),
	)

	queryElapsed := time.Since(start)

	if err != nil {
		log.ErrorContext(ctx, "query failed",
			"error", err,
			"duration_ms", queryElapsed.Milliseconds(),
		)
		return nil, err
	}
	defer rows.Close()

	log.DebugContext(ctx, "query executed",
		"duration_ms", queryElapsed.Milliseconds(),
	)

	var targets []host.VMActionTarget
	var skipped int

	for rows.Next() {
		var t host.VMActionTarget
		if err := rows.Scan(&t.VMID, &t.HostIP, &t.Action); err != nil {
			log.ErrorContext(ctx, "failed to scan row",
				"error", err,
				"scanned_so_far", len(targets),
			)
			return nil, err
		}

		if t.Action == "" {
			// NULL CASE from the SQL — vm matched activity filter but
			// last_seen was older than 5 min, so no action was assigned.
			log.DebugContext(ctx, "skipping vm with no assigned action",
				"vm_id", t.VMID,
				"host_ip", t.HostIP,
			)
			skipped++
			continue
		}

		log.DebugContext(ctx, "vm action target found",
			"vm_id", t.VMID,
			"host_ip", t.HostIP,
			"action", t.Action,
		)

		targets = append(targets, t)
	}

	if err := rows.Err(); err != nil {
		log.ErrorContext(ctx, "row iteration error",
			"error", err,
			"targets_collected", len(targets),
		)
		return nil, err
	}

	log.InfoContext(ctx, "vm action targets resolved",
		"total_targets", len(targets),
		"skipped_no_action", skipped,
		"total_duration_ms", time.Since(start).Milliseconds(),
	)

	return targets, nil
}
