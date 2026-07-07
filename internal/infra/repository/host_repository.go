package postgres

import (
	"context"
	"database/sql"
	domain "ec2-api/internal/domain/host"
	"ec2-api/internal/interfaces"
	"fmt"
	"log"
	"time"

	"github.com/lib/pq"
)

type hostRepository struct {
	db *sql.DB
}

func NewHostRepository(db *sql.DB) interfaces.HostRepository {
	return &hostRepository{db: db}
}

func (r *hostRepository) ListAll(ctx context.Context) ([]domain.Host, error) {
	query := `SELECT id, hosttype,hostname, ip, ssh_user, ssh_private_key, cpu_total, cpu_used, ram_total, ram_free, disk_total, disk_free, status, last_heartbeat, available_templates FROM hosts`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query hosts: %w", err)
	}
	defer rows.Close()

	var hosts []domain.Host
	for rows.Next() {
		h := domain.Host{}
		err := rows.Scan(
			&h.ID,&h.HostType, &h.Hostname, &h.IP, &h.SSHUser, &h.SSHPrivateKey,
			&h.CPUTotal, &h.CPUUsed, &h.RAMTotal, &h.RAMFree,
			&h.DiskTotal, &h.DiskFree, &h.Status, &h.LastHeartbeat,
			pq.Array(&h.AvailableTemplates),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan host: %w", err)
		}
		hosts = append(hosts, h)
	}
	return hosts, nil
}

func (r *hostRepository) Update(host *domain.Host) error {
	log.Printf("[-->host-service] handling heartbeat for host %s, available templates: %v, ssh_user: %s", host.ID, host.AvailableTemplates, host.HostType)

	query := `
		INSERT INTO hosts (id, hosttype,hostname, ip, ssh_user, ssh_private_key, cpu_total, cpu_used, ram_total, ram_free, disk_total, disk_free, status, last_heartbeat, available_templates)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)	
		ON CONFLICT (id) DO UPDATE SET
		hosttype=	EXCLUDED.hosttype,
		hostname = EXCLUDED.hostname,
			ip = EXCLUDED.ip,
			ssh_user = CASE 
				WHEN hosts.ssh_user IS NOT NULL AND hosts.ssh_user != '' AND hosts.ssh_user != 'root' THEN hosts.ssh_user 
				ELSE EXCLUDED.ssh_user 
			END,
			ssh_private_key = CASE
				WHEN EXCLUDED.ssh_private_key != '' THEN EXCLUDED.ssh_private_key
				ELSE hosts.ssh_private_key
			END,
			cpu_total = EXCLUDED.cpu_total,
			cpu_used = EXCLUDED.cpu_used,
			ram_total = EXCLUDED.ram_total,
			ram_free = EXCLUDED.ram_free,
			disk_total = EXCLUDED.disk_total,
			disk_free = EXCLUDED.disk_free,
			available_templates = EXCLUDED.available_templates,
			status = EXCLUDED.status,
			last_heartbeat = EXCLUDED.last_heartbeat
	`
	_, err := r.db.Exec(query,
		host.ID, host.HostType, host.Hostname, host.IP, host.SSHUser, host.SSHPrivateKey,
		host.CPUTotal, host.CPUUsed, host.RAMTotal, host.RAMFree,
		host.DiskTotal, host.DiskFree, host.Status, host.LastHeartbeat,
		pq.Array(host.AvailableTemplates),
	)
	return err
}

func (r *hostRepository) GetBestHosts(limit int) ([]*domain.Host, error) {
	// Scoring logic: sort by RAM free desc, CPU used asc, Disk free desc
	query := `
		SELECT id,hosttype, hostname, ip, ssh_user, cpu_total, cpu_used, ram_total, ram_free, disk_total, disk_free, status, last_heartbeat, created_at, available_templates
		FROM hosts
		WHERE status = 'active' AND last_heartbeat > $1
		ORDER BY ram_free DESC, cpu_used ASC, disk_free DESC
		LIMIT $2
	`
	// Assume heartbeats within last 5 minutes are active
	// threshold := time.Now().Add(-5 * time.Minute)
		threshold := time.Now().Add(-12 * time.Hour)
	rows, err := r.db.Query(query, threshold, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query best hosts: %w", err)
	}
	defer rows.Close()

	var hosts []*domain.Host
	for rows.Next() {
		h := &domain.Host{}
		err := rows.Scan(
			&h.ID,&h.HostType, &h.Hostname, &h.IP, &h.SSHUser, &h.CPUTotal, &h.CPUUsed,
			&h.RAMTotal, &h.RAMFree, &h.DiskTotal, &h.DiskFree,
			&h.Status, &h.LastHeartbeat, &h.CreatedAt,
			pq.Array(&h.AvailableTemplates),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan host: %w", err)
		}
		hosts = append(hosts, h)
	}
	return hosts, nil
}

func (r *hostRepository) GetBestHostsByType(limit int, targetType string) ([]*domain.Host, error) {
	// Scoring logic: sort by RAM free desc, CPU used asc, Disk free desc
	query := `
		SELECT id,hosttype, hostname, ip, ssh_user, cpu_total, cpu_used, ram_total, ram_free, disk_total, disk_free, status, last_heartbeat, created_at, available_templates
		FROM hosts
		WHERE status = 'active' AND last_heartbeat > $1 AND hosttype = $2
		ORDER BY ram_free DESC, cpu_used ASC, disk_free DESC
		LIMIT $3
	`
	// Assume heartbeats within last 5 minutes are active
	// threshold := time.Now().Add(-5 * time.Minute)
		threshold := time.Now().Add(-12 * time.Hour)
	rows, err := r.db.Query(query, threshold, targetType, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query best hosts by type: %w", err)
	}
	defer rows.Close()

	var hosts []*domain.Host
	for rows.Next() {
		h := &domain.Host{}
		err := rows.Scan(
			&h.ID,&h.HostType, &h.Hostname, &h.IP, &h.SSHUser, &h.CPUTotal, &h.CPUUsed,
			&h.RAMTotal, &h.RAMFree, &h.DiskTotal, &h.DiskFree,
			&h.Status, &h.LastHeartbeat, &h.CreatedAt,
			pq.Array(&h.AvailableTemplates),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan host: %w", err)
		}
		hosts = append(hosts, h)
	}
	return hosts, nil
}

func (r *hostRepository) GetByID(id string) (*domain.Host, error) {
	query := `SELECT id,hosttype, hostname, ip, ssh_user, ssh_private_key, cpu_total, cpu_used, ram_total, ram_free, disk_total, disk_free, status, last_heartbeat, available_templates FROM hosts WHERE id = $1`
	row := r.db.QueryRow(query, id)
	host := &domain.Host{}
	err := row.Scan(
		&host.ID,&host.HostType, &host.Hostname, &host.IP, &host.SSHUser, &host.SSHPrivateKey,
		&host.CPUTotal, &host.CPUUsed, &host.RAMTotal, &host.RAMFree,
		&host.DiskTotal, &host.DiskFree, &host.Status, &host.LastHeartbeat,
		pq.Array(&host.AvailableTemplates),
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get host by id: %w", err)
	}
	return host, nil
}
