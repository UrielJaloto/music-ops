//go:build !windows

package transcoding

const executableSuffix = ""

func listInstalledExecutables(string) []string {
	return nil
}

func listFDKLibraryCandidates() []string {
	return nil
}
