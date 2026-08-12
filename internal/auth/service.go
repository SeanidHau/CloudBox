package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	DefaultStorageQuotaBytes int64 = 1 << 30
	InvitationLifetime             = 7 * 24 * time.Hour
)

var (
	ErrUsernameRequired        = errors.New("username is required")
	ErrPasswordRequired        = errors.New("password is required")
	ErrInviteCodeRequired      = errors.New("invite code is required")
	ErrInvalidCredentials      = errors.New("invalid username or password")
	ErrAccountDisabled         = errors.New("account is disabled")
	ErrInvalidInvitation       = errors.New("invite code is invalid, expired, used, or revoked")
	ErrAdminRequired           = errors.New("administrator permission is required")
	ErrInvalidQuota            = errors.New("storage quota must be greater than zero")
	ErrCurrentPasswordRequired = errors.New("current password is required")
	ErrNewPasswordRequired     = errors.New("new password is required")
	ErrCannotDisableLastAdmin  = errors.New("cannot disable the last administrator")
	ErrCannotManageSelf        = errors.New("administrator cannot modify own status")
	ErrPasswordChangeRequired  = errors.New("password change is required")
)

type Service struct {
	repo         *Repository
	jwtSecret    string
	defaultQuota int64
	now          func() time.Time
}

func NewService(repo *Repository, jwtSecret string, quotaBytes ...int64) *Service {
	defaultQuota := DefaultStorageQuotaBytes
	if len(quotaBytes) > 0 && quotaBytes[0] > 0 {
		defaultQuota = quotaBytes[0]
	}
	return &Service{repo: repo, jwtSecret: jwtSecret, defaultQuota: defaultQuota, now: time.Now}
}

func (s *Service) Register(username string, password string, inviteCode string) (*User, error) {
	if username == "" {
		return nil, ErrUsernameRequired
	}
	if password == "" {
		return nil, ErrPasswordRequired
	}
	if inviteCode == "" {
		return nil, ErrInviteCodeRequired
	}

	digest := invitationDigest(inviteCode)
	invitation, codeHash, err := s.repo.FindInvitationByDigest(digest)
	if errors.Is(err, ErrInvitationNotFound) || !invitationUsable(invitation, s.now()) {
		return nil, ErrInvalidInvitation
	}
	if err != nil {
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(codeHash), []byte(inviteCode)); err != nil {
		return nil, ErrInvalidInvitation
	}

	passwordHash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	user, err := s.repo.ClaimInvitationAndCreateUser(digest, username, passwordHash, s.defaultQuota, s.now())
	if errors.Is(err, ErrInvitationNotFound) {
		return nil, ErrInvalidInvitation
	}
	return user, err
}

func (s *Service) BootstrapAdmin(username string, password string) (*User, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, nil
	}
	passwordHash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	return s.repo.CreateAdminIfMissing(username, passwordHash, s.defaultQuota)
}

func (s *Service) Login(username string, password string) (string, *User, error) {
	user, err := s.repo.FindByUsername(username)
	if errors.Is(err, ErrUserNotFound) {
		return "", nil, ErrInvalidCredentials
	}
	if err != nil {
		return "", nil, err
	}
	if user.Status != StatusActive {
		return "", nil, ErrAccountDisabled
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", nil, ErrInvalidCredentials
	}
	token, err := s.issueToken(user)
	return token, user, err
}

func (s *Service) ValidateSession(userID int64, sessionVersion int64) error {
	user, err := s.repo.FindByID(userID)
	if errors.Is(err, ErrUserNotFound) {
		return ErrInvalidCredentials
	}
	if err != nil {
		return err
	}
	if user.Status != StatusActive || user.SessionVersion != sessionVersion {
		return ErrInvalidCredentials
	}
	return nil
}

func (s *Service) RequirePasswordChanged(userID int64) error {
	user, err := s.repo.FindByID(userID)
	if err != nil {
		return err
	}
	if user.MustChangePassword {
		return ErrPasswordChangeRequired
	}
	return nil
}

func (s *Service) ChangeOwnPassword(userID int64, currentPassword string, newPassword string) (*User, error) {
	if currentPassword == "" {
		return nil, ErrCurrentPasswordRequired
	}
	if newPassword == "" {
		return nil, ErrNewPasswordRequired
	}
	user, err := s.repo.FindByID(userID)
	if err != nil {
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)); err != nil {
		return nil, ErrInvalidCredentials
	}
	passwordHash, err := hashPassword(newPassword)
	if err != nil {
		return nil, err
	}
	return s.repo.ChangePassword(userID, passwordHash)
}

