package postgres

import (
	"database/sql"
	"time"

	"ec2-api/internal/domain"
	"ec2-api/internal/interfaces"

	"github.com/jmoiron/sqlx"
)

type templateRepository struct {
	db *sqlx.DB
}

func NewTemplateRepository(db *sqlx.DB) interfaces.TemplateRepository {
	return &templateRepository{db: db}
}

func (r *templateRepository) Create(template *domain.Template) error {
	query := `INSERT INTO templates (instance_id, name, description, image, cpu, ram, status, created_at, user_id) 
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`

	template.CreatedAt = time.Now()
	err := r.db.QueryRow(query, template.InstanceID, template.Name, template.Description,
		template.Image, template.CPU, template.RAM, template.Status, template.CreatedAt, template.UserID).Scan(&template.ID)
	return err
}

func (r *templateRepository) FindByID(id int) (*domain.Template, error) {
	var template domain.Template
	query := `SELECT * FROM templates WHERE id = $1`

	err := r.db.Get(&template, query, id)
	if err == sql.ErrNoRows {
		return nil, domain.ErrTemplateNotFound
	}
	return &template, err
}

func (r *templateRepository) FindByName(name string) (*domain.Template, error) {
	var template domain.Template
	query := `SELECT * FROM templates WHERE name = $1`

	err := r.db.Get(&template, query, name)
	if err == sql.ErrNoRows {
		return nil, domain.ErrTemplateNotFound
	}
	return &template, err
}

func (r *templateRepository) FindAll() ([]*domain.Template, error) {
	var templates []*domain.Template
	query := `SELECT * FROM templates ORDER BY created_at DESC`
	err := r.db.Select(&templates, query)
	return templates, err
}

func (r *templateRepository) UpdateStatus(id int, status domain.TemplateStatus) error {
	query := `UPDATE templates SET status = $1 WHERE id = $2`
	_, err := r.db.Exec(query, status, id)
	return err
}

func (r *templateRepository) Delete(id int) error {
	query := `DELETE FROM templates WHERE id = $1`
	_, err := r.db.Exec(query, id)
	return err
}
