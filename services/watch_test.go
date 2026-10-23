package services

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"
)

type watcherFake struct {
	existing    []string
	arrivals    []string
	existingErr error
	arrivalsErr error
	keepOpen    bool
}

func (fake watcherFake) Existing(context.Context, string) ([]string, error) {
	return fake.existing, fake.existingErr
}

func (fake watcherFake) Arrivals(context.Context, string) (<-chan string, error) {
	if fake.arrivalsErr != nil {
		return nil, fake.arrivalsErr
	}
	channel := make(chan string, len(fake.arrivals))
	for _, folder := range fake.arrivals {
		channel <- folder
	}
	if !fake.keepOpen {
		close(channel)
	}
	return channel, nil
}

type folderImporterFake struct {
	exitStatus map[string]int
	errors     map[string]error
	imported   []string
	cancelOn   string
	cancel     context.CancelFunc
}

func (fake *folderImporterFake) Import(_ context.Context, input ImportInput) (ImportOutput, error) {
	fake.imported = append(fake.imported, input.Folder)
	if input.Folder == fake.cancelOn {
		fake.cancel()
	}
	return ImportOutput{ExitStatus: fake.exitStatus[input.Folder]}, fake.errors[input.Folder]
}

type archiverSpy struct {
	archived [][2]string
	err      error
}

func (spy *archiverSpy) Archive(_ context.Context, folder, destination string) error {
	spy.archived = append(spy.archived, [2]string{folder, destination})
	return spy.err
}

type stabilityFake struct {
	unstableChecks map[string]int
	err            error
	checks         map[string]int
}

func (fake *stabilityFake) IsStable(_ context.Context, folder string, _ time.Duration) (bool, error) {
	fake.checks[folder]++
	return fake.checks[folder] > fake.unstableChecks[folder], fake.err
}

type watchProgressSpy struct {
	events []string
}

func (spy *watchProgressSpy) FolderImported(folder string, exitStatus int) {
	spy.events = append(spy.events, "imported "+folder+" "+strconv.Itoa(exitStatus))
}

func (spy *watchProgressSpy) FolderFailed(folder, reason string) {
	spy.events = append(spy.events, "failed "+folder+" "+reason)
}

type watchDoubles struct {
	watcher   watcherFake
	importer  *folderImporterFake
	archiver  *archiverSpy
	stability *stabilityFake
	progress  *watchProgressSpy
}

func newWatchDoubles() watchDoubles {
	return watchDoubles{
		importer:  &folderImporterFake{exitStatus: map[string]int{}, errors: map[string]error{}},
		archiver:  &archiverSpy{},
		stability: &stabilityFake{unstableChecks: map[string]int{}, checks: map[string]int{}},
		progress:  &watchProgressSpy{},
	}
}

func (doubles watchDoubles) service() WatchService {
	return NewWatchService(doubles.watcher, doubles.importer, doubles.archiver, doubles.stability, doubles.progress)
}

func watchInput() WatchInput {
	return WatchInput{Inbox: "inbox", Import: ImportInput{Output: "out"}, QuietPeriod: time.Second}
}

func TestWatchService_Watch_ExistingChildrenProcessedFirst(t *testing.T) {
	doubles := newWatchDoubles()
	doubles.watcher = watcherFake{existing: []string{"inbox/old"}, arrivals: []string{"inbox/new"}}

	err := doubles.service().Watch(context.Background(), watchInput())

	if err != nil || !slices.Equal(doubles.importer.imported, []string{"inbox/old", "inbox/new"}) {
		t.Fatalf("imported = %v err = %v", doubles.importer.imported, err)
	}
}

func TestWatchService_Watch_Routing(t *testing.T) {
	cases := []struct {
		name                string
		exitStatus          int
		importErr           error
		expectedDestination string
		expectedEvent       string
	}{
		{name: "Success_ArchivedToProcessed", exitStatus: 0, expectedDestination: "inbox/_processed", expectedEvent: "imported inbox/a 0"},
		{name: "ExitStatusTwo_ArchivedToFailed", exitStatus: 2, expectedDestination: "inbox/_failed", expectedEvent: "imported inbox/a 2"},
		{name: "ImporterError_ArchivedToFailed", importErr: errTechnical, expectedDestination: "inbox/_failed", expectedEvent: "failed inbox/a technical failure"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			doubles := newWatchDoubles()
			doubles.watcher = watcherFake{arrivals: []string{"inbox/a"}}
			doubles.importer.exitStatus["inbox/a"] = testCase.exitStatus
			doubles.importer.errors["inbox/a"] = testCase.importErr

			err := doubles.service().Watch(context.Background(), watchInput())

			if err != nil || !slices.Equal(doubles.archiver.archived, [][2]string{{"inbox/a", testCase.expectedDestination}}) {
				t.Fatalf("archived = %v err = %v", doubles.archiver.archived, err)
			}
			if !slices.Equal(doubles.progress.events, []string{testCase.expectedEvent}) {
				t.Fatalf("events = %v", doubles.progress.events)
			}
		})
	}
}

