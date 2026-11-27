package ipc

import (
	"context"
	"yaare/internal/domain/preferences"
)

type preferencesRepository interface {
	Preferences(context.Context) (preferences.Snapshot, error)
	SavePreferences(context.Context, preferences.SaveRequest) (preferences.Snapshot, error)
}

type PreferencesService struct{ repository preferencesRepository }

func NewPreferencesService(repository preferencesRepository) *PreferencesService {
	return &PreferencesService{repository: repository}
}

func (s *PreferencesService) Load(ctx context.Context) (preferences.Snapshot, error) {
	if err := workspaceCaller(ctx); err != nil {
		return preferences.Snapshot{}, err
	}
	return s.repository.Preferences(ctx)
}

func (s *PreferencesService) Save(ctx context.Context, request preferences.SaveRequest) (preferences.Snapshot, error) {
	if err := workspaceCaller(ctx); err != nil {
		return preferences.Snapshot{}, err
	}
	return s.repository.SavePreferences(ctx, request)
}
