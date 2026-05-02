package postgres

import (
	"database/sql"
	"time"

	"ec2-api/internal/domain"
	"ec2-api/internal/interfaces"

	"github.com/jmoiron/sqlx"
)

type securityGroupRepository struct {
	db *sqlx.DB
}

func NewSecurityGroupRepository(db *sqlx.DB) interfaces.SecurityGroupRepository {
	return &securityGroupRepository{db: db}
}

func (r *securityGroupRepository) Create(sg *domain.SecurityGroup) error {
	query := `INSERT INTO security_groups (name, description, created_at, user_id) 
	          VALUES ($1, $2, $3, $4) RETURNING id`

	sg.CreatedAt = time.Now()
	err := r.db.QueryRow(query, sg.Name, sg.Description, sg.CreatedAt, sg.UserID).Scan(&sg.ID)
	return err
}

func (r *securityGroupRepository) FindByID(id int) (*domain.SecurityGroup, error) {
	var sg domain.SecurityGroup
	query := `SELECT id, name, description, created_at FROM security_groups WHERE id = $1`

	err := r.db.Get(&sg, query, id)
	if err == sql.ErrNoRows {
		return nil, domain.ErrSecurityGroupNotFound
	}
	if err != nil {
		return nil, err
	}

	// Fetch rules
	rules, err := r.GetRules(id)
	if err != nil {
		return nil, err
	}
	sg.Rules = rules

	return &sg, nil
}

func (r *securityGroupRepository) FindByName(name string) (*domain.SecurityGroup, error) {
	var sg domain.SecurityGroup
	query := `SELECT id, name, description, created_at FROM security_groups WHERE name = $1`

	err := r.db.Get(&sg, query, name)
	if err == sql.ErrNoRows {
		return nil, domain.ErrSecurityGroupNotFound
	}
	if err != nil {
		return nil, err
	}

	// Fetch rules
	rules, err := r.GetRules(sg.ID)
	if err != nil {
		return nil, err
	}
	sg.Rules = rules

	return &sg, nil
}

func (r *securityGroupRepository) FindAll() ([]*domain.SecurityGroup, error) {
	var sgs []*domain.SecurityGroup
	query := `SELECT id, name, description, created_at FROM security_groups ORDER BY created_at DESC`
	err := r.db.Select(&sgs, query)
	if err != nil {
		return nil, err
	}

	for _, sg := range sgs {
		rules, err := r.GetRules(sg.ID)
		if err == nil {
			sg.Rules = rules
		}
	}

	return sgs, nil
}

func (r *securityGroupRepository) Update(sg *domain.SecurityGroup) error {
	query := `UPDATE security_groups SET name = $1, description = $2 WHERE id = $3`
	_, err := r.db.Exec(query, sg.Name, sg.Description, sg.ID)
	return err
}

func (r *securityGroupRepository) Delete(id int) error {
	query := `DELETE FROM security_groups WHERE id = $1`
	_, err := r.db.Exec(query, id)
	return err
}

// Rules
func (r *securityGroupRepository) AddRule(rule *domain.SecurityGroupRule) error {
	query := `INSERT INTO security_group_rules (security_group_id, type, protocol, from_port, to_port, source_dest_cidr, description, created_at)
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`

	rule.CreatedAt = time.Now()
	err := r.db.QueryRow(query, rule.SecurityGroupID, rule.Type, rule.Protocol, rule.FromPort, rule.ToPort, rule.SourceDestCIDR, rule.Description, rule.CreatedAt).Scan(&rule.ID)
	return err
}

func (r *securityGroupRepository) RemoveRule(ruleID int) error {
	query := `DELETE FROM security_group_rules WHERE id = $1`
	_, err := r.db.Exec(query, ruleID)
	return err
}

func (r *securityGroupRepository) GetRules(sgID int) ([]domain.SecurityGroupRule, error) {
	var rules []domain.SecurityGroupRule
	query := `SELECT * FROM security_group_rules WHERE security_group_id = $1 ORDER BY created_at ASC`
	err := r.db.Select(&rules, query, sgID)
	return rules, err
}

// Instance Associations
func (r *securityGroupRepository) AddInstanceToGroup(instanceID string, sgID int) error {
	query := `INSERT INTO instance_security_groups (instance_id, security_group_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	_, err := r.db.Exec(query, instanceID, sgID)
	return err
}

func (r *securityGroupRepository) RemoveInstanceFromGroup(instanceID string, sgID int) error {
	query := `DELETE FROM instance_security_groups WHERE instance_id = $1 AND security_group_id = $2`
	_, err := r.db.Exec(query, instanceID, sgID)
	return err
}

func (r *securityGroupRepository) GetGroupsForInstance(instanceID string) ([]*domain.SecurityGroup, error) {
	var sgs []*domain.SecurityGroup
	query := `SELECT sg.id, sg.name, sg.description, sg.created_at 
	          FROM security_groups sg
	          JOIN instance_security_groups isg ON sg.id = isg.security_group_id
	          WHERE isg.instance_id = $1`
	err := r.db.Select(&sgs, query, instanceID)
	if err != nil {
		return nil, err
	}

	for _, sg := range sgs {
		rules, err := r.GetRules(sg.ID)
		if err == nil {
			sg.Rules = rules
		}
	}

	return sgs, nil
}
