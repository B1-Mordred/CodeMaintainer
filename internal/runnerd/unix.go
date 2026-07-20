package runnerd

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
)

func ReadTokenFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect runnerd token: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("runnerd token must be a regular file inaccessible to group and other users")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read runnerd token: %w", err)
	}
	token := []byte(strings.TrimSpace(string(payload)))
	if len(token) < 32 || len(token) > 4096 {
		return nil, errors.New("runnerd token must contain between 32 and 4096 bytes")
	}
	return token, nil
}

func ListenUnix(path string) (net.Listener, error) {
	if path == "" {
		return nil, errors.New("runnerd socket path is required")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("refusing to replace non-socket runnerd path %s", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove stale runnerd socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect runnerd socket: %w", err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on runnerd socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
		return nil, fmt.Errorf("protect runnerd socket: %w", err)
	}
	return &protectedUnixListener{Listener: listener, path: path}, nil
}

type protectedUnixListener struct {
	net.Listener
	path string
}

func (l *protectedUnixListener) Close() error {
	listenErr := l.Listener.Close()
	removeErr := os.Remove(l.path)
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return removeErr
	}
	return listenErr
}
