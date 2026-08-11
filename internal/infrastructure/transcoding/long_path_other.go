//go:build !windows

package transcoding

func toProcessPath(path string) string {
	return path
}
