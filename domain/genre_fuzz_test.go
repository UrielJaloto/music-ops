package domain

import "testing"

func FuzzClassificationGenre(f *testing.F) {
	f.Add(uint8(0), uint8(1), true, true)
	f.Add(uint8(16), uint8(1), true, true)
	f.Add(uint8(4), uint8(0), true, true)
	f.Add(uint8(0), uint8(0), false, false)
	allowed := derivableGenres()
	f.Fuzz(func(t *testing.T, genreIndex, subgenreIndex uint8, isBrazilian, isKnown bool) {
		definition := genrePool[int(genreIndex)%len(genrePool)]
		details := ClassificationDetails{Genre: definition.name, Subgenre: subgenreAt(definition, subgenreIndex), IsBrazilian: isBrazilian, IsKnown: isKnown}

		classification, err := NewClassification(details)
		if err != nil {
			t.Fatalf("pool classification rejected: %+v %v", details, err)
		}

		if !allowed[classification.Genre().String()] {
			t.Fatalf("genre %q is not derivable from the pool", classification.Genre().String())
		}
	})
}

func subgenreAt(definition genreDefinition, index uint8) string {
	choices := append([]string{""}, definition.subgenres...)
	return choices[int(index)%len(choices)]
}

func derivableGenres() map[string]bool {
	allowed := map[string]bool{unknownGenreValue: true}
	for _, definition := range genrePool {
		for _, subgenre := range append([]string{""}, definition.subgenres...) {
			allowed[joinGenreParts(definition.name, subgenre)] = true
			allowed[joinGenreParts(withNationalSuffix(definition.name), withNationalSuffix(subgenre))] = true
		}
	}
	return allowed
}
