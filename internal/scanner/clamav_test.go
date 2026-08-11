package scanner

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
)

var _ Scanner = (*ClamAVScanner)(nil)

func TestNewClamAVScannerRequiresAddress(t *testing.T) {
	if _, err := NewClamAVScanner("  "); !errors.Is(err, ErrAddressRequired) {
		t.Fatalf("empty address error = %v, want %v", err, ErrAddressRequired)
	}
}

func TestClamAVScannerStreamsContentAndParsesResponses(t *testing.T) {
	for _, test := range []struct {
		name          string
		response      string
		wantInfected  bool
		wantSignature string
		wantError     bool
	}{
		{
			name:     "clean",
			response: "stream: OK\x00",
		},
		{
			name:          "infected",
			response:      "stream: Eicar-Test-Signature FOUND\x00",
			wantInfected:  true,
			wantSignature: "Eicar-Test-Signature",
		},
		{
			name:      "scanner error",
			response:  "stream: scan failed ERROR\x00",
			wantError: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			address, requests := startTestClamAVServer(t, test.response)
			scanner, err := NewClamAVScanner(address)
			if err != nil {
				t.Fatalf("create scanner: %v", err)
			}
			// A small chunk size proves that the client sends multiple INSTREAM frames.
			scanner.chunkSize = 3

			result, err := scanner.Scan(context.Background(), bytes.NewBufferString("cloudbox"))
			if test.wantError {
				if err == nil {
					t.Fatal("scanner error response should return an error")
				}
			} else if err != nil {
				t.Fatalf("scan content: %v", err)
			}
			if result.Infected != test.wantInfected || result.Signature != test.wantSignature {
				t.Fatalf("scan result = %#v, want infected=%t signature=%q", result, test.wantInfected, test.wantSignature)
			}

			request := <-requests
			if request.err != nil {
				t.Fatalf("read ClamAV request: %v", request.err)
			}
			if string(request.content) != "cloudbox" {
				t.Fatalf("streamed content = %q, want cloudbox", request.content)
			}
		})
	}
}

func TestParseClamAVResponseRejectsUnexpectedText(t *testing.T) {
	if _, err := parseClamAVResponse("stream: unknown\x00"); err == nil {
		t.Fatal("unexpected response should return an error")
	}
}

type clamAVRequest struct {
	content []byte
	err     error
}

func startTestClamAVServer(t *testing.T, response string) (string, <-chan clamAVRequest) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen test ClamAV server: %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})

	requests := make(chan clamAVRequest, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			requests <- clamAVRequest{err: err}
			return
		}
		defer conn.Close()

		content, err := readClamAVStream(conn)
		requests <- clamAVRequest{content: content, err: err}
		if err != nil {
			return
		}

		_, _ = io.WriteString(conn, response)
	}()

	return listener.Addr().String(), requests
}

func readClamAVStream(reader io.Reader) ([]byte, error) {
	command := make([]byte, len("zINSTREAM\x00"))
	if _, err := io.ReadFull(reader, command); err != nil {
		return nil, err
	}
	if string(command) != "zINSTREAM\x00" {
		return nil, errors.New("unexpected ClamAV command")
	}

	var content bytes.Buffer
	for {
		var length [4]byte
		if _, err := io.ReadFull(reader, length[:]); err != nil {
			return nil, err
		}

		size := binary.BigEndian.Uint32(length[:])
		if size == 0 {
			return content.Bytes(), nil
		}

		chunk := make([]byte, size)
		if _, err := io.ReadFull(reader, chunk); err != nil {
			return nil, err
		}
		_, _ = content.Write(chunk)
	}
}