func TestWatchService_Watch_UnstableFolderRetried(t *testing.T) {
	doubles := newWatchDoubles()
	doubles.watcher = watcherFake{arrivals: []string{"inbox/a"}}
	doubles.stability.unstableChecks["inbox/a"] = 2

	err := doubles.service().Watch(context.Background(), watchInput())

	if err != nil || doubles.stability.checks["inbox/a"] != 3 || len(doubles.importer.imported) != 1 {
		t.Fatalf("checks = %v imported = %v err = %v", doubles.stability.checks, doubles.importer.imported, err)
	}
}

func TestWatchService_Watch_StabilityError_ReportsAndContinues(t *testing.T) {
	doubles := newWatchDoubles()
	doubles.watcher = watcherFake{arrivals: []string{"inbox/a"}}
	doubles.stability.err = errTechnical

	err := doubles.service().Watch(context.Background(), watchInput())

	if err != nil || len(doubles.importer.imported) != 0 || len(doubles.progress.events) != 1 {
		t.Fatalf("events = %v err = %v", doubles.progress.events, err)
	}
}

func TestWatchService_Watch_ArchiveFoldersIgnored(t *testing.T) {
	doubles := newWatchDoubles()
	doubles.watcher = watcherFake{existing: []string{"inbox/_processed", `inbox\_failed`, "inbox/a"}}

	err := doubles.service().Watch(context.Background(), watchInput())

	if err != nil || !slices.Equal(doubles.importer.imported, []string{"inbox/a"}) {
		t.Fatalf("imported = %v err = %v", doubles.importer.imported, err)
	}
}

func TestWatchService_Watch_ArchiveError_IsReported(t *testing.T) {
	doubles := newWatchDoubles()
	doubles.watcher = watcherFake{arrivals: []string{"inbox/a"}}
	doubles.archiver.err = errTechnical

	err := doubles.service().Watch(context.Background(), watchInput())

	expected := []string{"imported inbox/a 0", "failed inbox/a archiving: technical failure"}
	if err != nil || !slices.Equal(doubles.progress.events, expected) {
		t.Fatalf("events = %v err = %v", doubles.progress.events, err)
	}
}

func TestWatchService_Watch_CancelReturnsNil(t *testing.T) {
	cases := []struct {
		name     string
		arrivals []string
		cancelOn string
	}{
		{name: "CancelWhileWaiting", arrivals: nil},
		{name: "CancelDuringImport", arrivals: []string{"inbox/a", "inbox/b"}, cancelOn: "inbox/a"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			doubles := newWatchDoubles()
			doubles.watcher = watcherFake{arrivals: testCase.arrivals, keepOpen: true}
			doubles.importer.cancelOn, doubles.importer.cancel = testCase.cancelOn, cancel
			if testCase.cancelOn == "" {
				cancel()
			}

			err := doubles.service().Watch(ctx, watchInput())

			if err != nil || len(doubles.archiver.archived) != 0 || slices.Contains(doubles.importer.imported, "inbox/b") {
				t.Fatalf("err = %v archived = %v imported = %v", err, doubles.archiver.archived, doubles.importer.imported)
			}
		})
	}
}

func TestWatchService_Watch_Errors(t *testing.T) {
	cases := []struct {
		name          string
		input         WatchInput
		watcher       watcherFake
		expectedError error
	}{
		{name: "EmptyInbox_ReturnsErrFolderMissing", input: WatchInput{}, expectedError: ErrFolderMissing},
		{name: "ExistingError_IsReturned", input: watchInput(), watcher: watcherFake{existingErr: errTechnical}, expectedError: errTechnical},
		{name: "WatcherError_IsReturned", input: watchInput(), watcher: watcherFake{arrivalsErr: errTechnical}, expectedError: errTechnical},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			doubles := newWatchDoubles()
			doubles.watcher = testCase.watcher

			err := doubles.service().Watch(context.Background(), testCase.input)

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
