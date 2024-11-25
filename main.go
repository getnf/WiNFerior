//go:build !gui && !tui
// +build !gui,!tui

package main

import (
	"fmt"
)

func main() {

	fmt.Println("Please specify a build tag (tui | gui)")
}