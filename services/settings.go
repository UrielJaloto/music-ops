package services

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

var (
	ErrSettingUnknown    = errors.New("setting key is unknown")
	ErrSettingKeyMissing = errors.New("setting key is missing")
)

const (
	secretKeySuffix   = "api_key"
	secretMask        = "****"
	secretVisibleTail = 4
)

type SettingsStore interface {
	Values(ctx context.Context) (map[string]string, error)
	Save(ctx context.Context, key, value string) error
	Location() string
	Keys() []string
}

type SettingValueOutput struct {
	Key   string
	Value string
}

type SettingsOutput struct {
	Location string
	Values   []SettingValueOutput
}

type SetSettingInput struct {
	Key   string
	Value string
}

type SettingsLocationOutput struct {
	Path string
}

type SettingsService struct {
	store SettingsStore
}

func NewSettingsService(store SettingsStore) SettingsService {
	return SettingsService{store: store}
}

func (service SettingsService) Show(ctx context.Context) (SettingsOutput, error) {
	values, err := service.store.Values(ctx)
	if err != nil {
		return SettingsOutput{}, fmt.Errorf("reading settings: %w", err)
	}
	keys := service.store.Keys()
	shown := make([]SettingValueOutput, 0, len(keys))
	for _, key := range keys {
		shown = append(shown, SettingValueOutput{Key: key, Value: displayedValue(key, values[key])})
	}
	return SettingsOutput{Location: service.store.Location(), Values: shown}, nil
}

func (service SettingsService) Get(ctx context.Context, key string) (SettingValueOutput, error) {
	if err := service.validateKey(key); err != nil {
		return SettingValueOutput{}, err
	}
	values, err := service.store.Values(ctx)
	if err != nil {
		return SettingValueOutput{}, fmt.Errorf("reading settings: %w", err)
	}
	return SettingValueOutput{Key: key, Value: values[key]}, nil
}

func (service SettingsService) Set(ctx context.Context, input SetSettingInput) error {
	if err := service.validateKey(input.Key); err != nil {
		return err
	}
	if err := service.store.Save(ctx, input.Key, input.Value); err != nil {
		return fmt.Errorf("saving setting %s: %w", input.Key, err)
	}
	return nil
}

func (service SettingsService) Path() SettingsLocationOutput {
	return SettingsLocationOutput{Path: service.store.Location()}
}

func (service SettingsService) validateKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return ErrSettingKeyMissing
	}
	if !slices.Contains(service.store.Keys(), key) {
		return ErrSettingUnknown
	}
	return nil
}

func displayedValue(key, value string) string {
	if !strings.HasSuffix(key, secretKeySuffix) || value == "" {
		return value
	}
	if len(value) < secretVisibleTail {
		return secretMask
	}
	return secretMask + value[len(value)-secretVisibleTail:]
}
