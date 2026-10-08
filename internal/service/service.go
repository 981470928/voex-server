// Package service implements business rules independently of the HTTP framework.
package service

import (
	"context"
	"gorm.io/gorm"
	"voex-server/internal/filestore"
	"voex-server/internal/repository"
)

type Service struct {
	db        *gorm.DB
	secret    []byte
	hashSlots chan struct{}
	dummyHash string
	Files     *filestore.Store
}

func New(db *gorm.DB, files *filestore.Store, secret []byte) *Service {
	return &Service{db: db, Files: files, secret: secret, hashSlots: make(chan struct{}, 4), dummyHash: hashPassword(randomPassword())}
}
func (s *Service) tx(ctx context.Context, write bool, fn func(*gorm.DB) any) any {
	return repository.Transaction(ctx, s.db, write, fn)
}
