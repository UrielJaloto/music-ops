package domain

import (
	"slices"
	"testing"
)

func TestTagTargets_AreValidInPool(t *testing.T) {
	if len(tagTargets) < 60 {
		t.Fatalf("tag map has %d entries, want at least 60", len(tagTargets))
	}
	for tag, target := range tagTargets {
		details := ClassificationDetails{Genre: target.genre, Subgenre: target.subgenre, IsKnown: true}

		if _, err := NewClassification(details); err != nil {
			t.Errorf("tag %q maps to invalid target %+v: %v", tag, target, err)
		}
	}
}

func TestNewTagEvidence(t *testing.T) {
	evidence := NewTagEvidence([]string{" Rock ", "rock", "", "MPB", "Seen Live"})

	if !slices.Equal(evidence.Tags(), []string{"rock", "mpb", "seen live"}) {
		t.Fatalf("tags = %#v", evidence.Tags())
	}
	evidence.Tags()[0] = "changed"
	if evidence.Tags()[0] != "rock" {
		t.Fatal("tags are not copied on read")
	}
}

func TestClassifyByTags(t *testing.T) {
	cases := []struct {
		name     string
		tags     []string
		expected string
	}{
		{"BrazilianRap", []string{"rap", "hip-hop", "brazilian rap", "rap nacional"}, "Rap Nacional"},
		{"BrazilianMetal", []string{"thrash metal", "metal", "brazilian"}, "Rock Nacional; Metal Nacional"},
		{"BrazilianPop", []string{"pop", "brasil"}, "Pop Nacional"},
		{"Samba", []string{"samba", "mpb", "brazil"}, "Samba"},
		{"AFI", []string{"punk rock", "rock", "alternative"}, "Rock; Punk"},
		{"AlexGaudino", []string{"house", "dance"}, "Electronic; House"},
		{"FirstMatchWins", []string{"seen live", "dance", "house"}, "Electronic; Dance"},
		{"FunkCarioca", []string{"funk carioca", "baile funk"}, "Funk BR; Funk Carioca"},
		{"NoMapping_IsUnknown", []string{"seen live", "favorites"}, "desconhecido"},
		{"NoTags_IsUnknown", []string{}, "desconhecido"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			classification := ClassifyByTags(NewTagEvidence(testCase.tags))

			if got := classification.Genre().String(); got != testCase.expected {
				t.Fatalf("genre = %q, want %q", got, testCase.expected)
			}
		})
	}
}

func TestHasBrazilianSignal(t *testing.T) {
	cases := []struct {
		tag      string
		expected bool
	}{
		{"brazil", true},
		{"brazilian rock", true},
		{"brasil", true},
		{"rap nacional", true},
		{"mpb", true},
		{"funk carioca", true},
		{"sertanejo", true},
		{"pagode", true},
		{"samba", true},
		{"rock", false},
		{"funk", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.tag, func(t *testing.T) {
			if got := NewTagEvidence([]string{testCase.tag}).HasBrazilianSignal(); got != testCase.expected {
				t.Fatalf("HasBrazilianSignal = %v, want %v", got, testCase.expected)
			}
		})
	}
}

func TestResolveNationality(t *testing.T) {
	rap := mustClassification(t, ClassificationDetails{Genre: "Rap", IsKnown: true})
	brazilianRap := rap.WithNationality(true)
	signal := NewTagEvidence([]string{"brasil"})
	none := NewTagEvidence([]string{"rap"})
	cases := []struct {
		name     string
		ballots  []NationalityBallot
		expected []bool
	}{
		{"GEN8_MajorityTrue", []NationalityBallot{{"A", brazilianRap, none}, {"a", brazilianRap, none}, {"A ", rap, none}}, []bool{true, true, true}},
		{"GEN8_MajorityFalse", []NationalityBallot{{"A", rap, none}, {"A", rap, none}, {"A", brazilianRap, none}}, []bool{false, false, false}},
		{"GEN8_TieWithSignal", []NationalityBallot{{"A", rap, signal}, {"A", brazilianRap, none}}, []bool{true, true}},
		{"GEN8_TieWithoutSignal", []NationalityBallot{{"A", rap, none}, {"A", brazilianRap, none}}, []bool{false, false}},
		{"GEN8_DifferentArtistsIndependent", []NationalityBallot{{"A", brazilianRap, none}, {"B", rap, none}}, []bool{true, false}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			resolved := ResolveNationality(testCase.ballots)

			got := make([]bool, 0, len(resolved))
			for _, classification := range resolved {
				got = append(got, classification.IsBrazilian())
			}
			if !slices.Equal(got, testCase.expected) {
				t.Fatalf("nationality = %v, want %v", got, testCase.expected)
			}
		})
	}
}

func TestResolveNationality_InputUnchanged(t *testing.T) {
	rap := mustClassification(t, ClassificationDetails{Genre: "Rap", IsKnown: true})
	ballots := []NationalityBallot{{"A", rap.WithNationality(true), NewTagEvidence(nil)}, {"A", rap.WithNationality(true), NewTagEvidence(nil)}, {"A", rap, NewTagEvidence(nil)}}

	ResolveNationality(ballots)

	if ballots[2].Classification.IsBrazilian() {
		t.Fatal("input ballot was changed")
	}
}

func TestResolveNationality_EmptyInput(t *testing.T) {
	resolved := ResolveNationality(nil)

	if resolved == nil || len(resolved) != 0 {
		t.Fatalf("resolved = %#v, want empty non-nil", resolved)
	}
}
