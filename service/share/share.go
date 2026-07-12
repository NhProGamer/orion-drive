// Package share implements share-link creation and public, policy-enforced access.
package share

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/filemanager"
	"github.com/NhProGamer/orion-drive/repository"
	"golang.org/x/crypto/bcrypt"
)

// Errors surfaced to the API layer.
var (
	ErrNotFound         = repository.ErrNotFound
	ErrExpired          = errors.New("this share has expired")
	ErrExhausted        = errors.New("this share has reached its download limit")
	ErrPasswordRequired = errors.New("a password is required")
	ErrWrongPassword    = errors.New("incorrect password")
	ErrFolderShare      = errors.New("folder sharing is not supported yet")
)

// Service coordinates shares (repository) and file delivery (filemanager).
type Service struct {
	repo  *repository.Repository
	files *filemanager.Manager
}

// New builds a share Service.
func New(repo *repository.Repository, files *filemanager.Manager) *Service {
	return &Service{repo: repo, files: files}
}

// CreateOptions describes a new share link.
type CreateOptions struct {
	FileID       uint
	Password     string
	ExpiresIn    time.Duration // 0 = never expires
	MaxDownloads int           // 0 = unlimited
}

// Create makes a share link for a file owned by the user.
func (s *Service) Create(ctx context.Context, user *model.User, opts CreateOptions) (*model.Share, error) {
	f, err := s.repo.File.GetByID(ctx, user.ID, opts.FileID)
	if err != nil {
		return nil, err
	}
	if f.IsFolder() {
		return nil, ErrFolderShare
	}

	share := &model.Share{
		Token:  randToken(22),
		FileID: f.ID,
		UserID: user.ID,
	}
	if opts.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(opts.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		share.Password = string(hash)
	}
	if opts.ExpiresIn > 0 {
		exp := time.Now().Add(opts.ExpiresIn)
		share.Expires = &exp
	}
	if opts.MaxDownloads > 0 {
		n := opts.MaxDownloads
		share.RemainDownloads = &n
	}
	if err := s.repo.Share.Create(ctx, share); err != nil {
		return nil, err
	}
	return share, nil
}

// List returns the user's shares.
func (s *Service) List(ctx context.Context, user *model.User) ([]model.Share, error) {
	return s.repo.Share.ListByUser(ctx, user.ID)
}

// Delete removes one of the user's shares.
func (s *Service) Delete(ctx context.Context, user *model.User, token string) error {
	return s.repo.Share.DeleteByToken(ctx, user.ID, token)
}

// PublicView is the metadata shown on a public share page.
type PublicView struct {
	Token       string `json:"token"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	HasPassword bool   `json:"has_password"`
	Expired     bool   `json:"expired"`
	Exhausted   bool   `json:"exhausted"`
	Downloads   int    `json:"downloads"`
	Owner       string `json:"owner"`
}

// View returns public metadata for a share and records a view.
func (s *Service) View(ctx context.Context, token string) (*PublicView, error) {
	share, err := s.repo.Share.GetByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	f, err := s.repo.File.GetByIDUnscoped(ctx, share.UserID, share.FileID)
	if err != nil {
		return nil, err
	}
	_ = s.repo.Share.IncrementViews(ctx, share.ID)

	owner := ""
	if u, err := s.repo.User.GetByID(ctx, share.UserID); err == nil {
		owner = u.DisplayName()
	}
	return &PublicView{
		Token:       share.Token,
		Name:        f.Name,
		Size:        f.Size,
		HasPassword: share.HasPassword(),
		Expired:     share.Expired(),
		Exhausted:   share.Exhausted(),
		Downloads:   share.Downloads,
		Owner:       owner,
	}, nil
}

// Download validates the share (expiry, download limit, password) and returns a
// download target for the shared file, then records the download.
func (s *Service) Download(ctx context.Context, token, password string) (*filemanager.DownloadTarget, error) {
	share, err := s.repo.Share.GetByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if share.Expired() {
		return nil, ErrExpired
	}
	if share.Exhausted() {
		return nil, ErrExhausted
	}
	if share.HasPassword() {
		if password == "" {
			return nil, ErrPasswordRequired
		}
		if bcrypt.CompareHashAndPassword([]byte(share.Password), []byte(password)) != nil {
			return nil, ErrWrongPassword
		}
	}

	owner, err := s.repo.User.GetByID(ctx, share.UserID)
	if err != nil {
		return nil, err
	}
	target, err := s.files.Download(ctx, owner, share.FileID)
	if err != nil {
		return nil, err
	}
	_ = s.repo.Share.RegisterDownload(ctx, share)
	return target, nil
}

const tokenAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// randToken returns a URL-safe random token of length n.
func randToken(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	for i := range buf {
		buf[i] = tokenAlphabet[int(buf[i])%len(tokenAlphabet)]
	}
	return string(buf)
}
