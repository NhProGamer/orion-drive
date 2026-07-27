// Package share implements share-link creation and public, policy-enforced
// access to shared files and folders.
package share

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/filemanager"
	"github.com/NhProGamer/orion-drive/pkg/thumb"
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
	ErrNotAFolder       = errors.New("target is not a folder")
	ErrForbidden        = errors.New("this action is not permitted on this share")
	ErrBadPermission    = errors.New("invalid permission")
)

// validPermission reports whether p is one of the accepted permission levels.
func validPermission(p string) bool {
	switch p {
	case model.SharePermRead, model.SharePermWrite, model.SharePermDeposit:
		return true
	}
	return false
}

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
	Permission   string        // "" defaults to read
	Password     string
	ExpiresIn    time.Duration // 0 = never expires
	MaxDownloads int           // 0 = unlimited
}

// Create makes a share link for a file or folder owned by the user.
func (s *Service) Create(ctx context.Context, user *model.User, opts CreateOptions) (*model.Share, error) {
	if user.Group != nil && !user.Group.CanShare() {
		return nil, ErrNotAllowed
	}
	perm := opts.Permission
	if perm == "" {
		perm = model.SharePermRead
	}
	if !validPermission(perm) {
		return nil, ErrBadPermission
	}
	f, err := s.repo.File.GetByID(ctx, user.ID, opts.FileID)
	if err != nil {
		return nil, err
	}
	// A deposit (blind drop box) only makes sense on a folder; read and write
	// apply to both files (write = Office editing) and folders.
	if perm == model.SharePermDeposit && !f.IsFolder() {
		return nil, ErrNotAFolder
	}

	share := &model.Share{
		Token:      randToken(22),
		FileID:     f.ID,
		UserID:     user.ID,
		Permission: perm,
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
	Permission   *string
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
	if opts.Permission != nil {
		if !validPermission(*opts.Permission) {
			return nil, ErrBadPermission
		}
		// Only a deposit share must point at a folder; write applies to files too.
		if *opts.Permission == model.SharePermDeposit {
			f, err := s.repo.File.GetByIDUnscoped(ctx, share.UserID, share.FileID)
			if err != nil {
				return nil, err
			}
			if !f.IsFolder() {
				return nil, ErrNotAFolder
			}
		}
		share.Permission = *opts.Permission
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
	Permission  string `json:"permission"`
	Wopi        bool   `json:"wopi"`        // set by the controller: online Office editing available
	Previewable bool   `json:"previewable"` // a visual thumbnail can be rendered for this file
	HasPassword bool   `json:"has_password"`
	Expired     bool   `json:"expired"`
	Exhausted   bool   `json:"exhausted"`
	Downloads   int    `json:"downloads"`
	Owner       string `json:"owner"`
}

// meta builds the public metadata for a share without recording a view. It
// returns the share alongside the view so callers can act on it (record a view,
// build a preview URL).
func (s *Service) meta(ctx context.Context, token string) (*model.Share, *PublicView, error) {
	share, err := s.repo.Share.GetByToken(ctx, token)
	if err != nil {
		return nil, nil, err
	}
	f, err := s.repo.File.GetByIDUnscoped(ctx, share.UserID, share.FileID)
	if err != nil {
		return nil, nil, err
	}
	view := &PublicView{
		Token:       share.Token,
		Permission:  share.Perm(),
		HasPassword: share.HasPassword(),
		Expired:     share.Expired(),
		Exhausted:   share.Exhausted(),
	}
	// Only reveal the file name, size, owner identity, and download count once the
	// share is actually accessible. For a password-protected or expired share,
	// merely holding the token must not leak this metadata (the client shows a
	// password prompt / expired notice from the flags above instead).
	if !share.HasPassword() && !share.Expired() {
		owner := ""
		if u, err := s.repo.User.GetByID(ctx, share.UserID); err == nil {
			owner = u.DisplayName()
		}
		view.Name = f.Name
		view.IsDir = f.IsFolder()
		view.Size = f.Size
		view.Downloads = share.Downloads
		view.Owner = owner
		// A visual preview is possible only for a downloadable (read/write, not
		// blind-deposit) single file whose type can be thumbnailed.
		if !f.IsFolder() && share.CanDownload() {
			if kind := thumb.Kind(path.Ext(f.Name)); kind != "" && thumb.Available(kind) {
				view.Previewable = true
			}
		}
	}
	return share, view, nil
}

// View returns public metadata for a share and records a view.
func (s *Service) View(ctx context.Context, token string) (*PublicView, error) {
	share, view, err := s.meta(ctx, token)
	if err != nil {
		return nil, err
	}
	_ = s.repo.Share.IncrementViews(ctx, share.ID)
	return view, nil
}

// Meta returns the same public metadata as View but does NOT record a view. Used
// by the server-rendered share preview (OpenGraph tags), which must not inflate
// the view counter on every social-media crawl or page load.
func (s *Service) Meta(ctx context.Context, token string) (*PublicView, error) {
	_, view, err := s.meta(ctx, token)
	return view, err
}

// Thumbnail returns a JPEG preview of a shared file for use as an OpenGraph
// image. It is refused for password-protected, expired or blind (deposit)
// shares — a crawler carries no password, and a protected share must not leak a
// visual — and is not counted as a download.
func (s *Service) Thumbnail(ctx context.Context, token, subPath string) ([]byte, error) {
	share, err := s.repo.Share.GetByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if share.Expired() {
		return nil, ErrExpired
	}
	if share.HasPassword() || !share.CanDownload() {
		return nil, ErrForbidden
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
	return s.files.Thumbnail(ctx, owner, target.ID)
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
	if !share.CanList() {
		return "", nil, ErrForbidden
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
	if !share.CanDownload() {
		return ErrForbidden
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
	if !share.CanDownload() {
		return nil, ErrForbidden
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
	// Atomically reserve the download slot BEFORE serving so concurrent requests
	// can't overshoot the download cap (the earlier Exhausted() check is only a
	// fast fail-early path on a possibly-stale value).
	reserved, err := s.repo.Share.ReserveDownload(ctx, share)
	if err != nil {
		return nil, err
	}
	if !reserved {
		return nil, ErrExhausted
	}
	dt, err := s.files.Download(ctx, owner, target.ID)
	if err != nil {
		_ = s.repo.Share.RefundDownload(ctx, share) // nothing served — give the slot back
		return nil, err
	}
	return dt, nil
}

// Inline authorizes a share and returns a download target for in-browser preview
// of a file at subPath, WITHOUT reserving/counting a download. Previewing is not
// metered (a media player issues many range requests for one view, which would
// otherwise multiply the counter), but it is still gated by the share's
// permission, password/expiry and exhaustion, so an exhausted or protected share
// reveals nothing.
func (s *Service) Inline(ctx context.Context, token, subPath, password string) (*filemanager.DownloadTarget, error) {
	share, err := s.authorize(ctx, token, password)
	if err != nil {
		return nil, err
	}
	if !share.CanDownload() {
		return nil, ErrForbidden
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
	return s.files.Download(ctx, owner, target.ID)
}

// ArchiveTarget authorizes a share, resolves the folder to archive at subPath,
// records the download and returns the owner and target so the caller can stream
// the ZIP itself (headers must be sent before the stream starts).
func (s *Service) ArchiveTarget(ctx context.Context, token, subPath, password string) (*model.User, *model.File, error) {
	share, err := s.authorize(ctx, token, password)
	if err != nil {
		return nil, nil, err
	}
	if !share.CanDownload() {
		return nil, nil, ErrForbidden
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
	// Atomically reserve one download slot (see Download). The archive stream is
	// started by the caller after headers are sent, so a failure there is not
	// refunded — a whole-folder archive counts as one download.
	reserved, err := s.repo.Share.ReserveDownload(ctx, share)
	if err != nil {
		return nil, nil, err
	}
	if !reserved {
		return nil, nil, ErrExhausted
	}
	return owner, target, nil
}

// --- Write access (anonymous visitors on write/deposit shares) ---------------
//
// Every write mirrors the read pattern: it runs "as the owner" (the owner's
// *model.User drives owner_id scoping, quota and storage namespacing) but every
// target is resolved through resolve() from the share root, so nothing outside
// the shared subtree is ever reachable.

// loadOwner returns the share owner's user record.
func (s *Service) loadOwner(ctx context.Context, share *model.Share) (*model.User, error) {
	return s.repo.User.GetByID(ctx, share.UserID)
}

// writeParent authorizes a write share and resolves the folder at subPath that
// the operation targets, ensuring it is inside the subtree and is a folder.
func (s *Service) writeParent(ctx context.Context, token, subPath, password string, need func(*model.Share) bool) (*model.Share, *model.User, *model.File, error) {
	share, err := s.authorize(ctx, token, password)
	if err != nil {
		return nil, nil, nil, err
	}
	if !need(share) {
		return nil, nil, nil, ErrForbidden
	}
	// A blind (deposit) share must never reveal its structure: pin writes to the
	// share root and ignore any subPath, so a visitor can't probe subfolder
	// existence/names via resolve() error differences.
	if share.Blind() {
		subPath = ""
	}
	target, err := s.resolve(ctx, share, subPath)
	if err != nil {
		return nil, nil, nil, err
	}
	if !target.IsFolder() {
		return nil, nil, nil, ErrNotAFolder
	}
	owner, err := s.loadOwner(ctx, share)
	if err != nil {
		return nil, nil, nil, err
	}
	return share, owner, target, nil
}

// CreateFolder creates a folder named name inside the shared folder at subPath.
func (s *Service) CreateFolder(ctx context.Context, token, subPath, name, password string) error {
	_, owner, parent, err := s.writeParent(ctx, token, subPath, password, (*model.Share).CanUpload)
	if err != nil {
		return err
	}
	_, err = s.files.CreateFolder(ctx, owner, &parent.ID, name)
	return err
}

// InitUpload starts an anonymous resumable upload into the shared folder at
// subPath. For a blind (deposit) share the name is made unique so an existing
// file is never silently overwritten by a visitor who cannot see it.
func (s *Service) InitUpload(ctx context.Context, token, subPath, name string, size int64, contributor, password string) (*filemanager.UploadSession, error) {
	share, owner, parent, err := s.writeParent(ctx, token, subPath, password, (*model.Share).CanUpload)
	if err != nil {
		return nil, err
	}
	if share.Blind() {
		name = s.uniqueName(ctx, owner.ID, &parent.ID, strings.TrimSpace(name))
	}
	sess, err := s.files.InitUpload(ctx, owner, &parent.ID, name, size)
	if err != nil {
		return nil, err
	}
	if err := s.files.AttachShare(sess, token, contributor); err != nil {
		return nil, err
	}
	return sess, nil
}

// PutChunk writes one chunk of an anonymous share upload.
func (s *Service) PutChunk(ctx context.Context, token, sid string, index int, r io.Reader) error {
	owner, _, err := s.shareUpload(ctx, token, sid)
	if err != nil {
		return err
	}
	_, err = s.files.PutChunk(ctx, owner, sid, index, r)
	return err
}

// CompleteUpload finalizes an anonymous share upload and tags the resulting file
// with its provenance (contributor name and originating share token).
func (s *Service) CompleteUpload(ctx context.Context, token, sid string) error {
	owner, sess, err := s.shareUpload(ctx, token, sid)
	if err != nil {
		return err
	}
	file, err := s.files.CompleteUpload(ctx, owner, sid)
	if err != nil {
		return err
	}
	s.tagProvenance(ctx, file, token, sess.Contributor)
	return nil
}

// CancelUpload aborts an anonymous share upload.
func (s *Service) CancelUpload(ctx context.Context, token, sid string) error {
	owner, _, err := s.shareUpload(ctx, token, sid)
	if err != nil {
		return err
	}
	return s.files.CancelUpload(owner, sid)
}

// shareUpload loads the owner and the share-scoped upload session, validating
// that the session was created for this token. It revalidates the share on every
// call so an upload started before the share was revoked/expired/downgraded is
// not allowed to finish. The password is not re-checked (the chunk/complete
// endpoints don't carry it), so only deletion/expiry/permission changes stop an
// in-flight upload.
func (s *Service) shareUpload(ctx context.Context, token, sid string) (*model.User, *filemanager.UploadSession, error) {
	share, err := s.repo.Share.GetByToken(ctx, token)
	if err != nil {
		return nil, nil, err
	}
	if share.Expired() {
		return nil, nil, ErrExpired
	}
	if !share.CanUpload() {
		return nil, nil, ErrForbidden
	}
	sess, err := s.files.GetShareSession(token, sid)
	if err != nil {
		return nil, nil, err
	}
	owner, err := s.repo.User.GetByID(ctx, sess.UserID)
	if err != nil {
		return nil, nil, err
	}
	return owner, sess, nil
}

// Rename renames the item at subPath within the shared subtree.
func (s *Service) Rename(ctx context.Context, token, subPath, newName, password string) error {
	share, target, owner, err := s.writeTarget(ctx, token, subPath, password, (*model.Share).CanModify)
	if err != nil {
		return err
	}
	_ = share
	_, err = s.files.Rename(ctx, owner, target.ID, newName)
	return err
}

// Move relocates the item at subPath into the folder at destPath, both inside
// the shared subtree.
func (s *Service) Move(ctx context.Context, token, subPath, destPath, password string) error {
	share, target, owner, err := s.writeTarget(ctx, token, subPath, password, (*model.Share).CanModify)
	if err != nil {
		return err
	}
	dest, err := s.resolve(ctx, share, destPath)
	if err != nil {
		return err
	}
	if !dest.IsFolder() {
		return ErrNotAFolder
	}
	return s.files.Move(ctx, owner, []uint{target.ID}, &dest.ID)
}

// DeleteItem moves the item at subPath to the owner's recycle bin (never a
// purge), so the owner can always restore anything a visitor removes.
func (s *Service) DeleteItem(ctx context.Context, token, subPath, password string) error {
	_, target, owner, err := s.writeTarget(ctx, token, subPath, password, (*model.Share).CanDelete)
	if err != nil {
		return err
	}
	return s.files.Trash(ctx, owner, []uint{target.ID})
}

// writeTarget authorizes a write share and resolves the item at subPath. subPath
// must be non-empty: the share root itself cannot be renamed, moved or deleted
// by a visitor (that would break the share and touch content outside it).
func (s *Service) writeTarget(ctx context.Context, token, subPath, password string, need func(*model.Share) bool) (*model.Share, *model.File, *model.User, error) {
	if cleanPath(subPath) == "" {
		return nil, nil, nil, ErrForbidden
	}
	share, err := s.authorize(ctx, token, password)
	if err != nil {
		return nil, nil, nil, err
	}
	if !need(share) {
		return nil, nil, nil, ErrForbidden
	}
	target, err := s.resolve(ctx, share, subPath)
	if err != nil {
		return nil, nil, nil, err
	}
	owner, err := s.loadOwner(ctx, share)
	if err != nil {
		return nil, nil, nil, err
	}
	return share, target, owner, nil
}

// uniqueName returns name, or the first "name (n).ext" variant that does not yet
// exist under parentID for the owner.
func (s *Service) uniqueName(ctx context.Context, ownerID uint, parentID *uint, name string) string {
	if _, err := s.repo.File.FindChildByName(ctx, ownerID, parentID, name); err != nil {
		return name
	}
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; i < 10000; i++ {
		cand := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := s.repo.File.FindChildByName(ctx, ownerID, parentID, cand); err != nil {
			return cand
		}
	}
	return name
}

// tagProvenance records who contributed a file through a share (best-effort).
func (s *Service) tagProvenance(ctx context.Context, file *model.File, token, contributor string) {
	props := map[string]any{}
	_ = file.Props.Unmarshal(&props)
	props["shared_via"] = token
	if contributor != "" {
		props["created_by"] = contributor
	}
	file.Props = model.MustJSON(props)
	_ = s.repo.File.Update(ctx, file)
}

// OfficeTarget authorizes a share and resolves an Office file within it for
// online editing. Read shares grant view-only editing, write shares grant full
// editing, and deposit (blind) shares are refused. It returns the file id, the
// owner's id (the WOPI token is minted against the owner), the write permission
// and the file name.
func (s *Service) OfficeTarget(ctx context.Context, token, subPath, password string) (fileID, ownerID uint, canWrite bool, name string, err error) {
	share, err := s.authorize(ctx, token, password)
	if err != nil {
		return 0, 0, false, "", err
	}
	if share.Blind() {
		return 0, 0, false, "", ErrForbidden
	}
	target, err := s.resolve(ctx, share, subPath)
	if err != nil {
		return 0, 0, false, "", err
	}
	if target.IsFolder() {
		return 0, 0, false, "", ErrNotAFile
	}
	return target.ID, share.UserID, share.CanModify(), target.Name, nil
}

// WOPIStillValid revalidates a share-originated WOPI session on each host call so
// revocation takes effect within the token lifetime. It returns whether the
// share is still usable (exists, not expired, still grants view/edit) and the
// currently effective write permission (a downgraded share drops to read-only).
// The password is not re-checked — the document server does not carry it — so a
// changed password does not end an in-flight session, only deletion/expiry/
// permission changes do.
func (s *Service) WOPIStillValid(ctx context.Context, token string) (canWrite, ok bool) {
	share, err := s.repo.Share.GetByToken(ctx, token)
	if err != nil || share.Expired() {
		return false, false
	}
	if !share.CanDownload() && !share.CanModify() {
		return false, false // downgraded to a blind/deposit share
	}
	return share.CanModify(), true
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

// randToken returns a URL-safe random token of length n, using rejection
// sampling so every alphabet symbol is equally likely (a plain byte % 62 would
// over-weight the first 8 symbols).
func randToken(n int) string {
	// Largest multiple of the alphabet size that fits in a byte; bytes at or above
	// it are rejected to keep the distribution uniform.
	const limit = 256 - (256 % len(tokenAlphabet))
	out := make([]byte, n)
	buf := make([]byte, n)
	for i := 0; i < n; {
		_, _ = rand.Read(buf)
		for _, b := range buf {
			if int(b) >= limit {
				continue
			}
			out[i] = tokenAlphabet[int(b)%len(tokenAlphabet)]
			if i++; i == n {
				break
			}
		}
	}
	return string(out)
}
