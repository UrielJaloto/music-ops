//go:build integration && windows

package desktop

import (
	"errors"
	"flag"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/UrielJaloto/music-ops/internal/services/importing"
	"golang.org/x/sys/windows"
)

const (
	detachedChildVariable = "MUSIC_OPS_DETACHED_CHILD"
	genericAll            = 0x10000000
	closeMessage          = 0x0010
	closeInterval         = 50 * time.Millisecond
)

var (
	user32             = windows.NewLazySystemDLL("user32.dll")
	createDesktop      = user32.NewProc("CreateDesktopW")
	closeDesktop       = user32.NewProc("CloseDesktop")
	setThreadDesktop   = user32.NewProc("SetThreadDesktop")
	enumDesktopWindows = user32.NewProc("EnumDesktopWindows")
	postMessage        = user32.NewProc("PostMessageW")
)

func TestCountConsoleProcesses_WithoutConsole(t *testing.T) {
	if os.Getenv(detachedChildVariable) != "" {
		if _, err := countConsoleProcesses(); err == nil || !strings.Contains(err.Error(), "reading console process list") {
			t.Fatalf("err = %v", err)
		}
		return
	}
	arguments := []string{"-test.run=^TestCountConsoleProcesses_WithoutConsole$"}
	if coverDir := flag.Lookup("test.gocoverdir"); coverDir != nil && coverDir.Value.String() != "" {
		arguments = append(arguments, "-test.gocoverdir="+coverDir.Value.String())
	}
	child := exec.Command(os.Args[0], arguments...)
	child.Env = append(os.Environ(), detachedChildVariable+"=1")
	child.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS}

	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("child: %v %s", err, output)
	}
}

func TestBrowseFolder_ClosedDialog_IsCancelled(t *testing.T) {
	name, err := windows.UTF16PtrFromString("music-ops-test")
	if err != nil {
		t.Fatal(err)
	}
	desk, _, createErr := createDesktop.Call(uintptr(unsafe.Pointer(name)), 0, 0, 0, genericAll, 0)
	if desk == 0 {
		t.Fatal(createErr)
	}
	t.Cleanup(func() { _, _, _ = closeDesktop.Call(desk) })
	done := make(chan error, 1)
	go browseOnDesktop(desk, done)
	go closeWindowsOnDesktop(desk, done)

	select {
	case err := <-done:
		if !errors.Is(err, importing.ErrFolderChoiceCancelled) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("dialog never closed")
	}
}

func browseOnDesktop(desk uintptr, done chan<- error) {
	runtime.LockOSThread()
	if isSet, _, err := setThreadDesktop.Call(desk); isSet == 0 {
		done <- err
		return
	}
	_, err := browseFolder("Choose", "")
	done <- err
}

func closeWindowsOnDesktop(desk uintptr, done chan error) {
	runtime.LockOSThread()
	_, _, _ = setThreadDesktop.Call(desk)
	closeAll := windows.NewCallback(func(window, _ uintptr) uintptr {
		_, _, _ = postMessage.Call(window, closeMessage, 0, 0)
		return 1
	})
	ticker := time.NewTicker(closeInterval)
	defer ticker.Stop()
	for len(done) == 0 {
		_, _, _ = enumDesktopWindows.Call(desk, closeAll, 0)
		<-ticker.C
	}
}
