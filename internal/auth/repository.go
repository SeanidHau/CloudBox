package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var ErrUsernameTaken = errors.New("username already exists")

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CreateUser(ctx context.Context, username, passwordHash string) (User, error) {
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash) VALUES (?, ?)`,
		username,
		passwordHash,
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return User{}, ErrUsernameTaken
		}
		return User{}, fmt.Errorf("insert user: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return User{}, fmt.Errorf("read inserted user id: %w", err)
	}

	return r.FindByID(ctx, id)
}

func (r *Repository) FindByUsername(ctx context.Context, username string) (User, error) {
	var user User
	err := r.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, created_at FROM users WHERE username = ?`,
		username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		return User{}, err
	}
	return user, nil
}

func (r *Repository) FindByID(ctx context.Context, id int64) (User, error) {
	var user User
	err := r.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, created_at FROM users WHERE id = ?`,
		id,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		return User{}, err
	}
	return user, nil
}

func isUniqueConstraint(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "constraint failed") || strings.Contains(err.Error(), "UNIQUE constraint failed"))
}
