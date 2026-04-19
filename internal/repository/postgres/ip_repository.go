package postgres

import (
	"database/sql"
	"time"

	"ec2-api/internal/domain"

	"github.com/jmoiron/sqlx"
)

type ipRepository struct {
	db *sqlx.DB
}

func NewIPRepository(db *sqlx.DB) domain.IPRepository {
	return &ipRepository{db: db}
}

func (r *ipRepository) Create(ip *domain.IPAllocation) error {
	query := `INSERT INTO ip_allocations (instance_id, public_ip, private_ip, port_mappings, status, created_at, user_id) 
	          VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`

	ip.CreatedAt = time.Now()
	err := r.db.QueryRow(query, ip.InstanceID, ip.PublicIP, ip.PrivateIP, ip.PortMappings,
		ip.Status, ip.CreatedAt, ip.UserID).Scan(&ip.ID)
	return err
}

func (r *ipRepository) FindByID(id int) (*domain.IPAllocation, error) {
	var ip domain.IPAllocation
	query := `SELECT * FROM ip_allocations WHERE id = $1`

	err := r.db.Get(&ip, query, id)
	if err == sql.ErrNoRows {
		return nil, domain.ErrIPNotFound
	}
	return &ip, err
}

func (r *ipRepository) FindAll() ([]*domain.IPAllocation, error) {
	var ips []*domain.IPAllocation
	query := `SELECT * FROM ip_allocations ORDER BY created_at DESC`
	err := r.db.Select(&ips, query)
	return ips, err
}

func (r *ipRepository) Update(ip *domain.IPAllocation) error {
	query := `UPDATE ip_allocations SET instance_id = $1, status = $2, port_mappings = $3 WHERE id = $4`
	_, err := r.db.Exec(query, ip.InstanceID, ip.Status, ip.PortMappings, ip.ID)
	return err
}

func (r *ipRepository) Delete(id int) error {
	query := `DELETE FROM ip_allocations WHERE id = $1`
	_, err := r.db.Exec(query, id)
	return err
}
