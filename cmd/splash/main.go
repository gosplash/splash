package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 3 || os.Args[1] != "check" {
		fmt.Fprintln(os.Stderr, "usage: splash check <file.splash>")
		os.Exit(1)
	}
	fmt.Println("splash check: not yet implemented")
}
