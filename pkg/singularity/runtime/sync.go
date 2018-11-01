package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
)

// State defines type for manipulating with container's state.
type State int

const (
	// StateCreating means container is being created at the moment.
	StateCreating State = 1 + iota
	// StateCreated means container is created without any errors.
	StateCreated
	// StateRunning means container is running at the moment.
	StateRunning
	// StateExited means container has finished possibly with errors.
	StateExited
)

// ObserveState listens on passed socket for container state changes
// and passes them to the channel. ObserveState creates socket
// if necessary. Since this function is used to sync with runtime the
// returned channel is unbuffered. The channel will be closed if either
// container has transmitted into StateExited or any error during networking occurred.
// ObserveState returns error only if it fails to start listener on the passed socket.
func ObserveState(ctx context.Context, socket string) (<-chan State, error) {
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("could not listen sync socket: %v", err)
	}

	syncChan := make(chan State)
	go func() {
		defer close(syncChan)
		defer ln.Close()

		for {
			select {
			case <-ctx.Done():
				log.Printf("context is done")
				return
			default:
				conn, err := ln.Accept()
				if err != nil {
					log.Printf("could not accept sync socket connection: %v", err)
					return
				}
				shouldExit := syncOnConn(ctx, conn, syncChan)
				if shouldExit {
					return
				}
			}
		}
	}()
	return syncChan, nil
}

func syncOnConn(ctx context.Context, conn net.Conn, syncChan chan<- State) bool {
	type statusInfo struct {
		Status string `json:"status"`
	}

	defer conn.Close()
	dec := json.NewDecoder(conn)
	var status statusInfo
	for {
		select {
		case <-ctx.Done():
			log.Printf("sync %s: context is done", conn.RemoteAddr())
			return false
		default:
			if dec.More() {
				log.Printf("got some data!")
				err := dec.Decode(&status)
				if err != nil {
					log.Printf("could not read state from %s: %v", conn.RemoteAddr(), err)
					return true
				}
				switch status.Status {
				case "creating":
					syncChan <- StateCreating
				case "created":
					syncChan <- StateCreated
				case "running":
					syncChan <- StateRunning
				case "stopped":
					syncChan <- StateExited
					log.Printf("received stopped from %s", conn.RemoteAddr())
					return true
				default:
					log.Printf("unknown status received on %s: %s", conn.RemoteAddr(), status.Status)
				}
			}
		}
	}
}
