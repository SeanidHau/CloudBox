package auth

import (
	"database/sql"
	"errors"
)

var ErrUserNotFound = errors.New("user not found")

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(username string, passwordHash string) (*User, error) {
	var id int64

	err := r.db.QueryRow(
		`INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id`,
		username,
		passwordHash,
	).Scan(&id)
	if err != nil {
		return nil, err
	}

	return r.FindByID(id)
}

func (r *Repository) FindByUsername(username string) (*User, error) {
	var user User

	err := r.db.QueryRow(
		`SELECT id, username, password_hash, created_at FROM users WHERE username = $1`,
		username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}

	if err != nil {
		return nil, err
	}

	return &user, nil
}

func (r *Repository) FindByID(id int64) (*User, error) {
	var user User

	err := r.db.QueryRow(
		`SELECT id, username, password_hash, created_at FROM users WHERE id = $1`,
		id,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}

	if err != nil {
		return nil, err
	}

	return &user, nil
}
