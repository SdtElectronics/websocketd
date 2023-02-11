// Copyright 2023 SdtElectronics
// All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package libwebsocketd

import (
	"io"
	"net"
	"os/exec"
	"syscall"
	"time"
)

type DomainEndpoint struct {
	cmd    	  *exec.Cmd
	closetime time.Duration
	conn	  *net.UnixConn
	output    chan []byte
	log       *LogScope
}

func NewDomainEndpoint(command *exec.Cmd, c *net.UnixConn, log *LogScope) *DomainEndpoint {
	return &DomainEndpoint{
		cmd:	 command,
		conn:	 c,
		output:  make(chan []byte),
		log:     log,
	}
}

func (de *DomainEndpoint) Terminate() {
	terminated := make(chan struct{})
	go func() { de.cmd.Wait(); terminated <- struct{}{} }()

	// a bit verbose to create good debugging trail
	select {
	case <-terminated:
		de.log.Debug("process", "Process %v terminated after stdin was closed", de.cmd.Process.Pid)
		return // means process finished
	case <-time.After(100*time.Millisecond + de.closetime):
	}

	err := de.cmd.Process.Signal(syscall.SIGINT)
	if err != nil {
		// process is done without this, great!
		de.log.Error("process", "SIGINT unsuccessful to %v: %s", de.cmd.Process.Pid, err)
	}

	select {
	case <-terminated:
		de.log.Debug("process", "Process %v terminated after SIGINT", de.cmd.Process.Pid)
		return // means process finished
	case <-time.After(250*time.Millisecond + de.closetime):
	}

	err = de.cmd.Process.Signal(syscall.SIGTERM)
	if err != nil {
		// process is done without this, great!
		de.log.Error("process", "SIGTERM unsuccessful to %v: %s", de.cmd.Process.Pid, err)
	}

	select {
	case <-terminated:
		de.log.Debug("process", "Process %v terminated after SIGTERM", de.cmd.Process.Pid)
		return // means process finished
	case <-time.After(500*time.Millisecond + de.closetime):
	}

	err = de.cmd.Process.Kill()
	if err != nil {
		de.log.Error("process", "SIGKILL unsuccessful to %v: %s", de.cmd.Process.Pid, err)
		return
	}

	select {
	case <-terminated:
		de.log.Debug("process", "Process %v terminated after SIGKILL", de.cmd.Process.Pid)
		return // means process finished
	case <-time.After(1000 * time.Millisecond):
	}

	de.log.Error("process", "SIGKILL did not terminate %v!", de.cmd.Process.Pid)
}

func (de *DomainEndpoint) Output() chan []byte {
	return de.output
}

func (de *DomainEndpoint) Send(msg []byte) bool {
	if _, err := de.conn.Write(msg); err != nil {
		if err != io.EOF {
			de.log.Trace("websocket", "Cannot send: %s", err)
		} else {
			de.log.Debug("process", "Process socket closed")
		}
		
		return false
	}

	return true
}

func (de *DomainEndpoint) StartReading() {
	go de.readPkt()
}

func (de *DomainEndpoint) readPkt() {
	buf := make([]byte, 10*1024*1024)
	for {
		n, err := de.conn.Read(buf)
		if err != nil {
			if err != io.EOF {
				de.log.Error("process", "Unexpected error while reading socket from process: %s", err)
			} else {
				de.log.Debug("process", "Process socket closed")
			}
			break
		}
		de.output <- append(make([]byte, 0, n), buf[:n]...) // cloned buffer
		// de.output <- buf[:n]
	}
	close(de.output)
}
