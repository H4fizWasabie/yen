package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func ServeUnix(ctx context.Context, path string, server *Server) error {
	if server == nil {
		return errors.New("rpc server is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("rpc socket path is not a socket: %s", path)
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	return serveListener(ctx, listener, server)
}

// ServeTCP provides the RPC transport for platforms without Unix sockets.
func ServeTCP(ctx context.Context, address string, server *Server) error {
	if server == nil {
		return errors.New("rpc server is required")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	return serveListener(ctx, listener, server)
}

func serveListener(ctx context.Context, listener net.Listener, server *Server) error {
	var connections sync.WaitGroup
	var activeMu sync.Mutex
	active := make(map[net.Conn]struct{})
	defer connections.Wait()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
		activeMu.Lock()
		for connection := range active {
			_ = connection.Close()
		}
		activeMu.Unlock()
	}()
	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		activeMu.Lock()
		active[connection] = struct{}{}
		activeMu.Unlock()
		connections.Add(1)
		go func() {
			defer connections.Done()
			defer connection.Close()
			defer func() {
				activeMu.Lock()
				delete(active, connection)
				activeMu.Unlock()
			}()
			_ = server.Serve(ctx, connection, connection)
		}()
	}
}

type Client struct {
	connection net.Conn
	scanner    *bufio.Scanner
	mu         sync.Mutex
}

func DialUnix(path string) (*Client, error) {
	return dialNetwork("unix", path)
}

func DialTCP(address string) (*Client, error) {
	return dialNetwork("tcp", address)
}

func dialNetwork(network, address string) (*Client, error) {
	connection, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(connection)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	return &Client{connection: connection, scanner: scanner}, nil
}

func (c *Client) Call(command map[string]any) (map[string]any, error) {
	return c.CallWithEvents(command, nil)
}

// CallWithEvents sends one JSONL command and returns its correlated response.
// Lines that are not the matching response are forwarded to onEvent, allowing
// callers to observe the same streaming events as the TypeScript RPC client.
func (c *Client) CallWithEvents(command map[string]any, onEvent func(map[string]any)) (map[string]any, error) {
	if c == nil {
		return nil, errors.New("rpc client is closed")
	}
	if command == nil {
		return nil, errors.New("rpc command is required")
	}
	request := make(map[string]any, len(command)+1)
	for key, value := range command {
		request[key] = value
	}
	if id, ok := request["id"].(string); !ok || id == "" {
		request["id"] = fmt.Sprintf("req_%d", time.Now().UnixNano())
	}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.connection == nil {
		return nil, errors.New("rpc client is closed")
	}
	if _, err := fmt.Fprintf(c.connection, "%s\n", data); err != nil {
		return nil, err
	}
	for c.scanner.Scan() {
		var response map[string]any
		if err := json.Unmarshal(c.scanner.Bytes(), &response); err != nil {
			continue
		}
		if response["type"] == "response" && response["id"] == request["id"] {
			return response, nil
		}
		if onEvent != nil {
			onEvent(response)
		}
	}
	if err := c.scanner.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("rpc connection closed")
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.connection == nil {
		return nil
	}
	err := c.connection.Close()
	c.connection = nil
	return err
}
