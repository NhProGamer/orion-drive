// Package sftpserver exposes a user's OrionDrive files over SFTP, authenticated
// with the same dedicated credentials as WebDAV. It adapts the virtual
// filesystem (repository + storage driver, via filemanager.Manager) to
// github.com/pkg/sftp's RequestServer handlers.
package sftpserver

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/NhProGamer/orion-drive/pkg/filemanager"
	"github.com/NhProGamer/orion-drive/repository"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh"
)

const (
	extUserID   = "orion-user-id"
	extReadOnly = "orion-readonly"
)

// Server is an SFTP front-end over OrionDrive's Manager.
type Server struct {
	mgr    *filemanager.Manager
	repo   *repository.Repository
	logger *slog.Logger
	addr   string
	config *ssh.ServerConfig
	ln     net.Listener
}

// New builds an SFTP server. The SSH host key is loaded from hostKeyPath, or
// generated (ed25519) and persisted there on first run.
func New(mgr *filemanager.Manager, repo *repository.Repository, logger *slog.Logger, addr, hostKeyPath string) (*Server, error) {
	signer, err := loadOrCreateHostKey(hostKeyPath)
	if err != nil {
		return nil, fmt.Errorf("sftp host key: %w", err)
	}
	s := &Server{mgr: mgr, repo: repo, logger: logger, addr: addr}
	cfg := &ssh.ServerConfig{PasswordCallback: s.authPassword}
	cfg.AddHostKey(signer)
	s.config = cfg
	return s, nil
}

// authPassword validates a WebDAV credential (username + generated password).
func (s *Server) authPassword(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
	ctx := context.Background()
	acct, err := s.repo.WebDAV.GetByUsername(ctx, c.User())
	if err != nil {
		return nil, errors.New("authentication failed")
	}
	if bcrypt.CompareHashAndPassword([]byte(acct.PasswordHash), pass) != nil {
		return nil, errors.New("authentication failed")
	}
	_ = s.repo.WebDAV.TouchLastUsed(ctx, acct.ID)
	ro := "0"
	if acct.ReadOnly {
		ro = "1"
	}
	return &ssh.Permissions{Extensions: map[string]string{
		extUserID:   strconv.FormatUint(uint64(acct.UserID), 10),
		extReadOnly: ro,
	}}, nil
}

// ListenAndServe accepts SSH connections until the listener is closed.
func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.ln = ln
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err // listener closed on shutdown
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(nConn net.Conn) {
	sconn, chans, reqs, err := ssh.NewServerConn(nConn, s.config)
	if err != nil {
		_ = nConn.Close()
		return
	}
	defer sconn.Close()
	go ssh.DiscardRequests(reqs)

	uid, _ := strconv.ParseUint(sconn.Permissions.Extensions[extUserID], 10, 64)
	readOnly := sconn.Permissions.Extensions[extReadOnly] == "1"
	user, err := s.repo.User.GetByID(context.Background(), uint(uid))
	if err != nil {
		return
	}

	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			_ = newChan.Reject(ssh.UnknownChannelType, "only session channels are supported")
			continue
		}
		ch, requests, err := newChan.Accept()
		if err != nil {
			continue
		}
		go func() {
			for req := range requests {
				ok := req.Type == "subsystem" && len(req.Payload) >= 4 && string(req.Payload[4:]) == "sftp"
				_ = req.Reply(ok, nil)
				if !ok {
					continue
				}
				h := newHandlers(s.mgr, s.repo, user, readOnly)
				srv := sftp.NewRequestServer(ch, sftp.Handlers{FileGet: h, FilePut: h, FileCmd: h, FileList: h})
				_ = srv.Serve()
				_ = srv.Close()
				_ = ch.Close()
				return
			}
		}()
	}
}

// Close stops accepting new connections.
func (s *Server) Close() error {
	if s.ln != nil {
		return s.ln.Close()
	}
	return nil
}

// loadOrCreateHostKey returns the SSH host key at path, generating and persisting
// a new ed25519 key if none exists yet.
func loadOrCreateHostKey(path string) (ssh.Signer, error) {
	if data, err := os.ReadFile(path); err == nil {
		return ssh.ParsePrivateKey(data)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(priv)
}
