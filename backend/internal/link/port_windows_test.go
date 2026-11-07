//go:build windows && go1.25

package link

import (
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// No COM port in a test: a named pipe stands in for the device. openHandle
// opens its client end the way it opens a port, and that is what these
// tests are about: the handle, not the port settings. (Go files a pipe
// under its own kind, a COM port as a plain file; both are overlapped, and
// Close cancels a pending read for either.)
func testPipe(t *testing.T) (server, client *os.File, h syscall.Handle) {
	t.Helper()
	name := fmt.Sprintf(`\\.\pipe\agents-deck-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	p, _ := syscall.UTF16PtrFromString(name)
	const pipeAccessDuplex, pipeTypeByte = 0x3, 0x0
	r, _, err := kernel32.NewProc("CreateNamedPipeW").Call(uintptr(unsafe.Pointer(p)),
		pipeAccessDuplex|syscall.FILE_FLAG_OVERLAPPED, pipeTypeByte, 1, 4096, 4096, 0, 0)
	if syscall.Handle(r) == syscall.InvalidHandle {
		t.Fatalf("CreateNamedPipe: %v", err)
	}
	server = os.NewFile(r, "server")
	h, err = openHandle(name)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	client = os.NewFile(uintptr(h), "client")
	t.Cleanup(func() { server.Close(); client.Close() })
	return server, client, h
}

// Programs the daemon starts must not inherit the port.
func TestOpenHandleNotInherited(t *testing.T) {
	_, _, h := testPipe(t)
	var flags uint32
	if r, _, err := kernel32.NewProc("GetHandleInformation").Call(uintptr(h), uintptr(unsafe.Pointer(&flags))); r == 0 {
		t.Fatal(err)
	}
	if flags&1 != 0 { // HANDLE_FLAG_INHERIT
		t.Error("the port's handle is inherited by child processes")
	}
}

// The session reads in one goroutine and writes in another: a write must
// not wait for the pending read, and Close must end the read (the daemon
// lets go of a port while nothing arrives on it).
func TestOpenHandleReadWriteClose(t *testing.T) {
	server, client, _ := testPipe(t)
	got := make(chan string, 1)
	readErr := make(chan error, 1)
	go func() {
		buf := make([]byte, 64)
		n, err := client.Read(buf)
		if err != nil {
			readErr <- err
			return
		}
		got <- string(buf[:n])
	}()
	time.Sleep(50 * time.Millisecond) // the read is pending now

	wrote := make(chan error, 1)
	go func() { _, err := client.Write([]byte("ping")); wrote <- err }()
	select {
	case err := <-wrote:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the write waited for the pending read")
	}
	buf := make([]byte, 4)
	if _, err := server.Read(buf); err != nil || string(buf) != "ping" {
		t.Fatalf("the other end read %q, %v", buf, err)
	}
	if _, err := server.Write([]byte("pong")); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-got:
		if s != "pong" {
			t.Fatalf("read %q", s)
		}
	case err := <-readErr:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("the read didn't return")
	}

	go func() {
		_, err := client.Read(make([]byte, 64))
		readErr <- err
	}()
	time.Sleep(50 * time.Millisecond)
	closed := make(chan error, 1)
	go func() { closed <- client.Close() }()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close waited for the pending read")
	}
	select {
	case err := <-readErr:
		if err == nil {
			t.Error("the read returned no error after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close didn't end the pending read")
	}
}

// A port that isn't there neither opens nor counts as plugged in.
func TestOpenPortMissing(t *testing.T) {
	if _, err := OpenPort("COM250", 115200); err == nil {
		t.Fatal("COM250 opened")
	}
	if PortPresent("COM250") {
		t.Error("COM250 is present")
	}
}
