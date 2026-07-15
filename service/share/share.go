// Package share implements share-link creation and public, policy-enforced
// access to shared files and folders.
package share

import (
	"context"
	"crypto/rand"
	"errors"
	"path"
	"strings"
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
	ErrNotAllowed       = errors.New("your group is not allowed to create shares")
	ErrNotAFile         = errors.New("target is not a file")
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

// Create makes a share link for a file or folder owned by the user.
func (s *Service) Create(ctx context.Context, user *model.User, opts CreateOptions) (*model.Share, error) {
	if user.Group != nil && !user.Group.CanShare() {
		return nil, ErrNotAllowed
	}
	f, err := s.repo.File.GetByID(ctx, user.ID, opts.FileID)
	if err != nil {
		return nil, err
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

// UpdateOptions describes changes to an existing share. A nil field is left
// unchanged; for Password, an empty string removes the password. For ExpiresDays
// and MaxDownloads, a value <= 0 clears the limit (never expires / unlimited).
type UpdateOptions struct {
	Password     *string
	ExpiresDays  *int
	MaxDownloads *int
}

// Update changes a share's settings (owner only).
func (s *Service) Update(ctx context.Context, user *model.User, token string, opts UpdateOptions) (*model.Share, error) {
	share, err := s.repo.Share.GetByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if share.UserID != user.ID {
		return nil, ErrNotFound
	}
	if opts.Password != nil {
		if *opts.Password == "" {
			share.Password = ""
		} else {
			hash, err := bcrypt.GenerateFromPassword([]byte(*opts.Password), bcrypt.DefaultCost)
			if err != nil {
				return nil, err
			}
			share.Password = string(hash)
		}
	}
	if opts.ExpiresDays != nil {
		if *opts.ExpiresDays <= 0 {
			share.Expires = nil
		} else {
			exp := time.Now().Add(time.Duration(*opts.ExpiresDays) * 24 * time.Hour)
			share.Expires = &exp
		}
	}
	if opts.MaxDownloads != nil {
		if *opts.MaxDownloads <= 0 {
			share.RemainDownloads = nil
		} else {
			remaining := *opts.MaxDownloads - share.Downloads
			if remaining < 0 {
				remaining = 0
			}
			share.RemainDownloads = &remaining
		}
	}
	if err := s.repo.Share.Update(ctx, share); err != nil {
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
	IsDir       bool   `json:"is_dir"`
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
		IsDir:       f.IsFolder(),
		Size:        f.Size,
		HasPassword: share.HasPassword(),
		Expired:     share.Expired(),
		Exhausted:   share.Exhausted(),
		Downloads:   share.Downloads,
		Owner:       owner,
	}, nil
}

// Entry is one item inside a shared folder listing.
type Entry struct {
	Name  string `json:"name"`
	Path  string `json:"path"` // path relative to the share root
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

// List returns the contents of a shared folder (or subfolder at subPath).
func (s *Service) ListDir(ctx context.Context, token, subPath, password string) (string, []Entry, error) {
	share, err := s.authorize(ctx, token, password)
	if err != nil {
		return "", nil, err
	}
	target, err := s.resolve(ctx, share, subPath)
	if err != nil {
		return "", nil, err
	}
	if !target.IsFolder() {
		return "", nil, ErrNotAFile
	}
	children, err := s.repo.File.ListChildren(ctx, share.UserID, &target.ID)
	if err != nil {
		return "", nil, err
	}
	rel := cleanPath(subPath)
	entries := make([]Entry, 0, len(children))
	for i := range children {
		c := &children[i]
		entries = append(entries, Entry{
			Name:  c.Name,
			Path:  strings.TrimPrefix(path.Join(rel, c.Name), "/"),
			IsDir: c.IsFolder(),
			Size:  c.Size,
		})
	}
	return target.Name, entries, nil
}

// Check validates a share for download (expiry, password, download limit, and
// that the target at subPath is a file) WITHOUT streaming or recording a
// download. Used by the public page to surface errors before starting the
// actual download (which is a plain browser navigation).
func (s *Service) Check(ctx context.Context, token, subPath, password string) error {
	share, err := s.authorize(ctx, token, password)
	if err != nil {
		return err
	}
	if share.Exhausted() {
		return ErrExhausted
	}
	target, err := s.resolve(ctx, share, subPath)
	if err != nil {
		return err
	}
	if target.IsFolder() {
		return ErrNotAFile
	}
	return nil
}

// Download validates the share and returns a download target for a file at
// subPath (empty for a file share's root), then records the download.
func (s *Service) Download(ctx context.Context, token, subPath, password string) (*filemanager.DownloadTarget, error) {
	share, err := s.authorize(ctx, token, password)
	if err != nil {
		return nil, err
	}
	if share.Exhausted() {
		return nil, ErrExhausted
	}
	target, err := s.resolve(ctx, share, subPath)
	if err != nil {
		return nil, err
	}
	if target.IsFolder() {
		return nil, ErrNotAFile
	}
	owner, err := s.repo.User.GetByID(ctx, share.UserID)
	if err != nil {
		return nil, err
	}
	dt, err := s.files.Download(ctx, owner, target.ID)
	if err != nil {
		return nil, err
	}
	_ = s.repo.Share.RegisterDownload(ctx, share)
	return dt, nil
}

// ArchiveTarget authorizes a share, resolves the folder to archive at subPath,
// records the download and returns the owner and target so the caller can stream
// the ZIP itself (headers must be sent before the stream starts).
func (s *Service) ArchiveTarget(ctx context.Context, token, subPath, password string) (*model.User, *model.File, error) {
	share, err := s.authorize(ctx, token, password)
	if err != nil {
		return nil, nil, err
	}
	if share.Exhausted() {
		return nil, nil, ErrExhausted
	}
	target, err := s.resolve(ctx, share, subPath)
	if err != nil {
		return nil, nil, err
	}
	owner, err := s.repo.User.GetByID(ctx, share.UserID)
	if err != nil {
		return nil, nil, err
	}
	_ = s.repo.Share.RegisterDownload(ctx, share)
	return owner, target, nil
}

// authorize validates a share's expiry and password (but not its download limit,
// which only gates actual downloads).
func (s *Service) authorize(ctx context.Context, token, password string) (*model.Share, error) {
	share, err := s.repo.Share.GetByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if share.Expired() {
		return nil, ErrExpired
	}
	if share.HasPassword() {
		if password == "" {
			return nil, ErrPasswordRequired
		}
		if bcrypt.CompareHashAndPassword([]byte(share.Password), []byte(password)) != nil {
			return nil, ErrWrongPassword
		}
	}
	return share, nil
}

// resolve walks subPath from the share root, staying within the shared subtree.
func (s *Service) resolve(ctx context.Context, share *model.Share, subPath string) (*model.File, error) {
	cur, err := s.repo.File.GetByID(ctx, share.UserID, share.FileID)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(cleanPath(subPath), "/") {
		if part == "" {
			continue
		}
		if !cur.IsFolder() {
			return nil, ErrNotFound
		}
		child, err := s.repo.File.FindChildByName(ctx, share.UserID, &cur.ID, part)
		if err != nil {
			return nil, ErrNotFound
		}
		cur = child
	}
	return cur, nil
}

// cleanPath normalises a subpath and strips any traversal.
func cleanPath(p string) string {
	return strings.Trim(path.Clean("/"+strings.ReplaceAll(p, "\\", "/")), "/")
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
