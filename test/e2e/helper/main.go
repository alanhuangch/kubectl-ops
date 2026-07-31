package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fatalf("expected mode: stable or restart-once")
	}

	switch os.Args[1] {
	case "stable":
		waitForever()
	case "restart-once":
		if len(os.Args) != 3 {
			fatalf("restart-once requires a marker path")
		}
		marker := os.Args[2]
		if _, err := os.Stat(marker); os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
				fatalf("create marker directory: %v", err)
			}
			if err := os.WriteFile(marker, []byte("restarted\n"), 0o644); err != nil {
				fatalf("write marker: %v", err)
			}
			os.Exit(137)
		} else if err != nil {
			fatalf("inspect marker: %v", err)
		}
		waitForever()
	default:
		fatalf("unknown mode %q", os.Args[1])
	}
}

func waitForever() {
	for {
		time.Sleep(time.Hour)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
