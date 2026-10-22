package service

import (
	"context"
	"os"
	"voex-server/internal/config"
	"voex-server/internal/database"
)

func (s *Service) Readiness(ctx context.Context) error {
	pool, err := s.db.DB()
	if err != nil {
		return err
	}
	if err = pool.PingContext(ctx); err != nil {
		return err
	}
	if err = database.Validate(s.db.WithContext(ctx)); err != nil {
		return err
	}
	_, err = os.Stat(config.Env("VOEX_STORAGE_ROOT", "/home/static"))
	return err
}
