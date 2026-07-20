package webdav

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"path"
	"strings"
	"time"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/repository"
	xwebdav "golang.org/x/net/webdav"
)

// Lock timeouts: a zero/infinite requested timeout falls back to defaultLockTTL,
// and no lock outlives maxLockTTL, so an abandoned lock cannot wedge a resource.
const (
	defaultLockTTL = 30 * time.Minute
	maxLockTTL     = 7 * 24 * time.Hour
)

// dbLocks is a database-backed xwebdav.LockSystem scoped to a single user. It is
// built per request (with the authenticated user's id) so locks are isolated
// per user yet persisted and shared across nodes. The LockSystem interface has
// no context, so database calls use context.Background.
type dbLocks struct {
	repo   *repository.Repository
	userID uint
}

func newLocks(repo *repository.Repository, userID uint) *dbLocks {
	return &dbLocks{repo: repo, userID: userID}
}

func (l *dbLocks) active(now time.Time) []model.WebDAVLock {
	out, _ := l.repo.WebDAVLock.ListActive(context.Background(), l.userID, now)
	return out
}

// ttl clamps a requested duration to the allowed range.
func ttl(d time.Duration) time.Duration {
	if d <= 0 || d > maxLockTTL {
		return defaultLockTTL
	}
	return d
}

// cleanName normalises a WebDAV name to an absolute, cleaned path.
func cleanName(name string) string {
	return path.Clean("/" + strings.TrimSpace(name))
}

// isAncestor reports whether a is p or an ancestor directory of p.
func isAncestor(a, p string) bool {
	if a == p {
		return true
	}
	if a == "/" {
		return true
	}
	return strings.HasPrefix(p, a+"/")
}

// Create takes a new lock, rejecting it when it conflicts with an existing lock
// (the same path, an ancestor's infinite-depth lock, or — for an infinite-depth
// request — any descendant lock).
func (l *dbLocks) Create(now time.Time, details xwebdav.LockDetails) (string, error) {
	root := cleanName(details.Root)
	depth := -1
	if details.ZeroDepth {
		depth = 0
	}
	ctx := context.Background()
	_ = l.repo.WebDAVLock.DeleteExpired(ctx, now) // opportunistic cleanup
	for _, lk := range l.active(now) {
		if lk.Path == root ||
			(lk.Depth == -1 && isAncestor(lk.Path, root)) ||
			(depth == -1 && isAncestor(root, lk.Path)) {
			return "", xwebdav.ErrLocked
		}
	}
	token := "opaquelocktoken:" + randToken()
	lock := &model.WebDAVLock{
		Token:   token,
		UserID:  l.userID,
		Path:    root,
		Depth:   depth,
		Owner:   details.OwnerXML,
		Expires: now.Add(ttl(details.Duration)),
	}
	if err := l.repo.WebDAVLock.Create(ctx, lock); err != nil {
		return "", err
	}
	return token, nil
}

// Refresh extends a lock's timeout.
func (l *dbLocks) Refresh(now time.Time, token string, duration time.Duration) (xwebdav.LockDetails, error) {
	ctx := context.Background()
	lk, err := l.repo.WebDAVLock.GetByToken(ctx, l.userID, token)
	if err != nil || !lk.Expires.After(now) {
		return xwebdav.LockDetails{}, xwebdav.ErrNoSuchLock
	}
	expires := now.Add(ttl(duration))
	if err := l.repo.WebDAVLock.Refresh(ctx, l.userID, token, expires); err != nil {
		return xwebdav.LockDetails{}, err
	}
	return xwebdav.LockDetails{
		Root:      lk.Path,
		Duration:  expires.Sub(now),
		OwnerXML:  lk.Owner,
		ZeroDepth: lk.Depth == 0,
	}, nil
}

// Unlock releases a lock by token.
func (l *dbLocks) Unlock(now time.Time, token string) error {
	ctx := context.Background()
	if _, err := l.repo.WebDAVLock.GetByToken(ctx, l.userID, token); err != nil {
		return xwebdav.ErrNoSuchLock
	}
	return l.repo.WebDAVLock.DeleteByToken(ctx, l.userID, token)
}

// Confirm verifies that the client holds the locks needed to modify name0 (and
// name1 for MOVE/COPY): every lock covering a target must be named by one of the
// If-header conditions. It returns a no-op release (locks are not ref-counted;
// enforcement is per operation).
func (l *dbLocks) Confirm(now time.Time, name0, name1 string, conditions ...xwebdav.Condition) (func(), error) {
	active := l.active(now)
	for _, name := range []string{name0, name1} {
		if name == "" {
			continue
		}
		p := cleanName(name)
		for _, lk := range active {
			covers := lk.Path == p || (lk.Depth == -1 && isAncestor(lk.Path, p))
			if covers && !conditionSatisfied(conditions, lk.Token) {
				return nil, xwebdav.ErrConfirmationFailed
			}
		}
	}
	return func() {}, nil
}

// conditionSatisfied reports whether the If-header conditions assert the token.
func conditionSatisfied(conditions []xwebdav.Condition, token string) bool {
	for _, c := range conditions {
		if !c.Not && c.Token == token {
			return true
		}
	}
	return false
}

func randToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
