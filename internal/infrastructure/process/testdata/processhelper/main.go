package main

import (
	"bytes"
	"io"
	"os"
	"strconv"
	"time"
)

const (
	failureExitCode = 3
	sleepDuration   = time.Minute
)

func main() {
	switch os.Args[1] {
	case "exit":
		code, _ := strconv.Atoi(os.Args[2])
		os.Exit(code)
	case "stderr":
		size, _ := strconv.Atoi(os.Args[2])
		_, _ = os.Stderr.Write(append([]byte("HEAD"), bytes.Repeat([]byte("x"), size)...))
		_, _ = os.Stderr.Write([]byte("TAIL"))
		os.Exit(failureExitCode)
	case "echo":
		_, _ = io.Copy(os.Stdout, os.Stdin)
	case "sleep":
		time.Sleep(sleepDuration)
	}
}
