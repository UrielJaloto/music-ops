//go:build windows

package desktop

import (
	"errors"
	"testing"

	"github.com/UrielJaloto/music-ops/internal/services/importing"
	"github.com/sqweek/dialog"
)

func TestWrapBrowseError(t *testing.T) {
	errDialog := errors.New("dialog")
	cancelled := wrapBrowseError(dialog.ErrCancelled)
	failed := wrapBrowseError(errDialog)

	if !errors.Is(cancelled, importing.ErrFolderChoiceCancelled) || !errors.Is(failed, errDialog) || failed.Error() != "choosing folder: dialog" || wrapBrowseError(nil) != nil {
		t.Fatalf("errors = %v %v", cancelled, failed)
	}
}
