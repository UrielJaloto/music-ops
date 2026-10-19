package domain

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

var (
	ErrTrackPathMissing         = newBusinessError("track source path is missing")
	ErrTrackTransitionForbidden = newBusinessError("track state transition is forbidden")
	ErrTrackPositionOutOfRange  = newBusinessError("track position out of range")
)

const (
	trackIDLength     = 16
	NoCoverStream     = -1
	positionSeparator = "/"
)

type TrackID string

func NewTrackID(path string, size int64, modifiedAt time.Time) TrackID {
	digest := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d", path, size, modifiedAt.UnixNano())))
	return TrackID(hex.EncodeToString(digest[:])[:trackIDLength])
}

type TrackStatus int

const (
	StatusDiscovered TrackStatus = iota
	StatusConverted
	StatusResolved
	StatusReady
	StatusPublished
	StatusDuplicate
	StatusSkipped
	StatusFailed
)

var terminalStatuses = map[TrackStatus]bool{
	StatusPublished: true,
	StatusDuplicate: true,
	StatusSkipped:   true,
	StatusFailed:    true,
}

type SourcePosition struct {
	TrackNumber int
	TrackTotal  int
	DiscNumber  int
	DiscTotal   int
}

type SourceTags struct {
	Title       string
	Artist      string
	AlbumArtist string
	Album       string
	Date        string
	Composer    string
	Copyright   string
	Position    SourcePosition
}

type CoverArt struct {
	Stream int
	Codec  string
}

type SourceDetails struct {
	Path         string
	RelativePath string
	Directory    string
	Codec        Codec
	Quality      AudioQuality
	Tags         SourceTags
	Cover        CoverArt
}

type TrackPosition struct {
	Number int
	Total  int
}

type TrackOutcome struct {
	KeeperPath   string
	FailureStage string
	Reason       string
}

type TrackMetadata struct {
	Title       string
	Artist      string
	AlbumArtist string
	Album       string
	Genre       string
	Track       string
	Disc        string
	Date        string
	Composer    string
	Copyright   string
}

type trackResolution struct {
	credit         Credit
	classification Classification
	position       TrackPosition
}

type Track struct {
	id         TrackID
	source     SourceDetails
	status     TrackStatus
	resolution trackResolution
	target     TargetPath
	outcome    TrackOutcome
}

func NewTrack(id TrackID, source SourceDetails) (Track, error) {
	if source.Path == "" {
		return Track{}, ErrTrackPathMissing
	}
	return Track{id: id, source: source, status: StatusDiscovered}, nil
}

func (track *Track) MarkConverted() error {
	next, isAllowed := map[TrackStatus]TrackStatus{StatusDiscovered: StatusConverted, StatusResolved: StatusReady}[track.status]
	if !isAllowed {
		return ErrTrackTransitionForbidden
	}
	track.status = next
	return nil
}

func (track *Track) Resolve(credit Credit, classification Classification) error {
	next, isAllowed := map[TrackStatus]TrackStatus{StatusDiscovered: StatusResolved, StatusConverted: StatusReady}[track.status]
	if !isAllowed {
		return ErrTrackTransitionForbidden
	}
	track.resolution.credit = credit
	track.resolution.classification = classification
	track.status = next
	return nil
}

func (track *Track) AssignPosition(position, total int) error {
	if track.status != StatusResolved && track.status != StatusReady {
		return ErrTrackTransitionForbidden
	}
	if position < 1 || position > total {
		return ErrTrackPositionOutOfRange
	}
	track.resolution.position = TrackPosition{Number: position, Total: total}
	return nil
}

func (track *Track) AssignTarget(target TargetPath) error {
	if track.status != StatusReady {
		return ErrTrackTransitionForbidden
	}
	track.target = target
	return nil
}

func (track *Track) MarkPublished() error {
	if !track.CanPublish() {
		return ErrTrackTransitionForbidden
	}
	track.status = StatusPublished
	return nil
}

func (track *Track) MarkDuplicate(keeper string) error {
	if track.status != StatusReady {
		return ErrTrackTransitionForbidden
	}
	track.outcome.KeeperPath = keeper
	track.status = StatusDuplicate
	return nil
}

func (track *Track) Skip(reason string) error {
	if track.status != StatusDiscovered {
		return ErrTrackTransitionForbidden
	}
	track.outcome.Reason = reason
	track.status = StatusSkipped
	return nil
}

func (track *Track) Fail(stage, reason string) error {
	if track.IsTerminal() {
		return ErrTrackTransitionForbidden
	}
	track.outcome.FailureStage = stage
	track.outcome.Reason = reason
	track.status = StatusFailed
	return nil
}

func (track Track) ID() TrackID {
	return track.id
}

func (track Track) Status() TrackStatus {
	return track.status
}

func (track Track) Source() SourceDetails {
	return track.source
}

func (track Track) Credit() Credit {
	return track.resolution.credit
}

func (track Track) Classification() Classification {
	return track.resolution.classification
}

func (track Track) Genre() Genre {
	return track.resolution.classification.Genre()
}

func (track Track) Position() TrackPosition {
	return track.resolution.position
}

func (track Track) Target() TargetPath {
	return track.target
}

func (track Track) Outcome() TrackOutcome {
	return track.outcome
}

func (track Track) IsTerminal() bool {
	return terminalStatuses[track.status]
}

func (track Track) CanPublish() bool {
	return track.status == StatusReady && track.target.Relative() != ""
}

func (track Track) Metadata() TrackMetadata {
	tags := track.source.Tags
	credit := track.resolution.credit
	return TrackMetadata{
		Title:       credit.Title().String(),
		Artist:      credit.Artists().String(),
		AlbumArtist: mainArtistOf(credit),
		Album:       tags.Album,
		Genre:       track.Genre().String(),
		Track:       formatPosition(track.resolution.position.Number, track.resolution.position.Total),
		Disc:        formatPosition(tags.Position.DiscNumber, tags.Position.DiscTotal),
		Date:        tags.Date,
		Composer:    tags.Composer,
		Copyright:   tags.Copyright,
	}
}

func mainArtistOf(credit Credit) string {
	if len(credit.Artists().artists) == 0 {
		return ""
	}
	return credit.Artists().MainArtist()
}

func formatPosition(number, total int) string {
	if number <= 0 {
		return ""
	}
	if total <= 0 {
		return strconv.Itoa(number)
	}
	return strconv.Itoa(number) + positionSeparator + strconv.Itoa(total)
}
