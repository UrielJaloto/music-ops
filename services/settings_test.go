package services

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type settingsStoreFake struct {
	values  map[string]string
	readErr error
	saveErr error
	saved   []SetSettingInput
}

func (fake *settingsStoreFake) Values(context.Context) (map[string]string, error) {
	return fake.values, fake.readErr
}

func (fake *settingsStoreFake) Save(_ context.Context, key, value string) error {
	fake.saved = append(fake.saved, SetSettingInput{Key: key, Value: value})
	return fake.saveErr
}

func (fake *settingsStoreFake) Location() string {
	return "C:/config.yaml"
}

func (fake *settingsStoreFake) Keys() []string {
	return []string{"output.dir", "lastfm.api_key"}
}

func TestSettingsService_Show(t *testing.T) {
	cases := []struct {
		name        string
		apiKey      string
		expectedKey string
	}{
		{name: "LongKey_ShowsLastFour", apiKey: "abcdef123456", expectedKey: "****3456"},
		{name: "ExactlyFour_ShowsTail", apiKey: "abcd", expectedKey: "****abcd"},
		{name: "ShortKey_IsFullyMasked", apiKey: "abc", expectedKey: "****"},
		{name: "EmptyKey_StaysEmpty", apiKey: "", expectedKey: ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &settingsStoreFake{values: map[string]string{"output.dir": "D:/Music", "lastfm.api_key": testCase.apiKey}}

			output, err := NewSettingsService(store).Show(context.Background())

			expected := []SettingValueOutput{{Key: "output.dir", Value: "D:/Music"}, {Key: "lastfm.api_key", Value: testCase.expectedKey}}
			if err != nil || output.Location != "C:/config.yaml" || !slices.Equal(output.Values, expected) {
				t.Fatalf("output = %+v err = %v", output, err)
			}
		})
	}
}

func TestSettingsService_Show_StoreError(t *testing.T) {
	_, err := NewSettingsService(&settingsStoreFake{readErr: errTechnical}).Show(context.Background())

	if !errors.Is(err, errTechnical) {
		t.Fatalf("error = %v", err)
	}
}

func TestSettingsService_Get(t *testing.T) {
	cases := []struct {
		name          string
		key           string
		readErr       error
		expected      SettingValueOutput
		expectedError error
	}{
		{name: "KnownKey_ReturnsValue", key: "output.dir", expected: SettingValueOutput{Key: "output.dir", Value: "D:/Music"}},
		{name: "SecretKey_ReturnsRawValue", key: "lastfm.api_key", expected: SettingValueOutput{Key: "lastfm.api_key", Value: "secret1234"}},
		{name: "EmptyKey_ReturnsErrSettingKeyMissing", key: " ", expectedError: ErrSettingKeyMissing},
		{name: "UnknownKey_ReturnsErrSettingUnknown", key: "output.color", expectedError: ErrSettingUnknown},
		{name: "StoreError_ReturnsWrappedError", key: "output.dir", readErr: errTechnical, expectedError: errTechnical},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &settingsStoreFake{values: map[string]string{"output.dir": "D:/Music", "lastfm.api_key": "secret1234"}, readErr: testCase.readErr}

			value, err := NewSettingsService(store).Get(context.Background(), testCase.key)

			if !errors.Is(err, testCase.expectedError) || value != testCase.expected {
				t.Fatalf("value = %+v err = %v", value, err)
			}
		})
	}
}

func TestSettingsService_Set(t *testing.T) {
	cases := []struct {
		name          string
		input         SetSettingInput
		saveErr       error
		expectedSaves int
		expectedError error
	}{
		{name: "KnownKey_IsSaved", input: SetSettingInput{Key: "output.dir", Value: "E:/Music"}, expectedSaves: 1},
		{name: "EmptyKey_ReturnsErrSettingKeyMissing", input: SetSettingInput{Value: "x"}, expectedError: ErrSettingKeyMissing},
		{name: "UnknownKey_ReturnsErrSettingUnknown", input: SetSettingInput{Key: "x.y", Value: "x"}, expectedError: ErrSettingUnknown},
		{name: "StoreRejectsValue_PropagatesError", input: SetSettingInput{Key: "output.dir", Value: "?"}, saveErr: errTechnical, expectedSaves: 1, expectedError: errTechnical},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &settingsStoreFake{saveErr: testCase.saveErr}

			err := NewSettingsService(store).Set(context.Background(), testCase.input)

			if !errors.Is(err, testCase.expectedError) || len(store.saved) != testCase.expectedSaves {
				t.Fatalf("err = %v saves = %v", err, store.saved)
			}
			if testCase.expectedSaves == 1 && store.saved[0] != testCase.input {
				t.Fatalf("saved = %+v", store.saved[0])
			}
		})
	}
}

func TestSettingsService_Path(t *testing.T) {
	if path := NewSettingsService(&settingsStoreFake{}).Path(); path.Path != "C:/config.yaml" {
		t.Fatalf("path = %+v", path)
	}
}
