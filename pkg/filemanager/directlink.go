package filemanager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/NhProGamer/orion-drive/model"
)

// CreateDirectLink creates a stable public link to a file's content.
func (m *Manager) CreateDirectLink(ctx context.Context, user *model.User, fileID uint) (*model.DirectLink, error) {
	f, err := m.repo.File.GetByID(ctx, user.ID, fileID)
	if err != nil {
		return nil, err
	}
	if f.IsFolder() {
		return nil, errors.New("cannot create a direct link to a folder")
	}
	link := &model.DirectLink{
		Token:   directToken(),
		FileID:  f.ID,
		OwnerID: user.ID,
	}
	if err := m.repo.DirectLink.Create(ctx, link); err != nil {
		return nil, err
	}
	return link, nil
}

// ListDirectLinks returns a file's direct links.
func (m *Manager) ListDirectLinks(ctx context.Context, user *model.User, fileID uint) ([]model.DirectLink, error) {
	return m.repo.DirectLink.ListByFile(ctx, user.ID, fileID)
}

// DeleteDirectLink removes one of the user's direct links.
func (m *Manager) DeleteDirectLink(ctx context.Context, user *model.User, token string) error {
	return m.repo.DirectLink.DeleteByToken(ctx, user.ID, token)
}

// DirectDownload resolves a direct link and returns a download target for its
// file, recording the download.
func (m *Manager) DirectDownload(ctx context.Context, token string) (*DownloadTarget, error) {
	link, err := m.repo.DirectLink.GetByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	owner, err := m.repo.User.GetByID(ctx, link.OwnerID)
	if err != nil {
		return nil, err
	}
	target, err := m.Download(ctx, owner, link.FileID)
	if err != nil {
		return nil, err
	}
	_ = m.repo.DirectLink.IncrementDownloads(ctx, link.ID)
	return target, nil
}

// directToken returns a random hex token.
func directToken() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
