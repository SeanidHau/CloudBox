package auth

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/SeanidHau/CloudBox/internal/database"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

func newTestService(t *testing.T, secret string) *Service {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "cloudbox-test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(db, "../../migrations/001_init.sql", "../../migrations/012_user_access.sql"); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	return NewService(NewRepository(db), secret)
}

func createTestAdmin(t *testing.T, service *Service) *User {
	t.Helper()
	user, err := service.BootstrapAdmin("admin", "AdminPassword123")
	if err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}
	if user == nil {
		t.Fatal("expected first admin to be created")
	}
	return user
}

func inviteAndRegister(t *testing.T, service *Service, username string) *User {
	t.Helper()
	admin := createTestAdmin(t, service)
	invitation, err := service.CreateInvitation(admin.ID)
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	user, err := service.Register(username, "123456", invitation.Code)
	if err != nil {
		t.Fatalf("register invited user: %v", err)
	}
	return user
}

func TestRegisterRequiresUsableInvitationAndLoginReturnsToken(t *testing.T) {
	const secret = "test-secret"
	service := newTestService(t, secret)
	if _, err := service.Register("sean", "123456", ""); !errors.Is(err, ErrInviteCodeRequired) {
		t.Fatalf("register without invite = %v, want %v", err, ErrInviteCodeRequired)
	}
	user := inviteAndRegister(t, service, "sean")
	if user.ID == 0 || user.Role != RoleUser || user.StorageQuotaBytes != DefaultStorageQuotaBytes {
		t.Fatalf("registered user = %#v", user)
	}
	if user.PasswordHash == "123456" {
		t.Fatal("password was stored as plain text")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("123456")); err != nil {
		t.Fatalf("password hash does not match password: %v", err)
	}

	tokenText, loggedIn, err := service.Login("sean", "123456")
	if err != nil {
		t.Fatalf("login user: %v", err)
	}
	if tokenText == "" || loggedIn.ID != user.ID {
		t.Fatalf("login result = %q/%#v", tokenText, loggedIn)
	}
	token, err := jwt.Parse(tokenText, func(token *jwt.Token) (interface{}, error) { return []byte(secret), nil })
	if err != nil || !token.Valid {
		t.Fatalf("parse token: %v", err)
	}
	claims := token.Claims.(jwt.MapClaims)
	if got := int64(claims["user_id"].(float64)); got != user.ID {
		t.Fatalf("user_id claim = %d, want %d", got, user.ID)
	}
	if got := int64(claims["session_version"].(float64)); got != user.SessionVersion {
		t.Fatalf("session_version = %d, want %d", got, user.SessionVersion)
	}
}

func TestInvitationIsSingleUseAndCanBeRevoked(t *testing.T) {
	service := newTestService(t, "test-secret")
	admin := createTestAdmin(t, service)
	invitation, err := service.CreateInvitation(admin.ID)
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	if _, err := service.Register("first", "123456", invitation.Code); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	if _, err := service.Register("second", "123456", invitation.Code); !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("reuse invitation = %v, want %v", err, ErrInvalidInvitation)
	}

	revoked, err := service.CreateInvitation(admin.ID)
	if err != nil {
		t.Fatalf("create second invitation: %v", err)
	}
	if _, err := service.RevokeInvitation(admin.ID, revoked.Invitation.ID); err != nil {
		t.Fatalf("revoke invitation: %v", err)
	}
	if _, err := service.Register("third", "123456", revoked.Code); !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("revoked invitation = %v, want %v", err, ErrInvalidInvitation)
	}
}

func TestPasswordResetInvalidatesOldSessionAndRequiresChange(t *testing.T) {
	service := newTestService(t, "test-secret")
	user := inviteAndRegister(t, service, "sean")
	admin, err := service.repo.FindByUsername("admin")
	if err != nil {
		t.Fatalf("find admin: %v", err)
	}
	_, loggedIn, err := service.Login("sean", "123456")
	if err != nil {
		t.Fatalf("login user: %v", err)
	}
	temporary, resetUser, err := service.ResetPassword(admin.ID, user.ID)
	if err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if temporary == "" || !resetUser.MustChangePassword {
		t.Fatalf("reset result = %q/%#v", temporary, resetUser)
	}
	if err := service.ValidateSession(loggedIn.ID, loggedIn.SessionVersion); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old session validation = %v", err)
	}
	if _, _, err := service.Login("sean", temporary); err != nil {
		t.Fatalf("login temporary password: %v", err)
	}
	changed, err := service.ChangeOwnPassword(user.ID, temporary, "new-password")
	if err != nil || changed.MustChangePassword {
		t.Fatalf("change password = %#v/%v", changed, err)
	}
}

func TestAdminControlsUsersAndRejectsOrdinaryUsers(t *testing.T) {
	service := newTestService(t, "test-secret")
	user := inviteAndRegister(t, service, "sean")
	admin, err := service.repo.FindByUsername("admin")
	if err != nil {
		t.Fatalf("find admin: %v", err)
	}
	if _, err := service.ListUsers(user.ID); !errors.Is(err, ErrAdminRequired) {
		t.Fatalf("ordinary list users = %v", err)
	}
	updated, err := service.SetUserQuota(admin.ID, user.ID, 5<<30)
	if err != nil || updated.StorageQuotaBytes != 5<<30 {
		t.Fatalf("set quota = %#v/%v", updated, err)
	}
	if _, err := service.SetUserStatus(admin.ID, user.ID, StatusDisabled); err != nil {
		t.Fatalf("disable user: %v", err)
	}
	if _, _, err := service.Login("sean", "123456"); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("disabled login = %v", err)
	}
}

func TestBootstrapAdminPromotesExistingConfiguredUser(t *testing.T) {
	service := newTestService(t, "test-secret")
	passwordHash, err := hashPassword("ExistingPass123")
	if err != nil {
		t.Fatalf("hash existing password: %v", err)
	}
	user, err := service.repo.Create("configured", passwordHash, DefaultStorageQuotaBytes)
	if err != nil {
		t.Fatalf("create existing user: %v", err)
	}
	admin, err := service.BootstrapAdmin("configured", "different-password")
	if err != nil {
		t.Fatalf("bootstrap existing user: %v", err)
	}
	if admin == nil || admin.ID != user.ID || admin.Role != RoleAdmin {
		t.Fatalf("bootstrapped admin = %#v, want existing user %d with admin role", admin, user.ID)
	}
	if _, _, err := service.Login("configured", "ExistingPass123"); err != nil {
		t.Fatalf("existing password should be preserved: %v", err)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	service := newTestService(t, "test-secret")
	inviteAndRegister(t, service, "sean")
	_, _, err := service.Login("sean", "wrong-password")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("login error = %v, want %v", err, ErrInvalidCredentials)
	}
}
