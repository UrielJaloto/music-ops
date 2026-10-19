package domain

import (
	"errors"
	"testing"
)

func TestNewClassification(t *testing.T) {
	cases := []struct {
		name          string
		details       ClassificationDetails
		expectedGenre string
		expectedError error
	}{
		{name: "UnknownGenre_ReturnsErrGenreNotInPool", details: ClassificationDetails{Genre: "Polka", IsKnown: true}, expectedError: ErrGenreNotInPool},
		{name: "WrongCase_ReturnsErrGenreNotInPool", details: ClassificationDetails{Genre: "rock", IsKnown: true}, expectedError: ErrGenreNotInPool},
		{name: "SubgenreOfOtherGenre_ReturnsErrSubgenreNotInGenre", details: ClassificationDetails{Genre: "Rock", Subgenre: "House", IsKnown: true}, expectedError: ErrSubgenreNotInGenre},
		{name: "SubgenreWrongCase_ReturnsErrSubgenreNotInGenre", details: ClassificationDetails{Genre: "Rock", Subgenre: "metal", IsKnown: true}, expectedError: ErrSubgenreNotInGenre},
		{name: "ValidGenreAndSubgenre_IsAccepted", details: ClassificationDetails{Genre: "Rock", Subgenre: "Metal", IsKnown: true}, expectedGenre: "Rock; Metal"},
		{name: "GEN6_NotKnownIgnoresInvalidFields", details: ClassificationDetails{Genre: "Polka", Subgenre: "x", IsKnown: false}, expectedGenre: "desconhecido"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			classification, err := NewClassification(testCase.details)

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v, want %v", err, testCase.expectedError)
			}
			if err == nil && classification.Genre().String() != testCase.expectedGenre {
				t.Fatalf("genre = %q, want %q", classification.Genre().String(), testCase.expectedGenre)
			}
		})
	}
}

func TestClassificationGenre(t *testing.T) {
	cases := []struct {
		name     string
		details  ClassificationDetails
		expected string
	}{
		{"GEN3_SambaBrazilianNotSuffixed", ClassificationDetails{Genre: "Samba", IsBrazilian: true, IsKnown: true}, "Samba"},
		{"GEN3_FunkBRNotSuffixed", ClassificationDetails{Genre: "Funk BR", IsBrazilian: true, IsKnown: true}, "Funk BR"},
		{"GEN3_FunkCariocaBrazilian", ClassificationDetails{Genre: "Funk BR", Subgenre: "Funk Carioca", IsBrazilian: true, IsKnown: true}, "Funk BR; Funk Carioca"},
		{"GEN3_FunkCariocaNotBrazilian", ClassificationDetails{Genre: "Funk BR", Subgenre: "Funk Carioca", IsBrazilian: false, IsKnown: true}, "Funk BR; Funk Carioca"},
		{"GEN4_BrazilianRap", ClassificationDetails{Genre: "Rap", IsBrazilian: true, IsKnown: true}, "Rap Nacional"},
		{"GEN4_BrazilianRockMetal", ClassificationDetails{Genre: "Rock", Subgenre: "Metal", IsBrazilian: true, IsKnown: true}, "Rock Nacional; Metal Nacional"},
		{"GEN4_BrazilianPop", ClassificationDetails{Genre: "Pop", IsBrazilian: true, IsKnown: true}, "Pop Nacional"},
		{"GEN5_InternationalRockPunk", ClassificationDetails{Genre: "Rock", Subgenre: "Punk", IsKnown: true}, "Rock; Punk"},
		{"GEN5_ElectronicDance", ClassificationDetails{Genre: "Electronic", Subgenre: "Dance", IsKnown: true}, "Electronic; Dance"},
		{"GEN5_GenreWithoutSubgenre", ClassificationDetails{Genre: "Jazz / Blues", IsKnown: true}, "Jazz / Blues"},
		{"GEN6_UnknownIgnoresFields", ClassificationDetails{Genre: "Rock", IsBrazilian: true, IsKnown: false}, "desconhecido"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			classification := mustClassification(t, testCase.details)

			if got := classification.Genre().String(); got != testCase.expected {
				t.Fatalf("genre = %q, want %q", got, testCase.expected)
			}
		})
	}
}

func TestClassification_WithNationality(t *testing.T) {
	international := mustClassification(t, ClassificationDetails{Genre: "Rap", IsKnown: true})

	brazilian := international.WithNationality(true)

	if brazilian.Genre().String() != "Rap Nacional" || !brazilian.IsBrazilian() || !brazilian.IsKnown() {
		t.Fatalf("brazilian = %q", brazilian.Genre().String())
	}
	if international.Genre().String() != "Rap" || international.IsBrazilian() {
		t.Fatalf("original changed: %q", international.Genre().String())
	}
	if UnknownClassification().WithNationality(true).Genre().String() != "desconhecido" {
		t.Fatal("unknown with nationality must stay unknown")
	}
}

func TestGenreNames_HasTwentyEntries(t *testing.T) {
	names := GenreNames()

	if len(names) != 20 || names[0] != "Rock" || names[19] != "Pagode" {
		t.Fatalf("names = %v", names)
	}
}

func TestSubgenresBelongToOneGenre(t *testing.T) {
	owners := map[string]int{}
	for _, definition := range genrePool {
		for _, subgenre := range definition.subgenres {
			owners[subgenre]++
		}
	}

	subgenres := SubgenreNames()

	if len(subgenres) != 10 || len(owners) != len(subgenres) {
		t.Fatalf("subgenres = %v owners = %v", subgenres, owners)
	}
}

func TestGenreHints(t *testing.T) {
	hints := GenreHints()

	if len(hints) != 2 || hints["Funk"] != "classic international funk" || hints["Fonk"] != "phonk, independent genre" {
		t.Fatalf("hints = %v", hints)
	}
}

func mustClassification(t *testing.T, details ClassificationDetails) Classification {
	t.Helper()
	classification, err := NewClassification(details)
	if err != nil {
		t.Fatalf("building classification %+v: %v", details, err)
	}
	return classification
}
