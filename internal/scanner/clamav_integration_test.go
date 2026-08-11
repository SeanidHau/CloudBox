package scanner

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestClamAVScannerIntegration(t *testing.T) {
	address := strings.TrimSpace(os.Getenv("CLAMAV_TEST_ADDRESS"))
	if address == "" {
		t.Skip("set CLAMAV_TEST_ADDRESS to run against a real clamd server")
	}

	scanner, err := NewClamAVScanner(address)
	if err != nil {
		t.Fatalf("create ClamAV scanner: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clean, err := scanner.Scan(ctx, strings.NewReader("CloudBox clean file"))
	if err != nil {
		t.Fatalf("scan clean content: %v", err)
	}
	if clean.Infected {
		t.Fatalf("clean scan result = %#v, want not infected", clean)
	}

	// EICAR is the standard harmless anti-malware test signature.
	infected, err := scanner.Scan(ctx, strings.NewReader(
		"X5O!P%@AP[4\\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*",
	))
	if err != nil {
		t.Fatalf("scan EICAR content: %v", err)
	}
	if !infected.Infected || infected.Signature == "" {
		t.Fatalf("EICAR scan result = %#v, want infected result with signature", infected)
	}
}