func (s *Service) ListUsers(requesterID int64) ([]User, error) {
	if err := s.requireAdmin(requesterID); err != nil {
		return nil, err
	}
	return s.repo.ListUsers()
}

func (s *Service) SetUserQuota(requesterID int64, userID int64, quotaBytes int64) (*User, error) {
	if err := s.requireAdmin(requesterID); err != nil {
		return nil, err
	}
	if quotaBytes <= 0 {
		return nil, ErrInvalidQuota
	}
	return s.repo.SetUserQuota(userID, quotaBytes)
}

func (s *Service) SetUserStatus(requesterID int64, userID int64, status string) (*User, error) {
	if err := s.requireAdmin(requesterID); err != nil {
		return nil, err
	}
	if requesterID == userID {
		return nil, ErrCannotManageSelf
	}
	if status != StatusActive && status != StatusDisabled {
		return nil, ErrUserNotFound
	}
	if status == StatusDisabled {
		users, err := s.repo.ListUsers()
		if err != nil {
			return nil, err
		}
		for _, user := range users {
			if user.ID == userID && user.Role == RoleAdmin && user.Status == StatusActive {
				admins := 0
				for _, candidate := range users {
					if candidate.Role == RoleAdmin && candidate.Status == StatusActive {
						admins++
					}
				}
				if admins <= 1 {
					return nil, ErrCannotDisableLastAdmin
				}
			}
		}
	}
	return s.repo.SetUserStatus(userID, status)
}

func (s *Service) ResetPassword(requesterID int64, userID int64) (string, *User, error) {
	if err := s.requireAdmin(requesterID); err != nil {
		return "", nil, err
	}
	temporaryPassword, err := randomToken(18)
	if err != nil {
		return "", nil, err
	}
	passwordHash, err := hashPassword(temporaryPassword)
	if err != nil {
		return "", nil, err
	}
	user, err := s.repo.ResetPassword(userID, passwordHash)
	return temporaryPassword, user, err
}

func (s *Service) RevokeAllUserShares(requesterID int64, userID int64) (int64, error) {
	if err := s.requireAdmin(requesterID); err != nil {
		return 0, err
	}
	if _, err := s.repo.FindByID(userID); err != nil {
		return 0, err
	}
	return s.repo.RevokeAllUserShares(userID)
}

func (s *Service) CreateInvitation(requesterID int64) (*CreatedInvitation, error) {
	if err := s.requireAdmin(requesterID); err != nil {
		return nil, err
	}
	code, err := randomToken(20)
	if err != nil {
		return nil, err
	}
	codeHash, err := hashPassword(code)
	if err != nil {
		return nil, err
	}
	invitation, err := s.repo.CreateInvitation(invitationDigest(code), codeHash, requesterID, s.now().Add(InvitationLifetime))
	if err != nil {
		return nil, err
	}
	return &CreatedInvitation{Invitation: *invitation, Code: code}, nil
}

func (s *Service) ListInvitations(requesterID int64) ([]Invitation, error) {
	if err := s.requireAdmin(requesterID); err != nil {
		return nil, err
	}
	return s.repo.ListInvitations()
}

func (s *Service) RevokeInvitation(requesterID int64, invitationID int64) (*Invitation, error) {
	if err := s.requireAdmin(requesterID); err != nil {
		return nil, err
	}
	return s.repo.RevokeInvitation(invitationID)
}

func (s *Service) requireAdmin(userID int64) error {
	user, err := s.repo.FindByID(userID)
	if err != nil {
		return err
	}
	if user.Status != StatusActive || user.Role != RoleAdmin {
		return ErrAdminRequired
	}
	return nil
}

func (s *Service) issueToken(user *User) (string, error) {
	claims := jwt.MapClaims{
		"user_id":              user.ID,
		"session_version":      user.SessionVersion,
		"role":                 user.Role,
		"must_change_password": user.MustChangePassword,
		"exp":                  s.now().Add(24 * time.Hour).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.jwtSecret))
}

func invitationDigest(code string) string {
	sum := sha256.Sum256([]byte(code))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func invitationUsable(invitation *Invitation, now time.Time) bool {
	return invitation != nil && !invitation.UsedAt.Valid && !invitation.RevokedAt.Valid && invitation.ExpiresAt.After(now)
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

func randomToken(size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
