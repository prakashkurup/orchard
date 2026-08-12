package codeburn

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testPayload = `{
  "generated":"2026-08-12T18:00:00Z",
  "current":{
    "label":"Today","cost":3.25,"calls":12,"sessions":2,
    "inputTokens":100,"outputTokens":20,"cacheReadTokens":800,"cacheWriteTokens":10,
    "cacheHitPercent":88.9,"topProjects":[{"name":"orchard","cost":3.25,"sessions":2}]
  },
  "history":{"daily":[{"date":"2026-08-12","cost":3.25,"calls":12}]},
  "currency":{"code":"USD","symbol":"$","rate":1}
}`

func TestDecodePayloadToleratesAdditiveFields(t *testing.T) {
	data := strings.Replace(testPayload, `"current":{`, `"futureTopLevel":true,"current":{"futureField":"ignored",`, 1)
	payload, err := decodePayload([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if payload.Current.Label != "Today" || payload.Current.Cost != 3.25 || payload.Current.Calls != 12 {
		t.Fatalf("unexpected payload: %+v", payload.Current)
	}
	if len(payload.Current.TopProjects) != 1 || payload.Current.TopProjects[0].Name != "orchard" {
		t.Fatalf("projects not decoded: %+v", payload.Current.TopProjects)
	}
}

func TestDecodePayloadRejectsMissingContractFields(t *testing.T) {
	if _, err := decodePayload([]byte(`{"current":{"cost":1}}`)); err == nil {
		t.Fatal("missing current.label should fail")
	}
}

func TestQueryArgsAreScopedAndReadOnly(t *testing.T) {
	t.Setenv("HOME", "/tmp/codeburn-home")
	got := queryArgs(PeriodWeek, "~/Documents/GitHub")
	want := []string{
		"status", "--format", "menubar-json", "--period", "week", "--provider", "all",
		"--no-optimize", "--no-timeline", "--project", "/tmp/codeburn-home/Documents/GitHub",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestClientUsesResidentServeProtocol(t *testing.T) {
	response := `{"id":1,"ok":true,"output":` + strconv.Quote(testPayload) + `}`
	script := writeExecutable(t, "#!/bin/sh\n"+
		"printf '%s\\n' '{\"ready\":true,\"pid\":123}'\n"+
		"IFS= read -r request\n"+
		"printf '%s\\n' '"+shellSingleQuote(response)+"'\n")

	client := NewClientWithExecutable(script)
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	payload, err := client.Query(ctx, PeriodToday, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if payload.Current.Cost != 3.25 || payload.Current.Sessions != 2 {
		t.Fatalf("unexpected resident payload: %+v", payload.Current)
	}
}

func TestClientFallsBackToOneShotStatus(t *testing.T) {
	script := writeExecutable(t, "#!/bin/sh\n"+
		"if [ \"$1\" = \"serve\" ]; then printf '%s\\n' '{}'; exit 0; fi\n"+
		"printf '%s\\n' '"+shellSingleQuote(testPayload)+"'\n")

	client := NewClientWithExecutable(script)
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	payload, err := client.Query(ctx, PeriodToday, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if payload.Current.Label != "Today" {
		t.Fatalf("fallback payload = %+v", payload.Current)
	}
}

func TestClientStopsResidentOnTimeout(t *testing.T) {
	script := writeExecutable(t, "#!/bin/sh\n"+
		"printf '%s\\n' '{\"ready\":true,\"pid\":123}'\n"+
		"IFS= read -r request\n"+
		"sleep 5\n")

	client := NewClientWithExecutable(script)
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	_, err := client.Query(ctx, PeriodToday, "/repo")
	if err == nil {
		t.Fatal("timed-out query should fail")
	}
}

func TestDisabledClientIsUnavailable(t *testing.T) {
	t.Setenv("ORCHARD_CODEBURN", "off")
	client := NewClient()
	_, err := client.Query(context.Background(), PeriodToday, "/repo")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func writeExecutable(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-codeburn")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellSingleQuote(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}
