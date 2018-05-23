package main

import (
	"fmt"
	"runtime"
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	fmt.Println("hello btc")

	s := NewServer()
	s.start()
	defer func() {
		s.WaitForShutdown()
		s.Stop()
	}()
}
