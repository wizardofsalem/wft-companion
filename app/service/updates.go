package service

import (
	"context"
	"errors"
	"time"
)

func (s *Service) AskFirst() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.UpdateAskFirst
}

func (s *Service) Consented() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Consent != nil
}

func (s *Service) CheckUpdates() error {
	if s.Updater == nil {
		return errors.New("Updates aren't available in this version.")
	}
	if !s.Consented() {
		return errors.New("Finish setting up first.")
	}
	s.Updater.CheckNow()
	return nil
}

func (s *Service) InstallUpdate() error {
	if s.Updater == nil {
		return errors.New("Updates aren't available in this version.")
	}
	if !s.Consented() {
		return errors.New("Finish setting up first.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	return s.Updater.Install(ctx)
}

func (s *Service) Flush(ctx context.Context) {
	s.mu.Lock()
	s.force = true
	s.mu.Unlock()
	s.Tick(ctx)
}

func (s *Service) SetGoldShared(on bool) error {
	return errors.New("This isn't available yet.")
}
