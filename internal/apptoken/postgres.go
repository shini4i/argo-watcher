package apptoken

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// whereID is the predicate every by-id statement in this store shares.
const whereID = "id = ?"

// tokenRow is the app_tokens table. It stays unexported: the scope is two columns
// on the wire and one Scope everywhere else, and nothing outside this file should
// have to know which.
type tokenRow struct {
	Id          uuid.UUID                   `gorm:"column:id;type:uuid;not null;default:gen_random_uuid();"`
	TokenHash   []byte                      `gorm:"column:token_hash;not null;uniqueIndex;"`
	Apps        datatypes.JSONSlice[string] `gorm:"column:apps;type:jsonb;not null;"`
	AllApps     bool                        `gorm:"column:all_apps;not null;default:false;"`
	Hint        string                      `gorm:"column:hint;not null;"`
	Description string                      `gorm:"column:description;not null;default:'';"`
	CreatedBy   string                      `gorm:"column:created_by;not null;"`
	CreatedAt   time.Time                   `gorm:"column:created_at;autoCreateTime;not null;"`
	ExpiresAt   sql.NullTime                `gorm:"column:expires_at;"`
	RevokedAt   sql.NullTime                `gorm:"column:revoked_at;"`
	LastUsedAt  sql.NullTime                `gorm:"column:last_used_at;"`
}

func (tokenRow) TableName() string {
	return "app_tokens"
}

// toToken maps the row onto the metadata callers work with.
func (row *tokenRow) toToken() Token {
	return Token{
		Id:          row.Id,
		Scope:       Scope{Apps: row.Apps, AllApps: row.AllApps},
		Hint:        row.Hint,
		Description: row.Description,
		CreatedBy:   row.CreatedBy,
		CreatedAt:   row.CreatedAt,
		ExpiresAt:   row.ExpiresAt.Time,
		RevokedAt:   row.RevokedAt.Time,
		LastUsedAt:  row.LastUsedAt.Time,
	}
}

// PostgresStore persists application deploy tokens in Postgres, so a token issued
// through one replica is honored by all of them and revoking it takes effect
// everywhere on the next request.
type PostgresStore struct {
	db *gorm.DB
}

// NewPostgresStore creates a Store backed by the given database handle.
func NewPostgresStore(db *gorm.DB) Store {
	return &PostgresStore{db: db}
}

// Issue mints a token and stores everything about it except the secret.
func (s *PostgresStore) Issue(scope Scope, description, createdBy string, expiresAt time.Time) (*IssuedToken, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}

	credential, err := NewCredential()
	if err != nil {
		return nil, err
	}

	// A nil slice would marshal to JSON null rather than an empty array, and the
	// scope CHECK evaluates to NULL against that instead of failing.
	apps := scope.Apps
	if apps == nil {
		apps = []string{}
	}

	row := tokenRow{
		TokenHash:   credential.Hash,
		Apps:        datatypes.NewJSONSlice(apps),
		AllApps:     scope.AllApps,
		Hint:        credential.Hint,
		Description: description,
		CreatedBy:   createdBy,
		ExpiresAt:   nullTime(expiresAt),
	}

	if err := s.db.Create(&row).Error; err != nil {
		return nil, fmt.Errorf("failed to store the application deploy token: %w", err)
	}

	return &IssuedToken{Token: row.toToken(), Secret: credential.Secret}, nil
}

// Lookup finds a token by the digest of the presented secret, one indexed read.
// No constant-time comparison is needed: there is no stored secret to compare
// against, only a digest a caller cannot reach without already holding 256 bits
// of the right value.
func (s *PostgresStore) Lookup(secret string) (*Token, error) {
	var row tokenRow

	err := s.db.Where("token_hash = ?", Hash(secret)).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to look up the application deploy token: %w", err)
	}

	token := row.toToken()
	return &token, nil
}

// List returns every token, newest first.
func (s *PostgresStore) List() ([]Token, error) {
	var rows []tokenRow

	if err := s.db.Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list application deploy tokens: %w", err)
	}

	tokens := make([]Token, 0, len(rows))
	for index := range rows {
		tokens = append(tokens, rows[index].toToken())
	}

	return tokens, nil
}

// Revoke withdraws a token. The revoked_at guard makes a second revocation a
// no-op rather than rewriting when the first one happened, and distinguishing that
// no-op from an unknown id needs the follow-up existence check.
func (s *PostgresStore) Revoke(id uuid.UUID) error {
	result := s.db.Model(&tokenRow{}).
		Where(whereID, id).
		Where("revoked_at IS NULL").
		Update("revoked_at", time.Now())

	if result.Error != nil {
		return fmt.Errorf("failed to revoke the application deploy token: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return nil
	}

	var count int64
	if err := s.db.Model(&tokenRow{}).Where(whereID, id).Count(&count).Error; err != nil {
		return fmt.Errorf("failed to confirm the application deploy token exists: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}

	return nil
}

// MarkUsed records that the token authorized a deployment. A failure here must not
// fail the deployment, so callers log it and carry on; it is bookkeeping.
func (s *PostgresStore) MarkUsed(id uuid.UUID) error {
	if err := s.db.Model(&tokenRow{}).Where(whereID, id).Update("last_used_at", time.Now()).Error; err != nil {
		return fmt.Errorf("failed to record use of the application deploy token: %w", err)
	}

	return nil
}

// nullTime renders a zero time as SQL NULL, which is how "no expiry" is stored.
func nullTime(value time.Time) sql.NullTime {
	return sql.NullTime{Time: value, Valid: !value.IsZero()}
}
