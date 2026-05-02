package postgres

import (
	"database/sql"
	"time"

	"ec2-api/internal/domain"
	"ec2-api/internal/interfaces"
	"github.com/jmoiron/sqlx"
)

type sshKeyRepository struct {
	db *sqlx.DB
}

func NewSSHKeyRepository(db *sqlx.DB) interfaces.SSHKeyRepository {
	return &sshKeyRepository{db: db}
}

func (r *sshKeyRepository) Create(key *domain.SSHKey) error {
	query := `INSERT INTO ssh_keys (name, public_key, created_at, user_id) 
	          VALUES ($1, $2, $3, $4) RETURNING id`

	key.CreatedAt = time.Now()
	err := r.db.QueryRow(query, key.Name, key.PublicKey, key.CreatedAt, key.UserID).Scan(&key.ID)
	return err
}

func (r *sshKeyRepository) FindByID(id int) (*domain.SSHKey, error) {
	var key domain.SSHKey
	query := `SELECT * FROM ssh_keys WHERE id = $1`

	err := r.db.Get(&key, query, id)
	if err == sql.ErrNoRows {
		return nil, domain.ErrSSHKeyNotFound
	}
	return &key, err
}

func (r *sshKeyRepository) FindByName(name string) (*domain.SSHKey, error) {
	var key domain.SSHKey
	query := `SELECT * FROM ssh_keys WHERE name = $1`

	err := r.db.Get(&key, query, name)
	if err == sql.ErrNoRows {
		return nil, domain.ErrSSHKeyNotFound
	}
	return &key, err
}

func (r *sshKeyRepository) FindAll() ([]*domain.SSHKey, error) {
	var keys []*domain.SSHKey
	query := `SELECT * FROM ssh_keys ORDER BY created_at DESC`
	err := r.db.Select(&keys, query)
	return keys, err
}

func (r *sshKeyRepository) Delete(id int) error {
	query := `DELETE FROM ssh_keys WHERE id = $1`
	_, err := r.db.Exec(query, id)
	return err
}
