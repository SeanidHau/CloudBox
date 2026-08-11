package scanner

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
)

const defaultChunkSize = 32 * 1024

var ErrAddressRequired = errors.New("ClamAV address is required")

type Result struct {
	Infected  bool
	Signature string
}

type Scanner interface {
	Scan(ctx context.Context, reader io.Reader) (Result, error)
}

type ClamAVScanner struct {
	address   string
	chunkSize int
}

func NewClamAVScanner(address string) (*ClamAVScanner, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, ErrAddressRequired
	}

	return &ClamAVScanner{
		address:   address,
		chunkSize: defaultChunkSize,
	}, nil
}

func (s *ClamAVScanner) Scan(ctx context.Context, reader io.Reader) (Result, error) {
	if reader == nil {
		return Result{}, errors.New("scan content is required")
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.address)
	if err != nil {
		return Result{}, err
	}
	defer conn.Close()

	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()

	if _, err := conn.Write([]byte("zINSTREAM\x00")); err != nil {
		return Result{}, err
	}

	buffer := make([]byte, s.chunkSize)
	for {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}

		read, readErr := reader.Read(buffer)
		if read > 0 {
			var length [4]byte
			binary.BigEndian.PutUint32(length[:], uint32(read))

			if _, err := conn.Write(length[:]); err != nil {
				return Result{}, err
			}
			if _, err := conn.Write(buffer[:read]); err != nil {
				return Result{}, err
			}
		}

		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return Result{}, readErr
		}
	}

	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return Result{}, err
	}

	response, err := bufio.NewReader(conn).ReadString('\x00')
	if err != nil {
		return Result{}, err
	}

	return parseClamAVResponse(response)
}

func parseClamAVResponse(response string) (Result, error) {
	response = strings.TrimSpace(strings.TrimSuffix(response, "\x00"))

	switch {
	case strings.HasSuffix(response, " OK"):
		return Result{}, nil

	case strings.HasSuffix(response, " FOUND"):
		signature := strings.TrimSpace(strings.TrimSuffix(response, " FOUND"))
		if _, result, found := strings.Cut(signature, ":"); found {
			signature = strings.TrimSpace(result)
		}

		return Result{
			Infected:  true,
			Signature: signature,
		}, nil

	case strings.HasSuffix(response, " ERROR"):
		return Result{}, errors.New(response)

	default:
		return Result{}, fmt.Errorf("unexpected ClamAV response: %s", response)
	}
}
